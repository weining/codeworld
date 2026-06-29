package tui

import (
	"context"

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
