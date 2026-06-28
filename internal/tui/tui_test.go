package tui

import (
	"strings"
	"testing"

	"codeworld/internal/model"
)

func TestStatusLineShowsTokenBreakdown(t *testing.T) {
	line := StatusLine(Snapshot{
		Workspace: "repo",
		Provider:  "deepseek",
		Model:     "deepseek-v4-pro",
		Messages:  5,
		Approvals: 2,
		Usage:     model.Usage{InputTokens: 10, OutputTokens: 3, CacheTokens: 4, TotalTokens: 13},
	})

	for _, want := range []string{
		"workspace=repo",
		"provider=deepseek",
		"model=deepseek-v4-pro",
		"messages=5",
		"approvals=2",
		"input=10",
		"output=3",
		"cache=4",
		"total=13",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("status line missing %q in %q", want, line)
		}
	}
}
