package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugModelsAndPromptInput(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("CODEWORLD_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("provider = \"local\"\nmodel = \"demo-model\"\nlocal_base_url = \"http://127.0.0.1:11434/v1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runDebugCommand(context.Background(), &out, root, globalOptions{}, []string{"models", "--bundled"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "demo-model") {
		t.Fatalf("models = %s", out.String())
	}
	out.Reset()
	if err := runDebugCommand(context.Background(), &out, root, globalOptions{}, []string{"prompt-input", "inspect"}); err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(out.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) < 2 || messages[0]["role"] != "system" || messages[len(messages)-1]["content"] != "inspect" {
		t.Fatalf("messages = %#v", messages)
	}
}
