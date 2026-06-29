package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"codeworld/internal/app"
	"codeworld/internal/model"
)

type Options struct {
	TestMode bool
}

type Status struct {
	Workspace string
	Provider  string
	Model     string
	Messages  int
	Approvals int
	Usage     model.Usage
	Running   bool
}

func RuntimeStatus(rt *app.Runtime) Status {
	return Status{
		Workspace: rt.Workspace.Root,
		Provider:  rt.Session.Provider,
		Model:     rt.Session.Model,
		Messages:  len(rt.Messages),
		Approvals: len(rt.Session.Approvals),
		Usage:     rt.Usage,
	}
}

func StatusLine(status Status) string {
	running := ""
	if status.Running {
		running = " running"
	}
	return fmt.Sprintf("workspace=%s provider=%s model=%s messages=%d approvals=%d tokens input=%d output=%d cache=%d total=%d%s",
		status.Workspace,
		status.Provider,
		status.Model,
		status.Messages,
		status.Approvals,
		status.Usage.InputTokens,
		status.Usage.OutputTokens,
		status.Usage.CacheTokens,
		status.Usage.TotalTokens,
		running,
	)
}

func Run(ctx context.Context, rt *app.Runtime) error {
	return RunWithOptions(ctx, rt, Options{})
}

func RunWithOptions(ctx context.Context, rt *app.Runtime, opts Options) error {
	m := NewModel(rt)
	if opts.TestMode {
		_, err := fmt.Fprint(rt.Out, m.View())
		return err
	}
	programOpts := []tea.ProgramOption{tea.WithContext(ctx), tea.WithAltScreen()}
	_, err := tea.NewProgram(m, programOpts...).Run()
	return err
}
