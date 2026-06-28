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

type Runner struct {
	Model        model.Client
	Tools        *tools.Registry
	Policy       permissions.Policy
	Confirmer    Confirmer
	MaxSteps     int
	ModelName    string
	SystemPrompt string
}

type TurnResult struct {
	FinalText string
	Messages  []model.Message
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

	for step := 0; step < maxSteps; step++ {
		resp, err := r.Model.Generate(ctx, model.GenerateRequest{
			Model:    r.ModelName,
			Messages: append([]model.Message(nil), messages...),
			Tools:    r.Tools.Definitions(),
		})
		if err != nil {
			return TurnResult{}, err
		}

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
		return TurnResult{FinalText: finalText, Messages: messages}, nil
	}
	return TurnResult{}, fmt.Errorf("agent exceeded max steps %d", maxSteps)
}

func (r Runner) executeTool(ctx context.Context, call model.ToolCall) string {
	tool, ok := r.Tools.Get(call.Name)
	if !ok {
		return "tool error: unknown tool " + call.Name
	}

	req, err := tool.PermissionRequest(call.Arguments)
	if err != nil {
		return "permission request error: " + err.Error()
	}
	decision, err := r.permissionPolicy().Check(ctx, req)
	if err != nil {
		return "permission policy error: " + err.Error()
	}

	switch decision.Kind {
	case permissions.DecisionAllow:
	case permissions.DecisionDeny:
		return "permission denied: " + decision.Reason
	case permissions.DecisionAsk:
		if r.Confirmer == nil {
			return "permission denied: confirmation required"
		}
		allowed, err := r.Confirmer.Confirm(ctx, req, decision)
		if err != nil {
			return "confirmation error: " + err.Error()
		}
		if !allowed {
			return "permission denied by user"
		}
	default:
		return "permission denied: unknown permission decision " + string(decision.Kind)
	}

	result, err := tool.Execute(ctx, call.Arguments)
	if err != nil {
		return encodeToolResult(result, "tool error: "+err.Error())
	}
	return encodeToolResult(result, "")
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
