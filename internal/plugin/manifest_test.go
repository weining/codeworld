package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadManifestsDisabledReturnsNoTools 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadManifestsDisabledReturnsNoTools(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "example", `{"name":"example","tools":[{"name":"example.echo","description":"Echo","command":"echo","args":["{{text}}"],"input_schema":{"type":"object"},"risk":"read"}]}`)

	tools, err := LoadManifests(root, false)
	if err != nil {
		t.Fatalf("LoadManifests returned error: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %#v, want none while disabled", tools)
	}
}

// TestLoadManifestsRequiresNamespacedToolNames 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadManifestsRequiresNamespacedToolNames(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "example", `{"name":"example","tools":[{"name":"echo","description":"Echo","command":"echo","args":["{{text}}"],"input_schema":{"type":"object"},"risk":"read"}]}`)

	_, err := LoadManifests(root, true)
	if err == nil {
		t.Fatalf("LoadManifests accepted unnamespaced tool")
	}
}

// TestLoadManifestsRejectsTraversalPluginName 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadManifestsRejectsTraversalPluginName(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "example", `{"name":"../escape","tools":[]}`)

	_, err := LoadManifests(root, true)
	if err == nil {
		t.Fatalf("LoadManifests accepted traversal plugin name")
	}
}

// TestLoadManifestsReturnsToolsWhenEnabled 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadManifestsReturnsToolsWhenEnabled(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "example", `{"name":"example","tools":[{"name":"example.echo","description":"Echo","command":"echo","args":["{{text}}"],"input_schema":{"type":"object"},"risk":"read"}]}`)

	tools, err := LoadManifests(root, true)
	if err != nil {
		t.Fatalf("LoadManifests returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "example.echo" || tools[0].Command != "echo" {
		t.Fatalf("tools = %#v, want example.echo", tools)
	}
}

// writeManifest 是测试辅助函数，用于复用测试准备或断言逻辑。
func writeManifest(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, ".codeworld", "plugins", name, "plugin.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
