package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"codeworld/internal/app"
	"codeworld/internal/model"
	"codeworld/internal/session"
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
