package run

import (
	"context"
	"fmt"

	"codeworld/internal/agent"
	"codeworld/internal/app"
	"codeworld/internal/permissions"
)

func Once(ctx context.Context, rt *app.Runtime, input string) error {
	if input == "" {
		return fmt.Errorf("run input is empty")
	}
	rt.Runner.Reporter = reporter{out: rt.Out}
	rt.Runner.Confirmer = denyConfirmer{}
	result, err := rt.Runner.RunTurn(ctx, rt.Messages, input)
	if err != nil {
		return err
	}
	if err := rt.SaveTurn(result); err != nil {
		return err
	}
	_, err = fmt.Fprintln(rt.Out, result.FinalText)
	return err
}

type reporter struct {
	out interface {
		Write([]byte) (int, error)
	}
}

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

type denyConfirmer struct{}

func (denyConfirmer) Confirm(ctx context.Context, req permissions.Request, decision permissions.Decision) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, nil
}
