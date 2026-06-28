package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/tools"
)

const DefaultSystemPrompt = `You are codeworld, a conservative local coding agent.
Inspect files before editing them.
Keep changes narrow and explain why permissioned actions are needed.
Prefer apply_patch for existing source files.
Never invent file contents, command output, or test results.
When a tool fails, use the error result to decide the next step.`

type Confirmer interface {
	Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error)
}

type ToolEventStatus string

const (
	ToolEventStart   ToolEventStatus = "start"
	ToolEventSuccess ToolEventStatus = "success"
	ToolEventDenied  ToolEventStatus = "denied"
	ToolEventError   ToolEventStatus = "error"
)

type ToolEvent struct {
	Status  ToolEventStatus
	Name    string
	CallID  string
	Request permissions.Request
	Error   string
}

type ToolReporter interface {
	ReportTool(ctx context.Context, event ToolEvent)
}

type Runner struct {
	Model        model.Client
	Tools        *tools.Registry
	Policy       permissions.Policy
	Confirmer    Confirmer
	Reporter     ToolReporter
	MaxSteps     int
	ModelName    string
	SystemPrompt string
}

type TurnResult struct {
	FinalText string
	Messages  []model.Message
	Usage     model.Usage
}

func (r Runner) RunTurn(ctx context.Context, history []model.Message, input string) (TurnResult, error) {
	if err := ctx.Err(); err != nil {
		return TurnResult{}, err
	}
	if r.Model == nil {
		return TurnResult{}, fmt.Errorf("agent model is required")
	}
	if r.Tools == nil {
		return TurnResult{}, fmt.Errorf("agent tools registry is required")
	}

	maxSteps := r.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 20
	}
	messages := []model.Message{{Role: model.RoleSystem, Content: firstNonEmpty(r.SystemPrompt, DefaultSystemPrompt)}}
	messages = append(messages, history...)
	messages = append(messages, model.Message{Role: model.RoleUser, Content: input})
	var usage model.Usage

	for step := 0; step < maxSteps; step++ {
		resp, err := r.Model.Generate(ctx, model.GenerateRequest{
			Model:    r.ModelName,
			Messages: append([]model.Message(nil), messages...),
			Tools:    r.Tools.Definitions(),
		})
		if err != nil {
			return TurnResult{}, err
		}
		usage = usage.Add(resp.Usage)

		toolCalls := responseToolCalls(resp)
		if len(toolCalls) > 0 {
			messages = append(messages, model.Message{Role: model.RoleAssistant, Content: responseFinalText(resp), ToolCalls: toolCalls})
			for _, call := range toolCalls {
				content := r.executeTool(ctx, call)
				messages = append(messages, model.Message{Role: model.RoleTool, ToolCallID: call.ID, Content: content})
			}
			continue
		}

		finalText := responseFinalText(resp)
		if finalText == "" {
			return TurnResult{}, fmt.Errorf("model returned no final text and no tool calls")
		}
		messages = append(messages, model.Message{Role: model.RoleAssistant, Content: finalText})
		return TurnResult{FinalText: finalText, Messages: messages, Usage: usage}, nil
	}
	return TurnResult{}, fmt.Errorf("agent exceeded max steps %d", maxSteps)
}

func (r Runner) executeTool(ctx context.Context, call model.ToolCall) string {
	tool, ok := r.Tools.Get(call.Name)
	if !ok {
		r.reportTool(ctx, ToolEvent{Status: ToolEventError, Name: call.Name, CallID: call.ID, Error: "tool error: unknown tool " + call.Name})
		return "tool error: unknown tool " + call.Name
	}

	req, err := tool.PermissionRequest(call.Arguments)
	if err != nil {
		message := "permission request error: " + err.Error()
		r.reportTool(ctx, ToolEvent{Status: ToolEventError, Name: call.Name, CallID: call.ID, Error: message})
		return message
	}
	event := ToolEvent{Name: call.Name, CallID: call.ID, Request: req}
	r.reportTool(ctx, event.withStatus(ToolEventStart, ""))

	decision, err := r.permissionPolicy().Check(ctx, req)
	if err != nil {
		message := "permission policy error: " + err.Error()
		r.reportTool(ctx, event.withStatus(ToolEventError, message))
		return message
	}

	switch decision.Kind {
	case permissions.DecisionAllow:
	case permissions.DecisionDeny:
		message := "permission denied: " + decision.Reason
		r.reportTool(ctx, event.withStatus(ToolEventDenied, message))
		return message
	case permissions.DecisionAsk:
		if r.Confirmer == nil {
			message := "permission denied: confirmation required"
			r.reportTool(ctx, event.withStatus(ToolEventDenied, message))
			return message
		}
		allowed, err := r.Confirmer.Confirm(ctx, req, decision)
		if err != nil {
			message := "confirmation error: " + err.Error()
			r.reportTool(ctx, event.withStatus(ToolEventError, message))
			return message
		}
		if !allowed {
			message := "permission denied by user"
			r.reportTool(ctx, event.withStatus(ToolEventDenied, message))
			return message
		}
	default:
		message := "permission denied: unknown permission decision " + string(decision.Kind)
		r.reportTool(ctx, event.withStatus(ToolEventDenied, message))
		return message
	}

	result, err := tool.Execute(ctx, call.Arguments)
	if err != nil {
		message := "tool error: " + err.Error()
		r.reportTool(ctx, event.withStatus(ToolEventError, message))
		return encodeToolResult(result, message)
	}
	r.reportTool(ctx, event.withStatus(ToolEventSuccess, ""))
	return encodeToolResult(result, "")
}

func (r Runner) reportTool(ctx context.Context, event ToolEvent) {
	if r.Reporter != nil {
		r.Reporter.ReportTool(ctx, event)
	}
}

func (e ToolEvent) withStatus(status ToolEventStatus, message string) ToolEvent {
	e.Status = status
	e.Error = message
	return e
}

func (r Runner) permissionPolicy() permissions.Policy {
	if r.Policy != nil {
		return r.Policy
	}
	return permissions.ConservativePolicy{}
}

func responseFinalText(resp model.GenerateResponse) string {
	if resp.FinalText != "" {
		return resp.FinalText
	}
	return resp.Message.Content
}

func responseToolCalls(resp model.GenerateResponse) []model.ToolCall {
	if len(resp.ToolCalls) > 0 {
		return resp.ToolCalls
	}
	return resp.Message.ToolCalls
}

func encodeToolResult(result tools.Result, prefix string) string {
	data, err := json.Marshal(result)
	if err != nil {
		if prefix != "" {
			return prefix
		}
		return result.Content
	}
	if prefix != "" {
		return prefix + "\n" + string(data)
	}
	return string(data)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
