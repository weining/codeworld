package run

import (
	"context"
	"fmt"

	"codeworld/internal/agent"
	"codeworld/internal/app"
	"codeworld/internal/approvals"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/session"
)

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

type reporter struct {
	out interface {
		Write([]byte) (int, error)
	}
}

// ReportTool 把工具执行状态转换为用户可见的进度事件。
func (r reporter) ReportTool(ctx context.Context, event agent.ToolEvent) {
	if r.out == nil {
		return
	}
	switch event.Status {
	case agent.ToolEventStart:
		_, _ = fmt.Fprintf(r.out, "tool> %s target=%s risk=%s\n", event.Name, event.Request.Target, event.Request.Risk)
	case agent.ToolEventSuccess:
		_, _ = fmt.Fprintf(r.out, "tool< %s ok\n", event.Name)
	case agent.ToolEventDenied:
		_, _ = fmt.Fprintf(r.out, "tool< %s denied: %s\n", event.Name, event.Error)
	case agent.ToolEventError:
		_, _ = fmt.Fprintf(r.out, "tool< %s error: %s\n", event.Name, event.Error)
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
