package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultConfig 验证对应场景的行为，避免后续改动破坏既有约束。
func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.Provider != "deepseek" {
		t.Fatalf("Provider = %q, want deepseek", cfg.Provider)
	}
	if cfg.Model != "deepseek-v4-pro" {
		t.Fatalf("Model = %q, want deepseek-v4-pro", cfg.Model)
	}
	if cfg.MaxSteps != 20 {
		t.Fatalf("MaxSteps = %d, want 20", cfg.MaxSteps)
	}
	if cfg.Workspace != "." {
		t.Fatalf("Workspace = %q, want .", cfg.Workspace)
	}
	if cfg.SummaryMaxMessages != 40 {
		t.Fatalf("SummaryMaxMessages = %d, want 40", cfg.SummaryMaxMessages)
	}
	if cfg.IndexMaxFileBytes != 256*1024 {
		t.Fatalf("IndexMaxFileBytes = %d, want 256 KiB", cfg.IndexMaxFileBytes)
	}
}

// TestLoadProjectConfig 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadProjectConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	writeFile(t, filepath.Join(dir, ".codeworld", "config.toml"), "provider = \"deepseek\"\nmodel = \"deepseek-v4-flash\"\nmax_steps = 7\nworkspace = \".\"\n")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Model != "deepseek-v4-flash" {
		t.Fatalf("Model = %q, want deepseek-v4-flash", cfg.Model)
	}
	if cfg.MaxSteps != 7 {
		t.Fatalf("MaxSteps = %d, want 7", cfg.MaxSteps)
	}
	if cfg.APIKey != "test-key" {
		t.Fatalf("APIKey was not loaded from environment")
	}
}

// TestLoadExtendedProjectConfig 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadExtendedProjectConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_API_KEY", "openai-key")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
	writeFile(t, filepath.Join(dir, ".codeworld", "config.toml"), strings.Join([]string{
		"provider = \"openai\"",
		"model = \"gpt-4.1\"",
		"local_base_url = \"http://127.0.0.1:11434/v1\"",
		"plugins_enabled = true",
		"summary_max_messages = 80",
		"index_max_file_bytes = 65536",
	}, "\n"))

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Provider != "openai" || cfg.Model != "gpt-4.1" {
		t.Fatalf("provider/model = %s/%s, want openai/gpt-4.1", cfg.Provider, cfg.Model)
	}
	if cfg.LocalBaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("LocalBaseURL = %q", cfg.LocalBaseURL)
	}
	if !cfg.PluginsEnabled {
		t.Fatalf("PluginsEnabled = false, want true")
	}
	if cfg.SummaryMaxMessages != 80 {
		t.Fatalf("SummaryMaxMessages = %d, want 80", cfg.SummaryMaxMessages)
	}
	if cfg.IndexMaxFileBytes != 65536 {
		t.Fatalf("IndexMaxFileBytes = %d, want 65536", cfg.IndexMaxFileBytes)
	}
	if cfg.OpenAIAPIKey != "openai-key" || cfg.AnthropicAPIKey != "anthropic-key" {
		t.Fatalf("provider keys not loaded from environment")
	}
}

// TestLoadMCPServers 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadMCPServers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".codeworld", "config.toml"), strings.Join([]string{
		"[[mcp_servers]]",
		"name = \"demo\"",
		"command = \"node\"",
		"args = [\"server.js\", \"--stdio\"]",
	}, "\n"))

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("MCPServers = %#v, want one server", cfg.MCPServers)
	}
	server := cfg.MCPServers[0]
	if server.Name != "demo" || server.Command != "node" || len(server.Args) != 2 || server.Args[0] != "server.js" || server.Args[1] != "--stdio" {
		t.Fatalf("server = %#v, want parsed MCP server", server)
	}
}

// TestLoadLocalBaseURLFromEnvironment 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadLocalBaseURLFromEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEWORLD_LOCAL_BASE_URL", "http://localhost:1234/v1")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.LocalBaseURL != "http://localhost:1234/v1" {
		t.Fatalf("LocalBaseURL = %q, want env value", cfg.LocalBaseURL)
	}
}

// TestLoadRejectsUnknownConfigKeys 验证对应场景的行为，避免后续改动破坏既有约束。
func TestLoadRejectsUnknownConfigKeys(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".codeworld", "config.toml"), "provider = \"deepseek\"\nextra = \"nope\"\n")

	if _, err := Load(dir); err == nil {
		t.Fatalf("Load accepted an unknown config key")
	}
}

// writeFile 是测试辅助函数，用于复用测试准备或断言逻辑。
func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
