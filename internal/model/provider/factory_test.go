package provider

import (
	"path/filepath"
	"strings"
	"testing"
)

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

func TestNewClientRejectsMissingOpenAIAPIKey(t *testing.T) {
	_, err := NewClient(Config{Provider: "openai", Model: "gpt-4.1"})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY is not set") {
		t.Fatalf("err = %v, want missing OpenAI key", err)
	}
}

func TestNewClientRejectsUnknownProvider(t *testing.T) {
	_, err := NewClient(Config{Provider: "unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("err = %v, want unknown provider", err)
	}
}
