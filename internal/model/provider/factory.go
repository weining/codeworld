package provider

import (
	"fmt"

	"codeworld/internal/model"
	"codeworld/internal/model/anthropic"
	"codeworld/internal/model/deepseek"
	"codeworld/internal/model/openai"
)

type Config struct {
	Provider        string
	Model           string
	DeepSeekAPIKey  string
	OpenAIAPIKey    string
	AnthropicAPIKey string
	LocalBaseURL    string
	LogPath         string
}

func NewClient(cfg Config) (model.Client, error) {
	switch cfg.Provider {
	case "", "deepseek":
		client := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.Model)
		if cfg.LogPath != "" {
			client.SetLogger(deepseek.NewFileJSONLLogger(cfg.LogPath))
		}
		return client, nil
	case "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is not set")
		}
		client := openai.NewClient(cfg.OpenAIAPIKey, cfg.Model, "")
		if cfg.LogPath != "" {
			client.SetLogger(openai.NewFileJSONLLogger(cfg.LogPath))
		}
		return client, nil
	case "local":
		if cfg.LocalBaseURL == "" {
			return nil, fmt.Errorf("CODEWORLD_LOCAL_BASE_URL is not set")
		}
		client := openai.NewClient("", cfg.Model, cfg.LocalBaseURL)
		if cfg.LogPath != "" {
			client.SetLogger(openai.NewFileJSONLLogger(cfg.LogPath))
		}
		return client, nil
	case "anthropic":
		if cfg.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set")
		}
		client := anthropic.NewClient(cfg.AnthropicAPIKey, cfg.Model)
		if cfg.LogPath != "" {
			client.SetLogger(anthropic.NewFileJSONLLogger(cfg.LogPath))
		}
		return client, nil
	default:
		return nil, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}
