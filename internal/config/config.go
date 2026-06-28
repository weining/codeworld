package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Provider           string
	Model              string
	MaxSteps           int
	Workspace          string
	APIKey             string
	OpenAIAPIKey       string
	AnthropicAPIKey    string
	LocalBaseURL       string
	PluginsEnabled     bool
	SummaryMaxMessages int
	IndexMaxFileBytes  int
}

func Default() Config {
	return Config{
		Provider:           "deepseek",
		Model:              "deepseek-v4-pro",
		MaxSteps:           20,
		Workspace:          ".",
		SummaryMaxMessages: 40,
		IndexMaxFileBytes:  256 * 1024,
	}
}

func Load(root string) (Config, error) {
	cfg := Default()
	path := filepath.Join(root, ".codeworld", "config.toml")

	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Config{}, err
		}
		loadEnv(&cfg)
		return cfg, nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("invalid config line %q", line)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"")

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
		case "index_max_file_bytes":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid index_max_file_bytes %q: %w", value, err)
			}
			cfg.IndexMaxFileBytes = n
		default:
			return Config{}, fmt.Errorf("unknown config key %q", key)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}

	loadEnv(&cfg)
	return cfg, nil
}

func loadEnv(cfg *Config) {
	cfg.APIKey = os.Getenv("DEEPSEEK_API_KEY")
	cfg.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	cfg.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	if cfg.LocalBaseURL == "" {
		cfg.LocalBaseURL = os.Getenv("CODEWORLD_LOCAL_BASE_URL")
	}
}
