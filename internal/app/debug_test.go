package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/model"
	"codeworld/internal/session"
)

func TestDebugPromptInputIncludesSystemHistoryAndPrompt(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("CODEWORLD_HOME", home)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Keep tests focused.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := session.NewStore(root)
	sess := session.New(root, "local", "demo")
	sess.Messages = []session.Message{{Role: "user", Content: "old question"}, {Role: "assistant", Content: "old answer"}}
	if err := store.SaveCurrent(sess); err != nil {
		t.Fatal(err)
	}
	messages, err := DebugPromptInput(root, "", nil, "new question", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || messages[0].Role != model.RoleSystem || !strings.Contains(messages[0].Content, "Keep tests focused.") || messages[3].Content != "new question" {
		t.Fatalf("messages = %#v", messages)
	}
}
