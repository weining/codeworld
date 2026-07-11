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
	if cfg.SummaryMaxTokens != 64*1024 {
		t.Fatalf("SummaryMaxTokens = %d, want 65536", cfg.SummaryMaxTokens)
	}
	if cfg.ModelCallLogging {
		t.Fatal("ModelCallLogging = true, want secure default false")
	}
	if cfg.IndexMaxFileBytes != 256*1024 {
		t.Fatalf("IndexMaxFileBytes = %d, want 256 KiB", cfg.IndexMaxFileBytes)
	}
	if cfg.SandboxMode != "workspace-write" || cfg.SandboxNetwork {
		t.Fatalf("sandbox = %q network=%v, want workspace-write without network", cfg.SandboxMode, cfg.SandboxNetwork)
	}
}

func TestProjectConfigCannotDisableSandboxOrEnableNetwork(t *testing.T) {
	for name, body := range map[string]string{
		"full access": `sandbox_mode = "danger-full-access"`,
		"network":     `sandbox_network = true`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".codeworld", "config.toml"), body)
			if _, err := Load(root); err == nil {
				t.Fatal("expected unsafe project sandbox config to be rejected")
			}
		})
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
		"summary_max_tokens = 32000",
		"index_max_file_bytes = 65536",
		"model_call_logging = true",
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
	if cfg.SummaryMaxTokens != 32000 || !cfg.ModelCallLogging {
		t.Fatalf("summary/log config = %d/%v", cfg.SummaryMaxTokens, cfg.ModelCallLogging)
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

// TestLoadHTTPMCPServerConfig 验证 HTTP MCP 的地址、鉴权和 tool 过滤配置。
func TestLoadHTTPMCPServerConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".codeworld", "config.toml"), strings.Join([]string{
		"mcp_oauth_callback_port = 5555",
		"mcp_oauth_callback_url = \"http://localhost:5555/callback\"",
		"[[mcp_servers]]",
		"name = \"docs\"",
		"url = \"https://mcp.example.test/mcp\"",
		"bearer_token_env_var = \"DOCS_TOKEN\"",
		"http_headers = [\"X-Test: yes\"]",
		"enabled_tools = [\"search\"]",
		"disabled_tools = [\"write\"]",
	}, "\n"))

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.MCPOAuthCallbackPort != 5555 || cfg.MCPOAuthCallbackURL != "http://localhost:5555/callback" {
		t.Fatalf("oauth callback = %d/%q", cfg.MCPOAuthCallbackPort, cfg.MCPOAuthCallbackURL)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("MCPServers = %#v, want one server", cfg.MCPServers)
	}
	server := cfg.MCPServers[0]
	if server.URL != "https://mcp.example.test/mcp" || server.BearerTokenEnvVar != "DOCS_TOKEN" {
		t.Fatalf("server auth config = %#v", server)
	}
	if len(server.HTTPHeaders) != 1 || server.HTTPHeaders[0] != "X-Test: yes" {
		t.Fatalf("HTTPHeaders = %#v", server.HTTPHeaders)
	}
	if len(server.EnabledTools) != 1 || server.EnabledTools[0] != "search" || len(server.DisabledTools) != 1 || server.DisabledTools[0] != "write" {
		t.Fatalf("tool filters = enabled %#v disabled %#v", server.EnabledTools, server.DisabledTools)
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

func TestLoadRejectsUnsafeOrInvalidLimits(t *testing.T) {
	for _, content := range []string{
		"approval_mode = \"surprise\"\n",
		"approval_mode = \"full-access\"\n",
		"max_steps = 0\n",
		"summary_max_tokens = -1\n",
	} {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".codeworld", "config.toml"), content)
		if _, err := Load(dir); err == nil {
			t.Fatalf("Load accepted invalid config %q", content)
		}
	}
}

func TestLoadRejectsRemoteLocalProviderURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".codeworld", "config.toml"), "provider = \"local\"\nlocal_base_url = \"https://example.com/v1\"\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("Load accepted remote URL for local provider")
	}
}

func TestLoadWithOptionsLayersUserProjectAndProfile(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "config.toml"), strings.Join([]string{
		`provider = "openai"`,
		`model = "user-model"`,
		`max_steps = 30`,
		`[[mcp_servers]]`,
		`name = "docs"`,
		`command = "user-docs"`,
	}, "\n"))
	writeFile(t, filepath.Join(root, ".codeworld", "config.toml"), strings.Join([]string{
		`model = "project-model"`,
		`max_steps = 40`,
		`[[mcp_servers]]`,
		`name = "docs"`,
		`command = "project-docs"`,
	}, "\n"))
	writeFile(t, filepath.Join(home, "profiles", "fast.toml"), strings.Join([]string{
		`model = "profile-model"`,
		`approval_mode = "full-access"`,
		`sandbox_mode = "danger-full-access"`,
		`sandbox_network = true`,
	}, "\n"))

	cfg, err := LoadWithOptions(root, LoadOptions{Home: home, Profile: "fast"})
	if err != nil {
		t.Fatalf("LoadWithOptions: %v", err)
	}
	if cfg.Provider != "openai" || cfg.Model != "profile-model" || cfg.MaxSteps != 40 || cfg.Profile != "fast" {
		t.Fatalf("layered config = %#v", cfg)
	}
	if cfg.ApprovalMode != "full-access" {
		t.Fatalf("approval mode = %q", cfg.ApprovalMode)
	}
	if cfg.SandboxMode != "danger-full-access" || !cfg.SandboxNetwork {
		t.Fatalf("sandbox config = %q network=%v", cfg.SandboxMode, cfg.SandboxNetwork)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Command != "project-docs" {
		t.Fatalf("MCP servers = %#v", cfg.MCPServers)
	}
}

func TestLoadWithOptionsRejectsMissingOrUnsafeProfile(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if _, err := LoadWithOptions(root, LoadOptions{Home: home, Profile: "missing"}); err == nil {
		t.Fatal("missing profile accepted")
	}
	if _, err := LoadWithOptions(root, LoadOptions{Home: home, Profile: "../escape"}); err == nil {
		t.Fatal("unsafe profile name accepted")
	}
}

func TestLoadWithOptionsProjectCannotEnableFullAccess(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "config.toml"), `approval_mode = "full-access"`)
	writeFile(t, filepath.Join(root, ".codeworld", "config.toml"), `approval_mode = "full-access"`)
	if _, err := LoadWithOptions(root, LoadOptions{Home: home}); err == nil || !strings.Contains(err.Error(), "project approval_mode") {
		t.Fatalf("err = %v, want project full-access rejection", err)
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
