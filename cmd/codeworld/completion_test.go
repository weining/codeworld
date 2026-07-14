package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionDefaultsToBash(t *testing.T) {
	var out bytes.Buffer
	if err := runCompletionCommand(&out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "complete -F _codeworld_completion codeworld") {
		t.Fatalf("unexpected bash completion:\n%s", out.String())
	}
}

func TestCompletionScriptsIncludeContextAndEnumCandidates(t *testing.T) {
	tests := map[string][]string{
		"bash":       {"auth:codex", "plugin:marketplace", "execpolicy:check", "app-server", "--listen --stdio", completionApprovalModes, completionShells},
		"elvish":     {"&'codeworld;mcp'", "&'codeworld;plugin;marketplace'", "&'codeworld;execpolicy;check'", "cand bash", "cand zsh"},
		"fish":       {"__fish_seen_subcommand_from mcp", "__fish_seen_subcommand_from plugin; and __fish_seen_subcommand_from marketplace", "__fish_seen_subcommand_from execpolicy; and __fish_seen_subcommand_from check", completionColorModes},
		"powershell": {"$elements[1]", "list get add remove login logout", "'execpolicy'", completionSandboxModes},
		"zsh":        {"auth:codex", "plugin:marketplace", "execpolicy:check", completionLocalProviders, completionColorModes},
	}
	for shell, fragments := range tests {
		t.Run(shell, func(t *testing.T) {
			var out bytes.Buffer
			if err := runCompletionCommand(&out, []string{shell}); err != nil {
				t.Fatal(err)
			}
			for _, fragment := range fragments {
				if !strings.Contains(out.String(), fragment) {
					t.Errorf("completion is missing %q", fragment)
				}
			}
		})
	}
}

func TestCompletionRejectsInvalidArguments(t *testing.T) {
	if err := runCompletionCommand(&bytes.Buffer{}, []string{"tcsh"}); err == nil {
		t.Fatal("unsupported shell accepted")
	}
	if err := runCompletionCommand(&bytes.Buffer{}, []string{"bash", "zsh"}); err == nil {
		t.Fatal("multiple shells accepted")
	}
}
