package tui

import (
	"context"
	"fmt"

	"codeworld/internal/agent"
	"codeworld/internal/app"
)

type RunnerAdapter struct {
	rt     *app.Runtime
	events chan<- agent.TurnEvent
}

// NewRunnerAdapter 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewRunnerAdapter(rt *app.Runtime, events chan<- agent.TurnEvent) RunnerAdapter {
	return RunnerAdapter{rt: rt, events: events}
}

// RunTurn 执行主要流程，并把运行结果或错误返回给调用方。
func (a RunnerAdapter) RunTurn(ctx context.Context, input string) turnDoneMsg {
	result, err := a.rt.Runner.RunTurnStream(ctx, a.rt.Messages, input, func(event agent.TurnEvent) error {
		if a.events == nil {
			return nil
		}
		select {
		case a.events <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
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

// NewToolReporter 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewToolReporter() *ToolReporter {
	return &ToolReporter{events: make(chan TranscriptItem, 32)}
}

// Events 提供对外可复用的能力，并隐藏内部实现细节。
func (r *ToolReporter) Events() <-chan TranscriptItem {
	return r.events
}

// ReportTool 把工具执行状态转换为用户可见的进度事件。
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
