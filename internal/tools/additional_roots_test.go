package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"codeworld/internal/workspace"
)

func TestWriteFileToolWritesExplicitAdditionalRoot(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()
	ws, err := workspace.NewWithAdditional(root, []string{extra})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(extra, "notes.txt")
	args, _ := json.Marshal(map[string]string{"path": target, "content": "explicitly allowed"})
	if _, err := NewWriteFileTool(ws).Execute(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "explicitly allowed" {
		t.Fatalf("content=%q err=%v", data, err)
	}
}
