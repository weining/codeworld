package config

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Provider             string
	Model                string
	MaxSteps             int
	Workspace            string
	APIKey               string
	OpenAIAPIKey         string
	AnthropicAPIKey      string
	LocalBaseURL         string
	PluginsEnabled       bool
	SummaryMaxMessages   int
	SummaryMaxTokens     int
	IndexMaxFileBytes    int
	MCPOAuthCallbackPort int
	MCPOAuthCallbackURL  string
	ApprovalMode         string
	ModelCallLogging     bool
	MCPServers           []MCPServer
}

type MCPServer struct {
	Name              string
	Command           string
	Args              []string
	URL               string
	BearerTokenEnvVar string
	HTTPHeaders       []string
	EnabledTools      []string
	DisabledTools     []string
}

// Default 提供对外可复用的能力，并隐藏内部实现细节。
func Default() Config {
	return Config{
		Provider:           "deepseek",
		Model:              "deepseek-v4-pro",
		MaxSteps:           20,
		Workspace:          ".",
		SummaryMaxMessages: 40,
		SummaryMaxTokens:   64 * 1024,
		IndexMaxFileBytes:  256 * 1024,
	}
}

// Load 加载外部或项目内配置，并把原始数据转换为内部结构。
func Load(root string) (Config, error) {
	cfg := Default()
	path := filepath.Join(root, ".codeworld", "config.toml")

	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Config{}, err
		}
		loadEnv(&cfg)
		if err := validate(cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentMCP *MCPServer
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[[mcp_servers]]" {
			cfg.MCPServers = append(cfg.MCPServers, MCPServer{})
			currentMCP = &cfg.MCPServers[len(cfg.MCPServers)-1]
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("invalid config line %q", line)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"")

		if currentMCP != nil {
			if err := setMCPServerValue(currentMCP, key, value); err != nil {
				return Config{}, err
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
				return Config{}, fmt.Errorf("invalid max_steps %q: %w", value, err)
			}
			cfg.MaxSteps = n
		case "workspace":
			cfg.Workspace = value
		case "local_base_url":
			cfg.LocalBaseURL = value
		case "plugins_enabled":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid plugins_enabled %q: %w", value, err)
			}
			cfg.PluginsEnabled = enabled
		case "summary_max_messages":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid summary_max_messages %q: %w", value, err)
			}
			cfg.SummaryMaxMessages = n
		case "summary_max_tokens":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid summary_max_tokens %q: %w", value, err)
			}
			cfg.SummaryMaxTokens = n
		case "index_max_file_bytes":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid index_max_file_bytes %q: %w", value, err)
			}
			cfg.IndexMaxFileBytes = n
		case "mcp_oauth_callback_port":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid mcp_oauth_callback_port %q: %w", value, err)
			}
			cfg.MCPOAuthCallbackPort = n
		case "mcp_oauth_callback_url":
			cfg.MCPOAuthCallbackURL = value
		case "approval_mode":
			cfg.ApprovalMode = value
		case "model_call_logging":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid model_call_logging %q: %w", value, err)
			}
			cfg.ModelCallLogging = enabled
		default:
			return Config{}, fmt.Errorf("unknown config key %q", key)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}

	loadEnv(&cfg)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
	case "", "auto", "read-only":
	default:
		return fmt.Errorf("invalid or unsafe project approval_mode %q", cfg.ApprovalMode)
	}
	return nil
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
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("expected string array")
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if body == "" {
		return nil, nil
	}
	parts := strings.Split(body, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "\"") || !strings.HasSuffix(part, "\"") {
			return nil, fmt.Errorf("expected quoted string")
		}
		out = append(out, strings.Trim(part, "\""))
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
}
