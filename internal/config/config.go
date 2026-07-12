package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Profile               string
	Provider              string
	Model                 string
	MaxSteps              int
	Workspace             string
	APIKey                string
	OpenAIAPIKey          string
	AnthropicAPIKey       string
	LocalBaseURL          string
	PluginsEnabled        bool
	SummaryMaxMessages    int
	SummaryMaxTokens      int
	IndexMaxFileBytes     int
	MCPOAuthCallbackPort  int
	MCPOAuthCallbackURL   string
	ApprovalMode          string
	SandboxMode           string
	SandboxNetwork        bool
	SubagentMaxConcurrent int
	SubagentRoles         []SubagentRole
	ModelCallLogging      bool
	MCPServers            []MCPServer
}

type LoadOptions struct {
	Home      string
	Profile   string
	SkipUser  bool
	Overrides []string
}

type MCPServer struct {
	Name              string   `json:"name"`
	Command           string   `json:"command,omitempty"`
	Args              []string `json:"args,omitempty"`
	URL               string   `json:"url,omitempty"`
	BearerTokenEnvVar string   `json:"bearer_token_env_var,omitempty"`
	HTTPHeaders       []string `json:"http_headers,omitempty"`
	EnabledTools      []string `json:"enabled_tools,omitempty"`
	DisabledTools     []string `json:"disabled_tools,omitempty"`
}

type SubagentRole struct {
	Name         string
	Description  string
	Instructions string
}

// Default 提供对外可复用的能力，并隐藏内部实现细节。
func Default() Config {
	return Config{
		Provider:              "deepseek",
		Model:                 "deepseek-v4-pro",
		MaxSteps:              20,
		Workspace:             ".",
		SummaryMaxMessages:    40,
		SummaryMaxTokens:      64 * 1024,
		IndexMaxFileBytes:     256 * 1024,
		SandboxMode:           "workspace-write",
		SubagentMaxConcurrent: 4,
	}
}

// Load 加载外部或项目内配置，并把原始数据转换为内部结构。
func Load(root string) (Config, error) {
	return LoadWithOptions(root, LoadOptions{SkipUser: true})
}

// LoadWithOptions 按默认值、用户、项目、profile 和环境变量的顺序合并配置。
func LoadWithOptions(root string, opts LoadOptions) (Config, error) {
	cfg := Default()
	profile := strings.TrimSpace(opts.Profile)
	if profile == "" && !opts.SkipUser {
		profile = strings.TrimSpace(os.Getenv("CODEWORLD_PROFILE"))
	}
	home := ""
	if !opts.SkipUser || profile != "" {
		var err error
		home, err = configHome(opts.Home)
		if err != nil {
			return Config{}, err
		}
	}
	if !opts.SkipUser {
		if err := loadConfigFile(filepath.Join(home, "config.toml"), &cfg, true, false); err != nil {
			return Config{}, err
		}
		if err := loadConfigFile(filepath.Join(home, "mcp.toml"), &cfg, true, false); err != nil {
			return Config{}, err
		}
	}
	if err := loadConfigFile(filepath.Join(root, ".codeworld", "config.toml"), &cfg, false, false); err != nil {
		return Config{}, err
	}
	if profile != "" {
		if err := validateProfileName(profile); err != nil {
			return Config{}, err
		}
		if err := loadConfigFile(filepath.Join(home, "profiles", profile+".toml"), &cfg, true, true); err != nil {
			return Config{}, err
		}
		cfg.Profile = profile
	}
	cfg.MCPServers = dedupeMCPServers(cfg.MCPServers)
	cfg.SubagentRoles = dedupeSubagentRoles(cfg.SubagentRoles)
	loadEnv(&cfg)
	if err := applyOverrides(&cfg, opts.Overrides); err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyOverrides(cfg *Config, overrides []string) error {
	for _, override := range overrides {
		key, value, ok := strings.Cut(override, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return fmt.Errorf("invalid config override %q; expected key=value", override)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "\"") {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return fmt.Errorf("invalid config override %q: %w", override, err)
			}
			value = unquoted
		}
		switch key {
		case "provider":
			cfg.Provider = value
		case "model":
			cfg.Model = value
		case "workspace":
			cfg.Workspace = value
		case "local_base_url":
			cfg.LocalBaseURL = value
		case "approval_mode":
			cfg.ApprovalMode = value
		case "sandbox_mode":
			cfg.SandboxMode = value
		case "max_steps":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("config override %s must be an integer", key)
			}
			cfg.MaxSteps = n
		case "summary_max_messages":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("config override %s must be an integer", key)
			}
			cfg.SummaryMaxMessages = n
		case "summary_max_tokens":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("config override %s must be an integer", key)
			}
			cfg.SummaryMaxTokens = n
		case "index_max_file_bytes":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("config override %s must be an integer", key)
			}
			cfg.IndexMaxFileBytes = n
		case "subagent_max_concurrent":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("config override %s must be an integer", key)
			}
			cfg.SubagentMaxConcurrent = n
		case "plugins_enabled", "features.plugins":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("config override %s must be true or false", key)
			}
			cfg.PluginsEnabled = enabled
		case "model_call_logging", "features.model_call_logging":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("config override %s must be true or false", key)
			}
			cfg.ModelCallLogging = enabled
		case "sandbox_network":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("config override %s must be true or false", key)
			}
			cfg.SandboxNetwork = enabled
		default:
			return fmt.Errorf("unsupported config override key %q", key)
		}
	}
	return nil
}

