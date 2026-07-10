package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadAndRunCommandHook 验证 hooks.json 中的命令 hook 会按事件执行。
func TestLoadAndRunCommandHook(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "hook.out")
	if err := os.MkdirAll(filepath.Join(root, ".codeworld"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codeworld", "hooks.json"), []byte(`{
		"hooks": {
			"PreToolUse": [
				{"hooks": [{"type": "command", "command": "printf pre >> hook.out"}]}
			]
		}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	runner, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := runner.Run(context.Background(), "PreToolUse", Context{Tool: "shell"}); err == nil {
		t.Fatal("Run should reject unauthorized workspace hooks")
	}
	runner.Enable()
	if err := runner.Run(context.Background(), "PreToolUse", Context{Tool: "shell"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.TrimSpace(string(data)) != "pre" {
		t.Fatalf("hook output = %q, want pre", data)
	}
}
