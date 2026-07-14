package llmprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreCRUDAndPrivatePermissions(t *testing.T) {
	home := t.TempDir()
	store, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	profile := Profile{ID: "moonshot", Name: "Moonshot", APIFormat: FormatOpenAI, BaseURL: "https://api.moonshot.cn/v1/", Model: "kimi-k2", APIKeyEnv: "MOONSHOT_API_KEY"}
	if err := store.Upsert(profile); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := loaded.Get("moonshot")
	if !ok || got.BaseURL != "https://api.moonshot.cn/v1" || got.Model != "kimi-k2" {
		t.Fatalf("profile = %#v, ok=%v", got, ok)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if err := loaded.Delete("moonshot"); err != nil {
		t.Fatal(err)
	}
	if len(loaded.List()) != 0 {
		t.Fatalf("profiles = %#v", loaded.List())
	}
}

func TestValidateFormatsAndEnvironment(t *testing.T) {
	for _, profile := range []Profile{
		{ID: "bad id", APIFormat: FormatOpenAI, Model: "m"},
		{ID: "demo", APIFormat: "unknown", Model: "m"},
		{ID: "demo", APIFormat: FormatOpenAI, BaseURL: "file:///tmp/model", Model: "m"},
		{ID: "demo", APIFormat: FormatGemini, Model: "m", APIKeyEnv: "bad-key"},
		{ID: "demo", APIFormat: FormatGemini, Model: "m"},
		{ID: "demo", APIFormat: FormatAnthropic, Model: "m"},
		{ID: "demo", APIFormat: FormatCodex, BaseURL: "not-a-url", Model: "m"},
		{ID: "demo", APIFormat: FormatCodex, Model: "m", APIKeyEnv: "OPENAI_API_KEY"},
	} {
		if err := Validate(Normalize(profile)); err == nil {
			t.Fatalf("accepted %#v", profile)
		}
	}
	if err := Validate(Normalize(Profile{ID: "claude", APIFormat: FormatAnthropic, Model: "claude-test", APIKeyEnv: "ANTHROPIC_API_KEY"})); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRollsBackMemoryWhenPersistenceFails(t *testing.T) {
	root := t.TempDir()
	blockedHome := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedHome, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &Store{
		home: blockedHome,
		profiles: []Profile{{
			ID: "existing", APIFormat: FormatOpenAI, BaseURL: "http://127.0.0.1:11434/v1", Model: "model",
		}},
	}
	if err := store.Upsert(Profile{ID: "new", APIFormat: FormatOpenAI, BaseURL: "http://127.0.0.1:11434/v1", Model: "model"}); err == nil {
		t.Fatal("Upsert unexpectedly persisted through a file path")
	}
	if profiles := store.List(); len(profiles) != 1 || profiles[0].ID != "existing" {
		t.Fatalf("profiles changed after failed Upsert: %#v", profiles)
	}
	if err := store.Delete("existing"); err == nil {
		t.Fatal("Delete unexpectedly persisted through a file path")
	}
	if profiles := store.List(); len(profiles) != 1 || profiles[0].ID != "existing" {
		t.Fatalf("profiles changed after failed Delete: %#v", profiles)
	}
}
