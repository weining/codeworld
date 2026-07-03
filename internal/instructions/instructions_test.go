package instructions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadPrefersOverrideInSameDirectory 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadPrefersOverrideInSameDirectory(t *testing.T) {
	root := t.TempDir()
	writeInstructionFile(t, root, "AGENTS.md", "base instruction")
	writeInstructionFile(t, root, "AGENTS.override.md", "override instruction")

	loaded, err := Load(root, Options{MaxBytes: 32 * 1024})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Files) != 1 {
		t.Fatalf("files = %#v, want one selected instruction file", loaded.Files)
	}
	if filepath.Base(loaded.Files[0].Path) != "AGENTS.override.md" {
		t.Fatalf("selected file = %q, want override", loaded.Files[0].Path)
	}
	if !strings.Contains(loaded.Text, "override instruction") || strings.Contains(loaded.Text, "base instruction") {
		t.Fatalf("text = %q, want override text only", loaded.Text)
	}
}

// TestLoadOrdersParentBeforeChild 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadOrdersParentBeforeChild(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "services", "api")
	writeInstructionFile(t, root, "AGENTS.md", "root rules")
	writeInstructionFile(t, filepath.Join(root, "services"), "AGENTS.md", "service rules")
	writeInstructionFile(t, child, "AGENTS.md", "api rules")
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	loaded, err := Load(child, Options{MaxBytes: 32 * 1024})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	rootIdx := strings.Index(loaded.Text, "root rules")
	serviceIdx := strings.Index(loaded.Text, "service rules")
	apiIdx := strings.Index(loaded.Text, "api rules")
	if rootIdx < 0 || serviceIdx < 0 || apiIdx < 0 || !(rootIdx < serviceIdx && serviceIdx < apiIdx) {
		t.Fatalf("text order = %q, want root before service before api", loaded.Text)
	}
}

// TestLoadAppliesByteBudget 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadAppliesByteBudget(t *testing.T) {
	root := t.TempDir()
	writeInstructionFile(t, root, "AGENTS.md", strings.Repeat("a", 64))

	loaded, err := Load(root, Options{MaxBytes: 16})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(loaded.Text) > 80 {
		t.Fatalf("text length = %d, want capped text with marker", len(loaded.Text))
	}
	if !loaded.Truncated || !strings.Contains(loaded.Text, "[instructions truncated]") {
		t.Fatalf("loaded = %#v, want truncation marker", loaded)
	}
}

// writeInstructionFile 是测试辅助函数，用于复用测试准备或断言逻辑。
func writeInstructionFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
