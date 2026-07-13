package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type OAuthToken struct {
	ServerName    string    `json:"server_name"`
	AccessToken   string    `json:"access_token,omitempty"`
	RefreshToken  string    `json:"refresh_token,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
	TokenEndpoint string    `json:"token_endpoint,omitempty"`
	ClientID      string    `json:"client_id,omitempty"`
	Scope         string    `json:"scope,omitempty"`
}

type OAuthLoginOptions struct {
	ServerName   string
	ServerURL    string
	Scopes       []string
	CallbackURL  string
	CallbackPort int
	HTTPClient   *http.Client
	OpenURL      func(string) error
	NotifyURL    func(string)
}

type protectedResourceMetadata struct {
	AuthorizationServers []string `json:"authorization_servers"`
}

type authorizationServerMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

// OAuthTokenPath 返回旧 workspace 布局中 server 对应的 token 文件路径。
func OAuthTokenPath(root, serverName string) (string, error) {
	return oauthTokenPath(filepath.Join(root, ".codeworld", "mcp-oauth"), serverName)
}

// UserOAuthTokenPath 返回 CODEWORLD_HOME 中 server 对应的 token 文件路径。
func UserOAuthTokenPath(home, serverName string) (string, error) {
	return oauthTokenPath(filepath.Join(home, "mcp-oauth"), serverName)
}

func oauthTokenPath(dir, serverName string) (string, error) {
	if serverName == "" || serverName == "." || serverName == ".." || filepath.Base(serverName) != serverName || strings.Contains(serverName, "\\") {
		return "", fmt.Errorf("invalid mcp server name %q", serverName)
	}
	return filepath.Join(dir, serverName+".json"), nil
}

// SaveOAuthToken 保存 OAuth token，后续完整 login flow 会复用该持久化格式。
func SaveOAuthToken(root string, token OAuthToken) error {
	path, err := OAuthTokenPath(root, token.ServerName)
	return saveOAuthToken(path, token, err)
}

// SaveUserOAuthToken 保存用户级 MCP OAuth token。
func SaveUserOAuthToken(home string, token OAuthToken) error {
	path, err := UserOAuthTokenPath(home, token.ServerName)
	return saveOAuthToken(path, token, err)
}

func saveOAuthToken(path string, token OAuthToken, pathErr error) error {
	if pathErr != nil {
		return pathErr
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
	return loadOAuthToken(path, err)
}

// LoadUserOAuthToken 读取用户级 MCP OAuth token。
func LoadUserOAuthToken(home, serverName string) (OAuthToken, error) {
	path, err := UserOAuthTokenPath(home, serverName)
	return loadOAuthToken(path, err)
}

func loadOAuthToken(path string, pathErr error) (OAuthToken, error) {
	if pathErr != nil {
		return OAuthToken{}, pathErr
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

// DeleteOAuthToken 删除 server 对应的 OAuth 凭据；文件不存在时仍视为成功。
func DeleteOAuthToken(root, serverName string) error {
	path, err := OAuthTokenPath(root, serverName)
	return deleteOAuthToken(path, err)
}

// DeleteUserOAuthToken 删除用户级 MCP OAuth token。
func DeleteUserOAuthToken(home, serverName string) error {
	path, err := UserOAuthTokenPath(home, serverName)
	return deleteOAuthToken(path, err)
}

func deleteOAuthToken(path string, pathErr error) error {
	if pathErr != nil {
		return pathErr
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// RefreshOAuthToken 在 access token 即将过期时使用 refresh token 换取新凭据。
func RefreshOAuthToken(ctx context.Context, root, serverName string, client *http.Client) (OAuthToken, error) {
	token, err := LoadOAuthToken(root, serverName)
	if err != nil {
		return OAuthToken{}, err
	}
	return refreshOAuthToken(ctx, serverName, token, client, func(updated OAuthToken) error { return SaveOAuthToken(root, updated) })
}

// RefreshOAuthTokenWithFallback refreshes a user token first, then falls back to the legacy workspace store.
func RefreshOAuthTokenWithFallback(ctx context.Context, home, root, serverName string, client *http.Client) (OAuthToken, error) {
	token, err := LoadUserOAuthToken(home, serverName)
	if err == nil {
		return refreshOAuthToken(ctx, serverName, token, client, func(updated OAuthToken) error { return SaveUserOAuthToken(home, updated) })
	}
	if !os.IsNotExist(err) {
		return OAuthToken{}, err
	}
	token, err = LoadOAuthToken(root, serverName)
	if err != nil {
		return OAuthToken{}, err
	}
	return refreshOAuthToken(ctx, serverName, token, client, func(updated OAuthToken) error { return SaveOAuthToken(root, updated) })
}

func refreshOAuthToken(ctx context.Context, serverName string, token OAuthToken, client *http.Client, save func(OAuthToken) error) (OAuthToken, error) {
	if token.ExpiresAt.IsZero() || time.Until(token.ExpiresAt) > time.Minute {
		return token, nil
	}
	if token.RefreshToken == "" || token.TokenEndpoint == "" || token.ClientID == "" {
		return OAuthToken{}, fmt.Errorf("OAuth token for MCP server %q expired; run mcp login again", serverName)
	}
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {token.RefreshToken},
		"client_id":     {token.ClientID},
	}
	if token.Scope != "" {
		values.Set("scope", token.Scope)
	}
	updated, err := exchangeOAuthToken(ctx, client, token.TokenEndpoint, values)
	if err != nil {
		return OAuthToken{}, err
	}
	updated.ServerName, updated.TokenEndpoint, updated.ClientID = serverName, token.TokenEndpoint, token.ClientID
	if updated.RefreshToken == "" {
		updated.RefreshToken = token.RefreshToken
	}
	if updated.Scope == "" {
		updated.Scope = token.Scope
	}
	if err := save(updated); err != nil {
		return OAuthToken{}, err
	}
	return updated, nil
}

// LoginOAuth 完成 HTTP MCP server 的 OAuth 授权码 + PKCE 登录。
func LoginOAuth(ctx context.Context, opts OAuthLoginOptions) (OAuthToken, error) {
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	metadata, err := discoverAuthorizationServer(ctx, client, opts.ServerURL)
	if err != nil {
		return OAuthToken{}, err
	}
	callbackURL, listener, err := oauthCallback(opts.CallbackURL, opts.CallbackPort)
	if err != nil {
		return OAuthToken{}, err
	}
	defer listener.Close()
	clientID, err := registerOAuthClient(ctx, client, metadata.RegistrationEndpoint, callbackURL)
	if err != nil {
		return OAuthToken{}, err
	}
	verifier, err := randomOAuthValue(48)
	if err != nil {
		return OAuthToken{}, err
	}
	state, err := randomOAuthValue(24)
	if err != nil {
		return OAuthToken{}, err
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	authURL, err := url.Parse(metadata.AuthorizationEndpoint)
	if err != nil {
		return OAuthToken{}, err
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", callbackURL)
	query.Set("state", state)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challengeBytes[:]))
	query.Set("code_challenge_method", "S256")
	if len(opts.Scopes) > 0 {
		query.Set("scope", strings.Join(opts.Scopes, " "))
	}
	authURL.RawQuery = query.Encode()
	if opts.NotifyURL != nil {
		opts.NotifyURL(authURL.String())
	}
	if opts.OpenURL != nil {
		if err := opts.OpenURL(authURL.String()); err != nil {
			return OAuthToken{}, fmt.Errorf("open OAuth authorization URL: %w", err)
		}
	} else {
		// headless 环境可能没有浏览器启动器；URL 已输出，继续等待用户手动打开。
		_ = openBrowser(authURL.String())
	}
	code, err := waitForOAuthCallback(ctx, listener, callbackURL, state)
	if err != nil {
		return OAuthToken{}, err
	}
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {callbackURL},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	token, err := exchangeOAuthToken(ctx, client, metadata.TokenEndpoint, values)
	if err != nil {
		return OAuthToken{}, err
	}
	token.ServerName, token.TokenEndpoint, token.ClientID = opts.ServerName, metadata.TokenEndpoint, clientID
	if token.Scope == "" {
		token.Scope = strings.Join(opts.Scopes, " ")
	}
	return token, nil
}

func discoverAuthorizationServer(ctx context.Context, client *http.Client, serverURL string) (authorizationServerMetadata, error) {
	resourceURL, err := url.Parse(serverURL)
	if err != nil || resourceURL.Scheme == "" || resourceURL.Host == "" {
		return authorizationServerMetadata{}, fmt.Errorf("invalid MCP server URL %q", serverURL)
	}
	resourceMetadataURL := resourceURL.Scheme + "://" + resourceURL.Host + "/.well-known/oauth-protected-resource" + strings.TrimSuffix(resourceURL.EscapedPath(), "/")
	var resource protectedResourceMetadata
	if err := getOAuthJSON(ctx, client, resourceMetadataURL, &resource); err != nil {
		return authorizationServerMetadata{}, fmt.Errorf("discover OAuth protected resource metadata: %w", err)
	}
	if len(resource.AuthorizationServers) == 0 {
		return authorizationServerMetadata{}, fmt.Errorf("OAuth protected resource metadata has no authorization_servers")
	}
	issuer, err := url.Parse(resource.AuthorizationServers[0])
	if err != nil || issuer.Scheme == "" || issuer.Host == "" {
		return authorizationServerMetadata{}, fmt.Errorf("invalid OAuth authorization server %q", resource.AuthorizationServers[0])
	}
	metadataURL := issuer.Scheme + "://" + issuer.Host + "/.well-known/oauth-authorization-server" + strings.TrimSuffix(issuer.EscapedPath(), "/")
	var metadata authorizationServerMetadata
	if err := getOAuthJSON(ctx, client, metadataURL, &metadata); err != nil {
		return authorizationServerMetadata{}, fmt.Errorf("discover OAuth authorization server metadata: %w", err)
	}
	if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" || metadata.RegistrationEndpoint == "" {
		return authorizationServerMetadata{}, fmt.Errorf("OAuth server does not expose authorization, token, and registration endpoints")
	}
	return metadata, nil
}

func registerOAuthClient(ctx context.Context, client *http.Client, endpoint, callbackURL string) (string, error) {
	payload := map[string]any{
		"client_name": "Codeworld CLI", "redirect_uris": []string{callbackURL},
		"grant_types":    []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "token_endpoint_auth_method": "none",
	}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", oauthHTTPError(resp)
	}
	var result struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.ClientID == "" {
		return "", fmt.Errorf("OAuth client registration returned no client_id")
	}
	return result.ClientID, nil
}

func oauthCallback(explicitURL string, port int) (string, net.Listener, error) {
	host := "127.0.0.1"
	path := "/callback"
	if explicitURL != "" {
		parsed, err := url.Parse(explicitURL)
		if err != nil || parsed.Scheme != "http" || parsed.Hostname() == "" {
			return "", nil, fmt.Errorf("invalid MCP OAuth callback URL %q", explicitURL)
		}
		host, path = parsed.Hostname(), parsed.Path
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", nil, fmt.Errorf("MCP OAuth callback URL must use a loopback host")
		}
		if path == "" {
			path = "/"
		}
		if parsed.Port() != "" {
			fmt.Sscanf(parsed.Port(), "%d", &port)
		}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return "", nil, err
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, fmt.Sprint(actualPort)), Path: path}).String(), listener, nil
}

func waitForOAuthCallback(ctx context.Context, listener net.Listener, callbackURL, state string) (string, error) {
	path, _ := url.Parse(callbackURL)
	result := make(chan struct {
		code string
		err  error
	}, 1)
	var finish sync.Once
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path.Path {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("state") != state {
			finish.Do(func() {
				result <- struct {
					code string
					err  error
				}{err: fmt.Errorf("OAuth state mismatch")}
			})
			http.Error(w, "OAuth state mismatch", http.StatusBadRequest)
			return
		}
		if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
			finish.Do(func() {
				result <- struct {
					code string
					err  error
				}{err: fmt.Errorf("OAuth authorization failed: %s", oauthErr)}
			})
			http.Error(w, "OAuth authorization failed", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			finish.Do(func() {
				result <- struct {
					code string
					err  error
				}{err: fmt.Errorf("OAuth callback has no code")}
			})
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}
		finish.Do(func() {
			result <- struct {
				code string
				err  error
			}{code: code}
		})
		_, _ = io.WriteString(w, "Codeworld MCP login complete. You can close this window.\n")
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case value := <-result:
		return value.code, value.err
	}
}

func exchangeOAuthToken(ctx context.Context, client *http.Client, endpoint string, values url.Values) (OAuthToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return OAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return OAuthToken{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuthToken{}, oauthHTTPError(resp)
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return OAuthToken{}, err
	}
	if body.AccessToken == "" {
		return OAuthToken{}, fmt.Errorf("OAuth token response has no access_token")
	}
	token := OAuthToken{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken, Scope: body.Scope}
	if body.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	return token, nil
}

func getOAuthJSON(ctx context.Context, client *http.Client, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oauthHTTPError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func oauthHTTPError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("OAuth endpoint %s returned status %d: %s", resp.Request.URL, resp.StatusCode, strings.TrimSpace(string(data)))
}

func randomOAuthValue(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func openBrowser(target string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{target}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		name, args = "xdg-open", []string{target}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
