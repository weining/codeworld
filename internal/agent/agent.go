package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/tools"
)

const DefaultSystemPrompt = `You are codeworld, a conservative local coding agent.
Inspect files before editing them.
Keep changes narrow and explain why permissioned actions are needed.
Prefer apply_patch for existing source files.
Never invent file contents, command output, or test results.
When a tool fails, use the error result to decide the next step.
If you need to inspect files, search, or run commands, call the appropriate tool in the same turn. Do not reply only that you will check something.`

const deferredActionReprompt = "The previous assistant message said it would inspect, search, check, read, or run something, but it did not call a tool. If a tool is needed, call the appropriate tool now. Otherwise answer directly with the information already available. Do not say you will do something later."

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

type TurnEventKind string

const (
	TurnEventAssistantDelta    TurnEventKind = "assistant_delta"
	TurnEventAssistantDone     TurnEventKind = "assistant_done"
	TurnEventToolStart         TurnEventKind = "tool_start"
	TurnEventToolDone          TurnEventKind = "tool_done"
	TurnEventToolError         TurnEventKind = "tool_error"
	TurnEventPermissionRequest TurnEventKind = "permission_request"
	TurnEventPermissionResult  TurnEventKind = "permission_result"
	TurnEventUsage             TurnEventKind = "usage"
	TurnEventError             TurnEventKind = "error"
)

type TurnEvent struct {
	Kind    TurnEventKind
	Text    string
	Tool    ToolEvent
	Request permissions.Request
	Allowed bool
	Usage   model.Usage
	Err     error
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

	// 非流式路径保留完整的“模型 -> 工具 -> 模型”循环，便于不支持 stream 的 provider 复用同一套 agent 逻辑。
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
		if isDeferredActionPlaceholder(finalText) {
			messages = append(messages, model.Message{Role: model.RoleSystem, Content: deferredActionReprompt})
			continue
		}
		return TurnResult{FinalText: finalText, Messages: messages, Usage: usage}, nil
	}
	return TurnResult{}, fmt.Errorf("agent exceeded max steps %d", maxSteps)
}

func (r Runner) RunTurnStream(ctx context.Context, history []model.Message, input string, emit func(TurnEvent) error) (TurnResult, error) {
	if streamClient, ok := r.Model.(model.StreamClient); ok {
		return r.runTurnWithStream(ctx, streamClient, history, input, emit)
	}
	// provider 不支持 stream 时降级到非流式结果，并通过事件接口补齐最终输出和用量。
	result, err := r.RunTurn(ctx, history, input)
	if err != nil {
		if emit != nil {
			_ = emit(TurnEvent{Kind: TurnEventError, Err: err, Text: err.Error()})
		}
		return TurnResult{}, err
	}
	if emit != nil {
		if err := emit(TurnEvent{Kind: TurnEventAssistantDone, Text: result.FinalText}); err != nil {
			return TurnResult{}, err
		}
		if !result.Usage.IsZero() {
			if err := emit(TurnEvent{Kind: TurnEventUsage, Usage: result.Usage}); err != nil {
				return TurnResult{}, err
			}
		}
	}
	return result, nil
}

func (r Runner) runTurnWithStream(ctx context.Context, client model.StreamClient, history []model.Message, input string, emit func(TurnEvent) error) (TurnResult, error) {
	if err := ctx.Err(); err != nil {
		return TurnResult{}, err
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

	// 每一步都可能产生工具调用；工具结果写回 messages 后继续下一轮模型调用。
	for step := 0; step < maxSteps; step++ {
		resp, stepUsage, err := r.streamGenerate(ctx, client, model.GenerateRequest{
			Model:    r.ModelName,
			Messages: append([]model.Message(nil), messages...),
			Tools:    r.Tools.Definitions(),
		}, emit)
		if err != nil {
			if emit != nil {
				_ = emit(TurnEvent{Kind: TurnEventError, Err: err, Text: err.Error()})
			}
			return TurnResult{}, err
		}
		usage = usage.Add(stepUsage)

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
			err := fmt.Errorf("model returned no final text and no tool calls")
			if emit != nil {
				_ = emit(TurnEvent{Kind: TurnEventError, Err: err, Text: err.Error()})
			}
			return TurnResult{}, err
		}
		messages = append(messages, model.Message{Role: model.RoleAssistant, Content: finalText})
		if isDeferredActionPlaceholder(finalText) {
			messages = append(messages, model.Message{Role: model.RoleSystem, Content: deferredActionReprompt})
			continue
		}
		if emit != nil {
			if err := emit(TurnEvent{Kind: TurnEventAssistantDone, Text: finalText}); err != nil {
				return TurnResult{}, err
			}
		}
		return TurnResult{FinalText: finalText, Messages: messages, Usage: usage}, nil
	}
	return TurnResult{}, fmt.Errorf("agent exceeded max steps %d", maxSteps)
}

func (r Runner) streamGenerate(ctx context.Context, client model.StreamClient, req model.GenerateRequest, emit func(TurnEvent) error) (model.GenerateResponse, model.Usage, error) {
	var text string
	var usage model.Usage
	var final model.Message
	var toolCalls []model.ToolCall
	// 流式事件可能分开发送文本、工具调用和 usage，这里合并成一次完整的 GenerateResponse。
	err := client.Stream(ctx, req, func(event model.StreamEvent) error {
		switch event.Kind {
		case model.StreamEventTextDelta:
			text += event.Delta
			if emit != nil {
				return emit(TurnEvent{Kind: TurnEventAssistantDelta, Text: event.Delta})
			}
		case model.StreamEventUsage:
			usage = usage.Add(event.Usage)
			if emit != nil {
				return emit(TurnEvent{Kind: TurnEventUsage, Usage: event.Usage})
			}
		case model.StreamEventToolCall:
			toolCalls = append(toolCalls, event.ToolCalls...)
		case model.StreamEventDone:
			final = event.Message
			if len(event.ToolCalls) > 0 {
				toolCalls = append(toolCalls, event.ToolCalls...)
			}
			usage = usage.Add(event.Usage)
			if emit != nil && !event.Usage.IsZero() {
				return emit(TurnEvent{Kind: TurnEventUsage, Usage: event.Usage})
			}
		case model.StreamEventError:
			if event.Err != nil {
				return event.Err
			}
			return fmt.Errorf("model stream error")
		}
		return nil
	})
	if err != nil {
		return model.GenerateResponse{}, usage, err
	}
	if final.Content == "" {
		final = model.Message{Role: model.RoleAssistant, Content: text}
	}
	return model.GenerateResponse{Message: final, ToolCalls: toolCalls, FinalText: final.Content, Usage: usage}, usage, nil
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

	// 权限判定在真正执行工具前完成；Ask 类型必须经 Confirmer 返回后才继续。
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

func isDeferredActionPlaceholder(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	prefixes := []string{
		"let me ",
		"i'll ",
		"i will ",
		"i'm going to ",
		"i am going to ",
		"让我",
		"我来",
		"我先",
		"我会",
		"我将",
	}
	hasPrefix := false
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			hasPrefix = true
			break
		}
	}
	if !hasPrefix {
		return false
	}
	actions := []string{
		"check",
		"inspect",
		"search",
		"read",
		"run",
		"look",
		"查看",
		"检查",
		"查一下",
		"搜索",
		"读取",
		"运行",
		"看看",
	}
	for _, action := range actions {
		if strings.Contains(normalized, action) {
			return true
		}
	}
	return false
}