func loadConfigFile(path string, cfg *Config, allowFullAccess, required bool) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentMCP *MCPServer
	var currentRole *SubagentRole
	approvalModeSet := false
	sandboxModeSet := false
	sandboxNetworkSet := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[[mcp_servers]]" {
			cfg.MCPServers = append(cfg.MCPServers, MCPServer{})
			currentMCP = &cfg.MCPServers[len(cfg.MCPServers)-1]
			currentRole = nil
			continue
		}
		if line == "[[subagent_roles]]" {
			cfg.SubagentRoles = append(cfg.SubagentRoles, SubagentRole{})
			currentRole = &cfg.SubagentRoles[len(cfg.SubagentRoles)-1]
			currentMCP = nil
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s: invalid config line %q", path, line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "\"") {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return fmt.Errorf("%s: invalid string value %q: %w", path, value, err)
			}
			value = unquoted
		}

		if currentMCP != nil {
			if err := setMCPServerValue(currentMCP, key, value); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			continue
		}
		if currentRole != nil {
			if err := setSubagentRoleValue(currentRole, key, value); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			continue
		}

		switch key {
		case "provider":
			cfg.Provider = value
		case "model":
			cfg.Model = value
		case "max_steps":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: invalid max_steps %q: %w", path, value, err)
			}
			cfg.MaxSteps = n
		case "workspace":
			cfg.Workspace = value
		case "local_base_url":
			cfg.LocalBaseURL = value
		case "plugins_enabled":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s: invalid plugins_enabled %q: %w", path, value, err)
			}
			cfg.PluginsEnabled = enabled
		case "summary_max_messages":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: invalid summary_max_messages %q: %w", path, value, err)
			}
			cfg.SummaryMaxMessages = n
		case "summary_max_tokens":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: invalid summary_max_tokens %q: %w", path, value, err)
			}
			cfg.SummaryMaxTokens = n
		case "index_max_file_bytes":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: invalid index_max_file_bytes %q: %w", path, value, err)
			}
			cfg.IndexMaxFileBytes = n
		case "mcp_oauth_callback_port":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: invalid mcp_oauth_callback_port %q: %w", path, value, err)
			}
			cfg.MCPOAuthCallbackPort = n
		case "mcp_oauth_callback_url":
			cfg.MCPOAuthCallbackURL = value
		case "approval_mode":
			cfg.ApprovalMode = value
			approvalModeSet = true
		case "sandbox_mode":
			cfg.SandboxMode = value
			sandboxModeSet = true
		case "sandbox_network":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s: invalid sandbox_network %q: %w", path, value, err)
			}
			cfg.SandboxNetwork = enabled
			sandboxNetworkSet = true
		case "subagent_max_concurrent":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: invalid subagent_max_concurrent %q: %w", path, value, err)
			}
			cfg.SubagentMaxConcurrent = n
		case "model_call_logging":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s: invalid model_call_logging %q: %w", path, value, err)
			}
			cfg.ModelCallLogging = enabled
		default:
			return fmt.Errorf("%s: unknown config key %q", path, key)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if approvalModeSet && cfg.ApprovalMode == "full-access" && !allowFullAccess {
		return fmt.Errorf("%s: project approval_mode cannot be full-access", path)
	}
	if sandboxModeSet && cfg.SandboxMode == "danger-full-access" && !allowFullAccess {
		return fmt.Errorf("%s: project sandbox_mode cannot be danger-full-access", path)
	}
	if sandboxNetworkSet && cfg.SandboxNetwork && !allowFullAccess {
		return fmt.Errorf("%s: project sandbox_network cannot enable network access", path)
	}
	return nil
}

