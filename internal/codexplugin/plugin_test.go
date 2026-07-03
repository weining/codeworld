package codexplugin

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadProjectPluginsLoadsBundledSkills 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadProjectPluginsLoadsBundledSkills(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "super", ".codex-plugin/plugin.json", `{"name":"super","version":"0.1.0"}`)
	writePluginFile(t, root, "super", "skills/reviewer/SKILL.md", `---
name: reviewer
description: Review Go changes.
---

Run focused tests.
`)

	plugins, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject returned error: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("plugins = %#v, want one plugin", plugins)
	}
	if plugins[0].Name != "super" {
		t.Fatalf("plugin name = %q, want super", plugins[0].Name)
	}
	if len(plugins[0].Skills) != 1 || plugins[0].Skills[0].Name != "reviewer" || plugins[0].Skills[0].Source != "codex-plugin:super" {
		t.Fatalf("skills = %#v, want reviewer from plugin", plugins[0].Skills)
	}
}

// TestLoadProjectPluginsLoadsMCPServers 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadProjectPluginsLoadsMCPServers(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "docs", ".codex-plugin/plugin.json", `{"name":"docs"}`)
	writePluginFile(t, root, "docs", ".mcp.json", `{
		"mcp_servers": [{
			"name": "context7",
			"command": "npx",
			"args": ["-y", "@upstash/context7-mcp"]
		}]
	}`)

	plugins, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject returned error: %v", err)
	}
	if len(plugins) != 1 || len(plugins[0].MCPServers) != 1 {
		t.Fatalf("plugins = %#v, want one MCP server", plugins)
	}
	server := plugins[0].MCPServers[0]
	if server.Name != "docs.context7" || server.Command != "npx" || len(server.Args) != 2 || server.Args[1] != "@upstash/context7-mcp" {
		t.Fatalf("server = %#v, want namespaced stdio server", server)
	}
}

// TestLoadProjectPluginsRejectsManifestNameMismatch 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadProjectPluginsRejectsManifestNameMismatch(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "actual", ".codex-plugin/plugin.json", `{"name":"other"}`)

	_, err := LoadProject(root)
	if err == nil {
		t.Fatalf("LoadProject accepted mismatched plugin manifest name")
	}
}

// writePluginFile 是测试辅助函数，用于复用测试准备或断言逻辑。
func writePluginFile(t *testing.T, root, pluginName, rel, content string) {
	t.Helper()
	path := filepath.Join(root, ".codeworld", "codex-plugins", pluginName, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
