package codexauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBuildAuthorizeURLMatchesOpenClawCodexOAuth 验证授权 URL 参数与 OpenClaw 的 Codex OAuth 流程保持一致。
func TestBuildAuthorizeURLMatchesOpenClawCodexOAuth(t *testing.T) {
	flow, err := NewFlow(strings.NewReader(strings.Repeat("a", 64)))
	if err != nil {
		t.Fatalf("NewFlow returned error: %v", err)
	}

	authURL, err := flow.AuthorizeURL("codeworld")
	if err != nil {
		t.Fatalf("AuthorizeURL returned error: %v", err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("Parse auth URL: %v", err)
	}
	params := parsed.Query()
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != AuthorizeURL {
		t.Fatalf("authorize base = %s, want %s", parsed.Scheme+"://"+parsed.Host+parsed.Path, AuthorizeURL)
	}
	for key, want := range map[string]string{
		"response_type":              "code",
		"client_id":                  ClientID,
		"redirect_uri":               RedirectURI,
		"scope":                      Scope,
		"code_challenge_method":      "S256",
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"originator":                 "codeworld",
	} {
		if got := params.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if params.Get("code_challenge") == "" || params.Get("state") == "" {
		t.Fatalf("auth URL missing PKCE challenge or state: %s", authURL)
	}
}

// makeJWT 构造只供测试解析 account id 使用的无签名 JWT。
func makeJWT(accountID string) string {
	payload := `{"https://api.openai.com/auth":{"chatgpt_account_id":"` + accountID + `"}}`
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
}

// TestParseAuthorizationInput 支持完整 redirect URL、query string、code#state 和裸 code。
func TestParseAuthorizationInput(t *testing.T) {
	for _, tc := range []struct {
		input string
		code  string
		state string
	}{
		{"http://localhost:1455/auth/callback?code=oauth-code&state=oauth-state", "oauth-code", "oauth-state"},
		{"code=oauth-code&state=oauth-state", "oauth-code", "oauth-state"},
		{"oauth-code#oauth-state", "oauth-code", "oauth-state"},
		{" oauth-code ", "oauth-code", ""},
	} {
		got := ParseAuthorizationInput(tc.input)
		if got.Code != tc.code || got.State != tc.state {
			t.Fatalf("ParseAuthorizationInput(%q) = %#v, want code=%q state=%q", tc.input, got, tc.code, tc.state)
		}
	}
}

// TestExchangeAndRefreshToken 验证 token exchange 和 refresh 使用 OpenClaw 相同的 form 参数。
func TestExchangeAndRefreshToken(t *testing.T) {
	var requests []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		requests = append(requests, r.Form)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  makeJWT("acct-1"),
			"refresh_token": "refresh-next",
			"expires_in":    3600,
		})
	}))
	defer server.Close()
	client := NewOAuthClient(Options{TokenURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time {
		return time.Unix(100, 0)
	}})

	exchanged, err := client.Exchange(context.Background(), "code-1", "verifier-1", RedirectURI)
	if err != nil {
		t.Fatalf("Exchange returned error: %v", err)
	}
	refreshed, err := client.Refresh(context.Background(), "refresh-1")
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if exchanged.AccountID != "acct-1" || refreshed.AccountID != "acct-1" {
		t.Fatalf("account ids = %q/%q, want acct-1", exchanged.AccountID, refreshed.AccountID)
	}
	if requests[0].Get("grant_type") != "authorization_code" || requests[0].Get("client_id") != ClientID || requests[0].Get("code_verifier") != "verifier-1" {
		t.Fatalf("exchange form = %#v", requests[0])
	}
	if requests[1].Get("grant_type") != "refresh_token" || requests[1].Get("refresh_token") != "refresh-1" || requests[1].Get("client_id") != ClientID {
		t.Fatalf("refresh form = %#v", requests[1])
	}
}

// TestStoreLoadSaveCredentials 验证 OAuth 凭据落盘在 workspace 私有 auth 文件中。
func TestStoreLoadSaveCredentials(t *testing.T) {
	root := t.TempDir()
	store := Store{Root: root}
	cred := Credentials{Access: "access", Refresh: "refresh", Expires: 1234, AccountID: "acct-1"}
	if err := store.Save(cred); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded != cred {
		t.Fatalf("loaded = %#v, want %#v", loaded, cred)
	}
	info, err := os.Stat(filepath.Join(root, ".codeworld", "auth", "codex.json"))
	if err != nil {
		t.Fatalf("Stat credentials: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestUserStoreUsesCodeworldHomeLayout(t *testing.T) {
	home := t.TempDir()
	store := UserStore(home)
	if want := filepath.Join(home, "auth", "codex.json"); store.Path() != want {
		t.Fatalf("path = %q, want %q", store.Path(), want)
	}
}

// TestStoreDeleteCredentials 验证删除操作不会因为凭据已不存在而失败。
func TestStoreDeleteCredentials(t *testing.T) {
	store := Store{Root: t.TempDir()}
	if err := store.Save(Credentials{Access: "a", Refresh: "r", AccountID: "id"}); err != nil {
		t.Fatal(err)
	}
	removed, err := store.Delete()
	if err != nil || !removed {
		t.Fatalf("first Delete = %v, %v", removed, err)
	}
	removed, err = store.Delete()
	if err != nil || removed {
		t.Fatalf("second Delete = %v, %v", removed, err)
	}
}

// TestExtractAccountID decodes the ChatGPT account id from a Codex access token.
func TestExtractAccountID(t *testing.T) {
	accountID, err := ExtractAccountID(makeJWT("acct-1"))
	if err != nil {
		t.Fatalf("ExtractAccountID returned error: %v", err)
	}
	if accountID != "acct-1" {
		t.Fatalf("accountID = %q, want acct-1", accountID)
	}
}
