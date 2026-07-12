package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetUserFeaturePreservesConfigAndLoadsValue(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	original := "# keep this comment\nmodel = \"demo\"\n\n[[mcp_servers]]\nname = \"docs\"\ncommand = \"docs-server\"\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetUserFeature(home, "plugins", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "# keep this comment") || strings.Index(text, "plugins_enabled = true") > strings.Index(text, "[[mcp_servers]]") {
		t.Fatalf("config = %s", text)
	}
	cfg, err := LoadWithOptions(t.TempDir(), LoadOptions{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PluginsEnabled || cfg.Model != "demo" || len(cfg.MCPServers) != 1 {
		t.Fatalf("config = %#v", cfg)
	}
	if err := SetUserFeature(home, "plugins", false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Count(string(data), "plugins_enabled") != 1 || !strings.Contains(string(data), "plugins_enabled = false") {
		t.Fatalf("updated config = %s", data)
	}
}
