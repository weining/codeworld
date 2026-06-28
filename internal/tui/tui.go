package tui

import (
	"context"
	"fmt"

	"codeworld/internal/app"
	"codeworld/internal/model"
	"codeworld/internal/repl"
)

type Snapshot struct {
	Workspace string
	Provider  string
	Model     string
	Messages  int
	Approvals int
	Usage     model.Usage
}

func RuntimeSnapshot(rt *app.Runtime) Snapshot {
	return Snapshot{
		Workspace: rt.Workspace.Root,
		Provider:  rt.Session.Provider,
		Model:     rt.Session.Model,
		Messages:  len(rt.Messages),
		Approvals: len(rt.Session.Approvals),
		Usage:     rt.Usage,
	}
}

func StatusLine(snapshot Snapshot) string {
	return fmt.Sprintf("workspace=%s provider=%s model=%s messages=%d approvals=%d tokens input=%d output=%d cache=%d total=%d",
		snapshot.Workspace,
		snapshot.Provider,
		snapshot.Model,
		snapshot.Messages,
		snapshot.Approvals,
		snapshot.Usage.InputTokens,
		snapshot.Usage.OutputTokens,
		snapshot.Usage.CacheTokens,
		snapshot.Usage.TotalTokens,
	)
}

func Run(ctx context.Context, rt *app.Runtime) error {
	fmt.Fprintln(rt.Out, "codeworld tui")
	fmt.Fprintln(rt.Out, StatusLine(RuntimeSnapshot(rt)))
	shell := repl.REPL{
		In:                 rt.In,
		Out:                rt.Out,
		Runner:             rt.Runner,
		Store:              rt.Store,
		Session:            rt.Session,
		Messages:           rt.Messages,
		Usage:              rt.Usage,
		SummaryMaxMessages: rt.Config.SummaryMaxMessages,
		ShowStatusLine:     true,
		Diff:               rt.Diff,
	}
	return shell.Run(ctx)
}
