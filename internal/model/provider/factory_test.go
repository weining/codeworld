package provider

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeworld/internal/codexauth"
)

// TestNewClientCreatesDeepSeekByDefault 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewClientCreatesDeepSeekByDefault(t *testing.T) {
	client, err := NewClient(Config{
		Provider:       "deepseek",
		Model:          "deepseek-v4-pro",
		DeepSeekAPIKey: "deepseek-key",
		LogPath:        filepath.Join(t.TempDir(), "calls.jsonl"),
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("client nil")
	}
}

// TestNewClientCreatesOpenAIClient 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewClientCreatesOpenAIClient(t *testing.T) {
	client, err := NewClient(Config{
		Provider:     "openai",
		Model:        "gpt-4.1",
		OpenAIAPIKey: "openai-key",
		LogPath:      filepath.Join(t.TempDir(), "calls.jsonl"),
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("client nil")
	}
}

// TestNewClientCreatesLocalClientWithoutAPIKey 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewClientCreatesLocalClientWithoutAPIKey(t *testing.T) {
	client, err := NewClient(Config{
		Provider:     "local",
		Model:        "local-model",
		LocalBaseURL: "http://127.0.0.1:11434/v1",
		LogPath:      filepath.Join(t.TempDir(), "calls.jsonl"),
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("client nil")
	}
}

// TestNewClientCreatesCodexClientFromStoredOAuth 验证 provider=codex 使用 workspace 中的 OAuth 凭据。
func TestNewClientCreatesCodexClientFromStoredOAuth(t *testing.T) {
	t.Setenv("CODEWORLD_HOME", t.TempDir())
	root := t.TempDir()
	authDir := filepath.Join(root, ".codeworld", "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "codex.json"), []byte(`{
		"access":"`+makeProviderJWT("acct-1")+`",
		"refresh":"refresh-token",
		"expires":4102444800000,
		"account_id":"acct-1"
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	client, err := NewClient(Config{
		Provider: "codex",
		Model:    "gpt-5",
		Root:     root,
		LogPath:  filepath.Join(t.TempDir(), "calls.jsonl"),
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("client nil")
	}
}

func TestNewClientCreatesCodexClientFromUserOAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEWORLD_HOME", home)
	if err := codexauth.UserStore(home).Save(codexauth.Credentials{
		Access: makeProviderJWT("acct-user"), Refresh: "refresh-token", Expires: 4102444800000, AccountID: "acct-user",
	}); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Config{Provider: "codex", Model: "gpt-5", Root: t.TempDir()})
	if err != nil || client == nil {
		t.Fatalf("client=%#v err=%v", client, err)
	}
}

// TestNewClientCreatesAnthropicClient 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewClientCreatesAnthropicClient(t *testing.T) {
	client, err := NewClient(Config{
		Provider:        "anthropic",
		Model:           "claude-sonnet-4-5",
		AnthropicAPIKey: "anthropic-key",
		LogPath:         filepath.Join(t.TempDir(), "calls.jsonl"),
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if client == nil {
		t.Fatalf("client nil")
	}
}

func TestNewClientCreatesWireFormatClients(t *testing.T) {
	tests := []Config{
		{Provider: "profile:openai-compatible", APIFormat: "openai", Model: "custom-model", BaseURL: "http://127.0.0.1:11434/v1"},
		{Provider: "profile:anthropic-compatible", APIFormat: "anthropic", Model: "custom-model", BaseURL: "https://example.com", APIKey: "secret"},
		{Provider: "profile:gemini", APIFormat: "gemini", Model: "gemini-test", BaseURL: "https://generativelanguage.googleapis.com/v1beta", APIKey: "secret"},
	}
	for _, cfg := range tests {
		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient(%s): %v", cfg.APIFormat, err)
		}
		if client == nil {
			t.Fatalf("NewClient(%s) returned nil", cfg.APIFormat)
		}
	}
}

func TestNewClientValidatesWireFormatCredentials(t *testing.T) {
	for _, format := range []string{"anthropic", "gemini"} {
		_, err := NewClient(Config{Provider: "profile:test", APIFormat: format, Model: "test", BaseURL: "https://example.com"})
		if err == nil || !strings.Contains(err.Error(), "API key is not set") {
			t.Fatalf("format=%s err=%v, want missing API key", format, err)
		}
	}
	if _, err := NewClient(Config{Provider: "profile:test", APIFormat: "unknown", Model: "test"}); err == nil || !strings.Contains(err.Error(), "unknown API format") {
		t.Fatalf("err=%v, want unknown API format", err)
	}
}

// TestNewClientRejectsMissingAnthropicAPIKey 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewClientRejectsMissingAnthropicAPIKey(t *testing.T) {
	_, err := NewClient(Config{Provider: "anthropic", Model: "claude-sonnet-4-5"})
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY is not set") {
		t.Fatalf("err = %v, want missing Anthropic key", err)
	}
}

// TestNewClientRejectsMissingOpenAIAPIKey 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewClientRejectsMissingOpenAIAPIKey(t *testing.T) {
	_, err := NewClient(Config{Provider: "openai", Model: "gpt-4.1"})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY is not set") {
		t.Fatalf("err = %v, want missing OpenAI key", err)
	}
}

// TestNewClientRejectsMissingCodexOAuth 验证缺少 Codex OAuth 凭据时给出可执行提示。
func TestNewClientRejectsMissingCodexOAuth(t *testing.T) {
	t.Setenv("CODEWORLD_HOME", t.TempDir())
	_, err := NewClient(Config{Provider: "codex", Model: "gpt-5", Root: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "codeworld auth codex login") {
		t.Fatalf("err = %v, want codex login hint", err)
	}
}

// TestNewClientRejectsUnknownProvider 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewClientRejectsUnknownProvider(t *testing.T) {
	_, err := NewClient(Config{Provider: "unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("err = %v, want unknown provider", err)
	}
}

// makeProviderJWT 构造只供 provider factory 测试使用的 Codex JWT。
func makeProviderJWT(accountID string) string {
	payload := `{"https://api.openai.com/auth":{"chatgpt_account_id":"` + accountID + `"}}`
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
}
