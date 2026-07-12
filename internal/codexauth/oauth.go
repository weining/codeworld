package codexauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	AuthorizeURL = "https://auth.openai.com/oauth/authorize"
	TokenURL     = "https://auth.openai.com/oauth/token"
	RedirectURI  = "http://localhost:1455/auth/callback"
	Scope        = "openid profile email offline_access"
)

type Credentials struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	Expires   int64  `json:"expires"`
	AccountID string `json:"account_id"`
}

type Flow struct {
	Verifier  string
	Challenge string
	State     string
}

type AuthorizationInput struct {
	Code  string
	State string
}

type Options struct {
	TokenURL   string
	HTTPClient *http.Client
	Now        func() time.Time
}

type OAuthClient struct {
	tokenURL   string
	httpClient *http.Client
	now        func() time.Time
}

type Store struct {
	Root string
}

// NewFlow 构造 OpenClaw/Codex OAuth 使用的 PKCE verifier、challenge 和 state。
func NewFlow(random io.Reader) (Flow, error) {
	if random == nil {
		random = rand.Reader
	}
	verifierBytes := make([]byte, 32)
	if _, err := io.ReadFull(random, verifierBytes); err != nil {
		return Flow{}, err
	}
	stateBytes := make([]byte, 16)
	if _, err := io.ReadFull(random, stateBytes); err != nil {
		return Flow{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	sum := sha256.Sum256([]byte(verifier))
	return Flow{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
		State:     hex.EncodeToString(stateBytes),
	}, nil
}

// AuthorizeURL 生成与 OpenClaw Codex OAuth 相同参数的授权地址。
func (f Flow) AuthorizeURL(originator string) (string, error) {
	if originator == "" {
		originator = "codeworld"
	}
	u, err := url.Parse(AuthorizeURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", ClientID)
	q.Set("redirect_uri", RedirectURI)
	q.Set("scope", Scope)
	q.Set("code_challenge", f.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", f.State)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", originator)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ParseAuthorizationInput 接受 redirect URL、query string、code#state 或裸 code。
func ParseAuthorizationInput(input string) AuthorizationInput {
	value := strings.TrimSpace(input)
	if value == "" {
		return AuthorizationInput{}
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		return AuthorizationInput{Code: parsed.Query().Get("code"), State: parsed.Query().Get("state")}
	}
	if strings.Contains(value, "#") {
		code, state, _ := strings.Cut(value, "#")
		return AuthorizationInput{Code: code, State: state}
	}
	if strings.Contains(value, "code=") {
		params, _ := url.ParseQuery(value)
		return AuthorizationInput{Code: params.Get("code"), State: params.Get("state")}
	}
	return AuthorizationInput{Code: value}
}

// NewOAuthClient 创建 token exchange/refresh 客户端，测试可注入 endpoint 和时间。
func NewOAuthClient(options Options) OAuthClient {
	tokenURL := options.TokenURL
	if tokenURL == "" {
		tokenURL = TokenURL
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return OAuthClient{tokenURL: tokenURL, httpClient: httpClient, now: now}
}

// Exchange 使用 authorization code 换取 Codex OAuth 凭据。
func (c OAuthClient) Exchange(ctx context.Context, code, verifier, redirectURI string) (Credentials, error) {
	return c.postTokenForm(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {ClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	})
}

// Refresh 使用 refresh token 换取新的 Codex OAuth 凭据。
func (c OAuthClient) Refresh(ctx context.Context, refreshToken string) (Credentials, error) {
	return c.postTokenForm(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {ClientID},
	})
}

// postTokenForm 执行 token endpoint 请求，并做字段完整性校验。
func (c OAuthClient) postTokenForm(ctx context.Context, form url.Values) (Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Credentials{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Credentials{}, fmt.Errorf("OpenAI Codex token request failed (%d): %s", resp.StatusCode, string(data))
	}
	var body struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return Credentials{}, err
	}
	if body.AccessToken == "" || body.RefreshToken == "" || body.ExpiresIn <= 0 {
		return Credentials{}, fmt.Errorf("OpenAI Codex token response missing access_token, refresh_token, or expires_in")
	}
	accountID, err := ExtractAccountID(body.AccessToken)
	if err != nil {
		return Credentials{}, err
	}
	expires := c.now().Add(time.Duration(body.ExpiresIn * float64(time.Second))).UnixMilli()
	return Credentials{Access: body.AccessToken, Refresh: body.RefreshToken, Expires: expires, AccountID: accountID}, nil
}

// ExtractAccountID 从 ChatGPT OAuth JWT payload 中读取 chatgpt_account_id。
func ExtractAccountID(accessToken string) (string, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("access token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return "", err
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return "", err
	}
	auth, _ := parsed["https://api.openai.com/auth"].(map[string]any)
	accountID, _ := auth["chatgpt_account_id"].(string)
	if accountID == "" {
		return "", fmt.Errorf("Failed to extract accountId from token")
	}
	return accountID, nil
}

// Path 返回 workspace 私有 Codex OAuth 凭据路径。
func (s Store) Path() string {
	return filepath.Join(s.Root, ".codeworld", "auth", "codex.json")
}

// Load 读取已保存的 Codex OAuth 凭据。
func (s Store) Load() (Credentials, error) {
	data, err := os.ReadFile(s.Path())
	if err != nil {
		return Credentials{}, err
	}
	var cred Credentials
	if err := json.Unmarshal(data, &cred); err != nil {
		return Credentials{}, err
	}
	if cred.Access == "" || cred.Refresh == "" || cred.AccountID == "" {
		return Credentials{}, fmt.Errorf("codex OAuth credentials are incomplete")
	}
	return cred, nil
}

// Save 保存 Codex OAuth 凭据，文件权限固定为 0600。
func (s Store) Save(cred Credentials) error {
	path := s.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}
	data = append(bytes.TrimSpace(data), '\n')
	return os.WriteFile(path, data, 0o600)
}

// Delete 删除本 workspace 保存的 Codex OAuth 凭据；凭据不存在时保持幂等。
func (s Store) Delete() (bool, error) {
	err := os.Remove(s.Path())
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
