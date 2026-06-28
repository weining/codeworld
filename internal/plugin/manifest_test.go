package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestLoadManifestsRequiresNamespacedToolNames(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "example", `{"name":"example","tools":[{"name":"echo","description":"Echo","command":"echo","args":["{{text}}"],"input_schema":{"type":"object"},"risk":"read"}]}`)

	_, err := LoadManifests(root, true)
	if err == nil {
		t.Fatalf("LoadManifests accepted unnamespaced tool")
	}
}

func TestLoadManifestsRejectsTraversalPluginName(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "example", `{"name":"../escape","tools":[]}`)

	_, err := LoadManifests(root, true)
	if err == nil {
		t.Fatalf("LoadManifests accepted traversal plugin name")
	}
}

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
