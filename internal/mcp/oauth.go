package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type OAuthToken struct {
	ServerName   string    `json:"server_name"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// OAuthTokenPath 返回 server 对应的本地 token 文件路径。
func OAuthTokenPath(root, serverName string) (string, error) {
	if serverName == "" || serverName == "." || serverName == ".." || filepath.Base(serverName) != serverName || strings.Contains(serverName, "\\") {
		return "", fmt.Errorf("invalid mcp server name %q", serverName)
	}
	return filepath.Join(root, ".codeworld", "mcp-oauth", serverName+".json"), nil
}

// SaveOAuthToken 保存 OAuth token，后续完整 login flow 会复用该持久化格式。
func SaveOAuthToken(root string, token OAuthToken) error {
	path, err := OAuthTokenPath(root, token.ServerName)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadOAuthToken 读取已保存的 OAuth token。
func LoadOAuthToken(root, serverName string) (OAuthToken, error) {
	path, err := OAuthTokenPath(root, serverName)
	if err != nil {
		return OAuthToken{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return OAuthToken{}, err
	}
	var token OAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return OAuthToken{}, err
	}
	return token, nil
}