func validate(cfg Config) error {
	if cfg.MaxSteps <= 0 {
		return fmt.Errorf("max_steps must be greater than zero")
	}
	if cfg.SummaryMaxMessages <= 0 {
		return fmt.Errorf("summary_max_messages must be greater than zero")
	}
	if cfg.SummaryMaxTokens <= 0 {
		return fmt.Errorf("summary_max_tokens must be greater than zero")
	}
	if cfg.IndexMaxFileBytes <= 0 {
		return fmt.Errorf("index_max_file_bytes must be greater than zero")
	}
	if cfg.SubagentMaxConcurrent <= 0 {
		return fmt.Errorf("subagent_max_concurrent must be greater than zero")
	}
	if cfg.Provider == "local" {
		u, err := url.Parse(cfg.LocalBaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return fmt.Errorf("invalid local_base_url %q", cfg.LocalBaseURL)
		}
		host := u.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("local_base_url must use a loopback host")
		}
	}
	switch cfg.ApprovalMode {
	case "", "auto", "read-only", "full-access":
	default:
		return fmt.Errorf("invalid approval_mode %q", cfg.ApprovalMode)
	}
	switch cfg.SandboxMode {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return fmt.Errorf("invalid sandbox_mode %q", cfg.SandboxMode)
	}
	for _, role := range cfg.SubagentRoles {
		if strings.TrimSpace(role.Name) == "" {
			return fmt.Errorf("subagent role name is required")
		}
	}
	return nil
}

func setSubagentRoleValue(role *SubagentRole, key, value string) error {
	switch key {
	case "name":
		role.Name = value
	case "description":
		role.Description = value
	case "instructions":
		role.Instructions = value
	default:
		return fmt.Errorf("unknown subagent_roles key %q", key)
	}
	return nil
}

func configHome(explicit string) (string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return filepath.Abs(explicit)
	}
	if fromEnv := strings.TrimSpace(os.Getenv("CODEWORLD_HOME")); fromEnv != "" {
		return filepath.Abs(fromEnv)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codeworld"), nil
}

// Home 返回 Codeworld 用户配置目录。
func Home(explicit string) (string, error) {
	return configHome(explicit)
}

func validateProfileName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	return nil
}

func dedupeMCPServers(servers []MCPServer) []MCPServer {
	indexes := map[string]int{}
	result := make([]MCPServer, 0, len(servers))
	for _, server := range servers {
		if index, ok := indexes[server.Name]; ok && server.Name != "" {
			result[index] = server
			continue
		}
		if server.Name != "" {
			indexes[server.Name] = len(result)
		}
		result = append(result, server)
	}
	return result
}

func dedupeSubagentRoles(roles []SubagentRole) []SubagentRole {
	indexes := map[string]int{}
	result := make([]SubagentRole, 0, len(roles))
	for _, role := range roles {
		if index, ok := indexes[role.Name]; ok && role.Name != "" {
			result[index] = role
			continue
		}
		if role.Name != "" {
			indexes[role.Name] = len(result)
		}
		result = append(result, role)
	}
	return result
}

// setMCPServerValue 封装局部逻辑，保持调用方流程清晰。
func setMCPServerValue(server *MCPServer, key, value string) error {
	switch key {
	case "name":
		server.Name = value
	case "command":
		server.Command = value
	case "args":
		args, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("invalid mcp args %q: %w", value, err)
		}
		server.Args = args
	case "url":
		server.URL = value
	case "bearer_token_env_var":
		server.BearerTokenEnvVar = value
	case "http_headers":
		headers, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("invalid mcp http_headers %q: %w", value, err)
		}
		server.HTTPHeaders = headers
	case "enabled_tools":
		tools, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("invalid mcp enabled_tools %q: %w", value, err)
		}
		server.EnabledTools = tools
	case "disabled_tools":
		tools, err := parseStringArray(value)
		if err != nil {
			return fmt.Errorf("invalid mcp disabled_tools %q: %w", value, err)
		}
		server.DisabledTools = tools
	default:
		return fmt.Errorf("unknown mcp_servers key %q", key)
	}
	return nil
}

// parseStringArray 解析输入数据，并执行必要的格式校验。
func parseStringArray(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, fmt.Errorf("expected string array: %w", err)
	}
	return out, nil
}

// loadEnv 加载外部或项目内配置，并把原始数据转换为内部结构。
func loadEnv(cfg *Config) {
	cfg.APIKey = os.Getenv("DEEPSEEK_API_KEY")
	cfg.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	cfg.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	if cfg.LocalBaseURL == "" {
		cfg.LocalBaseURL = os.Getenv("CODEWORLD_LOCAL_BASE_URL")
	}
	if value := strings.TrimSpace(os.Getenv("CODEWORLD_SANDBOX")); value != "" {
		cfg.SandboxMode = value
	}
}
