package capability

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/workspace"
)

func TestLoadCombinesPluginToolsAndSkillContext(t *testing.T) {
	root := t.TempDir()
	writeCapabilityFile(t, filepath.Join(root, ".codeworld", "plugins", "demo", "plugin.json"), `{
		"name": "demo",
		"tools": [{
			"name": "demo.echo",
			"description": "echo input",
			"command": "printf",
			"args": ["{{text}}"],
			"input_schema": {"type": "object"},
			"risk": "read"
		}]
	}`)
	writeCapabilityFile(t, filepath.Join(root, ".codeworld", "skills", "reviewer", "SKILL.md"), `---
name: reviewer
description: Review Go changes.
---

Run tests before completion.
`)
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}

	loaded, err := Load(context.Background(), Options{
		Root:           ws.Root,
		Workspace:      ws,
		PluginsEnabled: true,
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Tools) != 1 || loaded.Tools[0].Definition().Name != "demo.echo" {
		t.Fatalf("tools = %#v, want demo.echo", loaded.Tools)
	}
	if len(loaded.Skills) != 1 || loaded.Skills[0].Name != "reviewer" {
		t.Fatalf("skills = %#v, want reviewer", loaded.Skills)
	}
	if !strings.Contains(loaded.SkillContext, "Project skills:") || !strings.Contains(loaded.SkillContext, "Run tests before completion.") {
		t.Fatalf("skill context = %q, want loaded skill text", loaded.SkillContext)
	}
}

func writeCapabilityFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
