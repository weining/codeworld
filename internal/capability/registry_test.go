package capability

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/workspace"
)

// TestLoadCombinesPluginToolsAndSkillContext 验证对应场景的行为，避免后续改动破坏既有约束。
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
	if !strings.Contains(loaded.SkillContext, "Available skills:") || !strings.Contains(loaded.SkillContext, "reviewer") {
		t.Fatalf("skill context = %q, want skill index", loaded.SkillContext)
	}
	if strings.Contains(loaded.SkillContext, "Run tests before completion.") {
		t.Fatalf("skill context = %q, should not include full skill body", loaded.SkillContext)
	}
}

// TestLoadIncludesCodexPluginSkills 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadIncludesCodexPluginSkills(t *testing.T) {
	root := t.TempDir()
	writeCapabilityFile(t, filepath.Join(root, ".codeworld", "codex-plugins", "super", ".codex-plugin", "plugin.json"), `{"name":"super"}`)
	writeCapabilityFile(t, filepath.Join(root, ".codeworld", "codex-plugins", "super", "skills", "reviewer", "SKILL.md"), `---
name: reviewer
description: Review Go changes.
---

Run focused tests.
`)
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}

	loaded, err := Load(context.Background(), Options{Root: ws.Root, Workspace: ws})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Skills) != 1 || loaded.Skills[0].Name != "reviewer" || loaded.Skills[0].Source != "codex-plugin:super" {
		t.Fatalf("skills = %#v, want reviewer from codex plugin", loaded.Skills)
	}
}

// writeCapabilityFile 是测试辅助函数，用于复用测试准备或断言逻辑。
func writeCapabilityFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
