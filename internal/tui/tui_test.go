package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"codeworld/internal/agent"
	"codeworld/internal/app"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/session"
	"codeworld/internal/tools"
	"codeworld/internal/workspace"
)

func TestStatusLineShowsTokenBreakdown(t *testing.T) {
	line := StatusLine(Status{
		Workspace: "repo",
		Provider:  "deepseek",
		Model:     "deepseek-v4-pro",
		Messages:  5,
		Approvals: 2,
		Usage:     model.Usage{InputTokens: 10, OutputTokens: 3, CacheTokens: 4, TotalTokens: 13},
	})

	for _, want := range []string{"workspace=repo", "provider=deepseek", "model=deepseek-v4-pro", "messages=5", "approvals=2", "input=10", "output=3", "cache=4", "total=13"} {
		if !strings.Contains(line, want) {
			t.Fatalf("status line missing %q in %q", want, line)
		}
	}
}

func TestRunReturnsWithoutTerminalRendererInTestMode(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Usage:     model.Usage{InputTokens: 1, OutputTokens: 2, CacheTokens: 3, TotalTokens: 4},
		In:        strings.NewReader("/exit\n"),
		Out:       &out,
	}

	err := RunWithOptions(context.Background(), &rt, Options{TestMode: true})
	if err != nil {
		t.Fatalf("RunWithOptions returned error: %v", err)
	}
	if !strings.Contains(out.String(), "deepseek-v4-pro") {
		t.Fatalf("output = %q, want model status", out.String())
	}
}

func TestModelViewContainsTranscriptStatusAndComposer(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Usage:     model.Usage{InputTokens: 10, OutputTokens: 3, CacheTokens: 4, TotalTokens: 13},
	}
	m := NewModel(&rt)
	m.width = 100
	m.height = 24
	m.items = append(m.items, TranscriptItem{Kind: ItemAssistant, Text: "ready"})
	m.refreshViewport()

	view := m.View()
	for _, want := range []string{"codeworld", "deepseek-v4-pro", "input=10", "ready", ">"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q in:\n%s", want, view)
		}
	}
}

type fakeModelClient struct {
	resp model.GenerateResponse
}

func (f fakeModelClient) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	return f.resp, nil
}

func TestRunnerAdapterUpdatesRuntimeUsageAndMessages(t *testing.T) {
	root := t.TempDir()
	rt := app.Runtime{
		Workspace: workspace.Workspace{Root: root},
		Store:     session.NewStore(root),
		Session:   session.New(root, "deepseek", "deepseek-v4-pro"),
		Runner: agent.Runner{
			Model:     fakeModelClient{resp: model.GenerateResponse{FinalText: "done", Usage: model.Usage{InputTokens: 3, OutputTokens: 2, CacheTokens: 1, TotalTokens: 5}}},
			Tools:     tools.NewRegistry(nil, nil),
			Policy:    permissions.ConservativePolicy{},
			MaxSteps:  1,
			ModelName: "deepseek-v4-pro",
		},
	}

	adapter := NewRunnerAdapter(&rt)
	result := adapter.RunTurn(context.Background(), "hello")
	if result.err != nil {
		t.Fatalf("RunTurn error: %v", result.err)
	}
	if result.text != "done" {
		t.Fatalf("text = %q, want done", result.text)
	}
	if rt.Usage.InputTokens != 3 || rt.Usage.OutputTokens != 2 || rt.Usage.CacheTokens != 1 || rt.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v, want accumulated usage", rt.Usage)
	}
	if len(rt.Messages) == 0 {
		t.Fatalf("runtime messages were not updated")
	}
}

func TestToolReporterAppendsToolEvents(t *testing.T) {
	reporter := NewToolReporter()
	reporter.ReportTool(context.Background(), agent.ToolEvent{
		Status: agent.ToolEventStart,
		Name:   "shell",
		Request: permissions.Request{
			Target: "mise exec -- go test ./...",
			Risk:   permissions.RiskExecute,
		},
	})

	item := <-reporter.Events()
	if item.Kind != ItemTool || !strings.Contains(item.Text, "shell start") || !strings.Contains(item.Text, "go test") {
		t.Fatalf("item = %#v, want tool start transcript item", item)
	}
}

func TestTUIConfirmerAllowsSessionApproval(t *testing.T) {
	confirmer := NewTUIConfirmer()
	done := make(chan bool, 1)
	go func() {
		allowed, err := confirmer.Confirm(context.Background(), permissions.Request{
			Action: permissions.ActionShell,
			Target: "mise exec -- go test ./...",
			Risk:   permissions.RiskExecute,
			Reason: "shell command",
		}, permissions.Decision{Kind: permissions.DecisionAsk})
		if err != nil {
			t.Errorf("Confirm returned error: %v", err)
		}
		done <- allowed
	}()

	req := <-confirmer.Requests()
	if req.Target != "mise exec -- go test ./..." {
		t.Fatalf("request = %#v, want shell target", req)
	}
	confirmer.Decide(PermissionDecision{Allow: true, Session: true})
	if !<-done {
		t.Fatalf("confirm denied, want allowed")
	}
}
