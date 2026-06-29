package tui

import (
	"context"
	"fmt"

	"codeworld/internal/agent"
	"codeworld/internal/app"
)

type RunnerAdapter struct {
	rt *app.Runtime
}

func NewRunnerAdapter(rt *app.Runtime) RunnerAdapter {
	return RunnerAdapter{rt: rt}
}

func (a RunnerAdapter) RunTurn(ctx context.Context, input string) turnDoneMsg {
	result, err := a.rt.Runner.RunTurn(ctx, a.rt.Messages, input)
	if err != nil {
		return turnDoneMsg{err: err}
	}
	if err := a.rt.SaveTurn(result); err != nil {
		return turnDoneMsg{err: err}
	}
	if err := a.rt.MaybeSummarize(ctx); err != nil {
		return turnDoneMsg{text: result.FinalText, err: err}
	}
	return turnDoneMsg{text: result.FinalText}
}

type ToolReporter struct {
	events chan TranscriptItem
}

func NewToolReporter() *ToolReporter {
	return &ToolReporter{events: make(chan TranscriptItem, 32)}
}

func (r *ToolReporter) Events() <-chan TranscriptItem {
	return r.events
}

func (r *ToolReporter) ReportTool(ctx context.Context, event agent.ToolEvent) {
	text := fmt.Sprintf("%s %s target=%s risk=%s", event.Name, event.Status, event.Request.Target, event.Request.Risk)
	if event.Error != "" {
		text += " error=" + event.Error
	}
	select {
	case r.events <- TranscriptItem{Kind: ItemTool, Text: text}:
	case <-ctx.Done():
	}
}
