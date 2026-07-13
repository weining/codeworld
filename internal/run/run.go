package run

import (
	"context"
	"errors"
	"fmt"
	"io"

	"codeworld/internal/agent"
	"codeworld/internal/app"
	"codeworld/internal/approvals"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/session"
)

type Options struct {
	Images       []model.ContentPart
	JSON         bool
	Ephemeral    bool
	OutputSchema []byte
	Color        bool
}

type Result struct {
	FinalText string
	Usage     model.Usage
	SessionID string
}

// Once 提供对外可复用的能力，并隐藏内部实现细节。
func Once(ctx context.Context, rt *app.Runtime, input string) error {
	return OnceWithImages(ctx, rt, input, nil)
}

// OnceWithImages 执行一次非交互任务，并把图片 content parts 附加到用户消息。
func OnceWithImages(ctx context.Context, rt *app.Runtime, input string, imageParts []model.ContentPart) error {
	if input == "" {
		return fmt.Errorf("run input is empty")
	}
	rt.Runner.Reporter = reporter{out: rt.Out}
	rt.Runner.Confirmer = nonInteractiveConfirmer{approvals: rt.Session.Approvals}
	userMessage := model.Message{Role: model.RoleUser, Content: input}
	if len(imageParts) > 0 {
		userMessage.Parts = append([]model.ContentPart{model.TextPart(input)}, imageParts...)
	}
	result, err := rt.Runner.RunTurnMessage(ctx, rt.Messages, userMessage)
	if err != nil {
		if len(result.Messages) > 0 || !result.Usage.IsZero() {
			return errors.Join(err, rt.SaveTurn(result))
		}
		return err
	}
	if err := rt.SaveTurn(result); err != nil {
		return err
	}
	if err := rt.MaybeSummarize(ctx); err != nil && rt.Err != nil {
		_, _ = fmt.Fprintf(rt.Err, "summary warning: %v\n", err)
	}
	_, err = fmt.Fprintln(rt.Out, result.FinalText)
	return err
}

// Execute 执行一次面向脚本的任务：普通模式把进度写入 stderr，JSON 模式把事件流写入 stdout。
func Execute(ctx context.Context, rt *app.Runtime, input string, opts Options) (Result, error) {
	if input == "" {
		return Result{}, fmt.Errorf("exec input is empty")
	}
	modelInput := input
	if len(opts.OutputSchema) > 0 {
		modelInput += "\n\n" + structuredOutputInstruction(opts.OutputSchema)
	}
	userMessage := model.Message{Role: model.RoleUser, Content: modelInput}
	if len(opts.Images) > 0 {
		userMessage.Parts = append([]model.ContentPart{model.TextPart(modelInput)}, opts.Images...)
	}

	var events *jsonEventWriter
	if opts.JSON {
		events = newJSONEventWriter(rt.Out)
		rt.Runner.Reporter = events
		if err := events.emit(map[string]any{"type": "thread.started", "thread_id": rt.Session.ID}); err != nil {
			return Result{}, err
		}
		if err := events.emit(map[string]any{"type": "turn.started"}); err != nil {
			return Result{}, err
		}
	} else {
		rt.Runner.Reporter = reporter{out: writerOrDiscard(rt.Err), color: opts.Color}
	}
	rt.Runner.Confirmer = nonInteractiveConfirmer{approvals: rt.Session.Approvals}

	emit := func(event agent.TurnEvent) error {
		if events == nil {
			return nil
		}
		return events.turnEvent(event)
	}
	turn, runErr := rt.Runner.RunTurnStreamMessage(ctx, rt.Messages, userMessage, emit)
	result := Result{FinalText: turn.FinalText, Usage: turn.Usage, SessionID: rt.Session.ID}
	if runErr != nil {
		if !opts.Ephemeral && (len(turn.Messages) > 0 || !turn.Usage.IsZero()) {
			runErr = errors.Join(runErr, rt.SaveTurn(turn))
		}
		if events != nil {
			_ = events.emit(map[string]any{"type": "turn.failed", "message": runErr.Error()})
		}
		return result, runErr
	}

	if !opts.Ephemeral {
		if err := rt.SaveTurn(turn); err != nil {
			if events != nil {
				_ = events.emit(map[string]any{"type": "turn.failed", "message": err.Error()})
			}
			return result, err
		}
		if err := rt.MaybeSummarize(ctx); err != nil && rt.Err != nil {
			_, _ = fmt.Fprintf(rt.Err, "summary warning: %v\n", err)
		}
	}
	if len(opts.OutputSchema) > 0 {
		if err := ValidateStructuredOutput(opts.OutputSchema, turn.FinalText); err != nil {
			if events != nil {
				_ = events.emit(map[string]any{"type": "turn.failed", "message": err.Error()})
			}
			return result, err
		}
	}
	if events != nil {
		if err := events.emit(map[string]any{
			"type":       "turn.completed",
			"final_text": turn.FinalText,
			"usage":      turn.Usage,
		}); err != nil {
			return result, err
		}
		return result, nil
	}
	_, err := fmt.Fprintln(rt.Out, turn.FinalText)
	return result, err
}

func writerOrDiscard(writer io.Writer) io.Writer {
	if writer == nil {
		return io.Discard
	}
	return writer
}

type reporter struct {
	out interface {
		Write([]byte) (int, error)
	}
	color bool
}

// ReportTool 把工具执行状态转换为用户可见的进度事件。
func (r reporter) ReportTool(ctx context.Context, event agent.ToolEvent) {
	if r.out == nil {
		return
	}
	switch event.Status {
	case agent.ToolEventStart:
		r.write("\x1b[36m", "tool> %s target=%s risk=%s\n", event.Name, event.Request.Target, event.Request.Risk)
	case agent.ToolEventSuccess:
		r.write("\x1b[32m", "tool< %s ok\n", event.Name)
	case agent.ToolEventDenied:
		r.write("\x1b[33m", "tool< %s denied: %s\n", event.Name, event.Error)
	case agent.ToolEventError:
		r.write("\x1b[31m", "tool< %s error: %s\n", event.Name, event.Error)
	}
}

func (r reporter) write(color, format string, args ...any) {
	if r.color {
		_, _ = fmt.Fprint(r.out, color)
	}
	_, _ = fmt.Fprintf(r.out, format, args...)
	if r.color {
		_, _ = fmt.Fprint(r.out, "\x1b[0m")
	}
}

type nonInteractiveConfirmer struct {
	approvals []session.Approval
}

// Confirm 向用户确认权限请求，并把选择返回给 agent 流程。
func (c nonInteractiveConfirmer) Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if req.Action == permissions.ActionShell && approvalSet(c.approvals).Allows(req.Target) {
		return true, nil
	}
	return false, nil
}

// approvalSet 封装局部逻辑，保持调用方流程清晰。
func approvalSet(items []session.Approval) approvals.Set {
	set := approvals.Set{}
	for _, item := range items {
		if item.Kind == "shell" {
			set.Add(item.Command)
		}
	}
	return set
}
