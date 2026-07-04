package provider

import (
	"context"
	"fmt"
	"time"

	"codeworld/internal/codexauth"
	"codeworld/internal/model"
	"codeworld/internal/model/anthropic"
	"codeworld/internal/model/codex"
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
	Root            string
	LogPath         string
}

// NewClient 创建并返回对应组件，集中设置默认依赖和初始状态。
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
	case "codex":
		cred, err := resolveCodexCredentials(cfg.Root)
		if err != nil {
			return nil, err
		}
		client := codex.NewClient(codex.Config{
			AccessToken: cred.Access,
			AccountID:   cred.AccountID,
			Model:       cfg.Model,
		})
		if cfg.LogPath != "" {
			client.SetLogger(codex.NewFileJSONLLogger(cfg.LogPath))
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

// resolveCodexCredentials 读取并按需刷新 OpenClaw 风格 Codex OAuth 凭据。
func resolveCodexCredentials(root string) (codexauth.Credentials, error) {
	store := codexauth.Store{Root: root}
	cred, err := store.Load()
	if err != nil {
		return codexauth.Credentials{}, fmt.Errorf("codex OAuth credentials not found; run codeworld auth codex login: %w", err)
	}
	if time.Until(time.UnixMilli(cred.Expires)) > time.Minute {
		return cred, nil
	}
	refreshed, err := codexauth.NewOAuthClient(codexauth.Options{}).Refresh(context.Background(), cred.Refresh)
	if err != nil {
		return codexauth.Credentials{}, fmt.Errorf("codex OAuth token refresh failed; run codeworld auth codex login: %w", err)
	}
	if err := store.Save(refreshed); err != nil {
		return codexauth.Credentials{}, err
	}
	return refreshed, nil
}
