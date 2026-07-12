package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginCommandsManageLocalMarketplace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEWORLD_HOME", home)
	marketplace := filepath.Join(t.TempDir(), "sample-market")
	manifestDir := filepath.Join(marketplace, "plugins", "sample", ".codex-plugin")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "plugin.json"), []byte(`{"name":"sample","version":"1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runPluginCommand(&out, []string{"marketplace", "add", marketplace}); err != nil {
		t.Fatal(err)
	}
	if err := runPluginCommand(&out, []string{"list", "--available", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"plugin_id": "sample@sample-market"`) {
		t.Fatalf("list = %s", out.String())
	}
	if err := runPluginCommand(&out, []string{"add", "sample@sample-market"}); err != nil {
		t.Fatal(err)
	}
	if err := runPluginCommand(&out, []string{"remove", "sample", "--marketplace", "sample-market"}); err != nil {
		t.Fatal(err)
	}
	if err := runPluginCommand(&out, []string{"marketplace", "remove", "sample-market"}); err != nil {
		t.Fatal(err)
	}
}
