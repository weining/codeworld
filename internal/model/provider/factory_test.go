package provider

import (
	"path/filepath"
	"strings"
	"testing"
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

// TestNewClientRejectsUnknownProvider 验证对应场景的行为，避免后续改动破坏既有约束。
func TestNewClientRejectsUnknownProvider(t *testing.T) {
	_, err := NewClient(Config{Provider: "unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("err = %v, want unknown provider", err)
	}
}
