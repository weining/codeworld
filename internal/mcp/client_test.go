package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestStdioClientListsAndCallsTools 验证对应场景的行为，避免后续改动破坏既有约束。
func TestStdioClientListsAndCallsTools(t *testing.T) {
	if os.Getenv("CODEWORLD_MCP_TEST_SERVER") == "1" {
		runFakeServer()
		return
	}
	ctx := context.Background()
	cmd := exec.Command(os.Args[0], "-test.run=TestStdioClientListsAndCallsTools")
	cmd.Env = append(os.Environ(), "CODEWORLD_MCP_TEST_SERVER=1")

	client, err := StartStdio(ctx, ServerConfig{Name: "demo", Command: cmd.Path, Args: cmd.Args[1:], Env: cmd.Env})
	if err != nil {
		t.Fatalf("StartStdio returned error: %v", err)
	}
	defer client.Close()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" || tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tools = %#v, want echo tool", tools)
	}
	result, err := client.CallTool(ctx, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result.Text() != "hello" {
		t.Fatalf("result = %#v, want hello text", result)
	}
}

// TestStdioClientListsResourcesAndPrompts 验证对应场景的行为，避免后续改动破坏既有约束。
func TestStdioClientListsResourcesAndPrompts(t *testing.T) {
	if os.Getenv("CODEWORLD_MCP_TEST_SERVER") == "1" {
		runFakeServer()
		return
	}
	ctx := context.Background()
	cmd := exec.Command(os.Args[0], "-test.run=TestStdioClientListsResourcesAndPrompts")
	cmd.Env = append(os.Environ(), "CODEWORLD_MCP_TEST_SERVER=1")

	client, err := StartStdio(ctx, ServerConfig{Name: "demo", Command: cmd.Path, Args: cmd.Args[1:], Env: cmd.Env})
	if err != nil {
		t.Fatalf("StartStdio returned error: %v", err)
	}
	defer client.Close()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	resources, err := client.ListResources(ctx)
	if err != nil {
		t.Fatalf("ListResources returned error: %v", err)
	}
	if len(resources) != 1 || resources[0].URI != "file:///demo.md" {
		t.Fatalf("resources = %#v, want demo resource", resources)
	}
	contents, err := client.ReadResource(ctx, "file:///demo.md")
	if err != nil {
		t.Fatalf("ReadResource returned error: %v", err)
	}
	if len(contents) != 1 || contents[0].Text != "resource text" {
		t.Fatalf("contents = %#v, want resource text", contents)
	}
	prompts, err := client.ListPrompts(ctx)
	if err != nil {
		t.Fatalf("ListPrompts returned error: %v", err)
	}
	if len(prompts) != 1 || prompts[0].Name != "review" {
		t.Fatalf("prompts = %#v, want review prompt", prompts)
	}
}

// TestHTTPClientListsAndCallsTools 验证 HTTP MCP transport 通过 JSON-RPC 调用工具。
func TestHTTPClientListsAndCallsTools(t *testing.T) {
	var sawAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer token-value" && r.Header.Get("X-Test") == "yes" {
			sawAuth = true
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "instructions": "Use docs carefully."}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "search", "description": "Search", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "result text"}}}
		default:
			result = map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()
	t.Setenv("DOCS_TOKEN", "token-value")

	client := NewHTTPClient(ServerConfig{Name: "docs", URL: server.URL, BearerTokenEnvVar: "DOCS_TOKEN", HTTPHeaders: []string{"X-Test: yes"}})
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if client.Instructions() != "Use docs carefully." {
		t.Fatalf("instructions = %q", client.Instructions())
	}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Fatalf("tools = %#v, want search", tools)
	}
	result, err := client.CallTool(context.Background(), "search", json.RawMessage(`{"q":"go"}`))
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result.Text() != "result text" || !sawAuth {
		t.Fatalf("result=%#v sawAuth=%v, want result text and auth headers", result, sawAuth)
	}
}

// TestOAuthTokenStoreRoundTrips 验证 OAuth token skeleton 使用受限路径持久化。
func TestOAuthTokenStoreRoundTrips(t *testing.T) {
	root := t.TempDir()
	token := OAuthToken{ServerName: "docs", AccessToken: "access", RefreshToken: "refresh"}

	if err := SaveOAuthToken(root, token); err != nil {
		t.Fatalf("SaveOAuthToken returned error: %v", err)
	}
	got, err := LoadOAuthToken(root, "docs")
	if err != nil {
		t.Fatalf("LoadOAuthToken returned error: %v", err)
	}
	if got.AccessToken != "access" || got.RefreshToken != "refresh" {
		t.Fatalf("token = %#v, want saved token", got)
	}
	if err := SaveOAuthToken(root, OAuthToken{ServerName: "../escape"}); err == nil {
		t.Fatalf("SaveOAuthToken accepted traversal server name")
	}
}

// TestOAuthLoginAndRefresh 验证发现、动态注册、PKCE 回调和 refresh token 构成完整链路。
func TestOAuthLoginAndRefresh(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			_ = json.NewEncoder(w).Encode(map[string]any{"authorization_servers": []string{server.URL + "/issuer"}})
		case "/.well-known/oauth-authorization-server/issuer":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": server.URL + "/issuer", "authorization_endpoint": server.URL + "/authorize",
				"token_endpoint": server.URL + "/token", "registration_endpoint": server.URL + "/register",
			})
		case "/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "codeworld-client"})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("client_id") != "codeworld-client" {
				t.Errorf("client_id = %q", r.Form.Get("client_id"))
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "refreshed", "expires_in": 3600})
				return
			}
			if r.Form.Get("code") != "auth-code" || r.Form.Get("code_verifier") == "" {
				t.Errorf("token form = %#v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "initial", "refresh_token": "refresh", "expires_in": 1, "scope": "tools.read"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	token, err := LoginOAuth(context.Background(), OAuthLoginOptions{
		ServerName: "docs", ServerURL: server.URL + "/mcp", Scopes: []string{"tools.read"},
		HTTPClient: server.Client(), NotifyURL: func(string) {},
		OpenURL: func(target string) error {
			authURL, err := url.Parse(target)
			if err != nil {
				return err
			}
			callback, err := url.Parse(authURL.Query().Get("redirect_uri"))
			if err != nil {
				return err
			}
			query := callback.Query()
			query.Set("code", "auth-code")
			query.Set("state", authURL.Query().Get("state"))
			callback.RawQuery = query.Encode()
			go func() {
				resp, callbackErr := http.Get(callback.String())
				if callbackErr == nil {
					_ = resp.Body.Close()
				}
			}()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("LoginOAuth returned error: %v", err)
	}
	if token.AccessToken != "initial" || token.RefreshToken != "refresh" || token.ClientID != "codeworld-client" {
		t.Fatalf("token = %#v", token)
	}
	root := t.TempDir()
	token.ExpiresAt = time.Now().Add(-time.Minute)
	if err := SaveOAuthToken(root, token); err != nil {
		t.Fatal(err)
	}
	refreshed, err := RefreshOAuthToken(context.Background(), root, "docs", server.Client())
	if err != nil {
		t.Fatalf("RefreshOAuthToken returned error: %v", err)
	}
	if refreshed.AccessToken != "refreshed" || refreshed.RefreshToken != "refresh" {
		t.Fatalf("refreshed token = %#v", refreshed)
	}
	if err := DeleteOAuthToken(root, "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOAuthToken(root, "docs"); !os.IsNotExist(err) {
		t.Fatalf("LoadOAuthToken after delete = %v", err)
	}
}

// TestHTTPClientUsesOAuthTokenAsFallback 验证环境变量 bearer token 未配置时使用 OAuth token。
func TestHTTPClientUsesOAuthTokenAsFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oauth-value" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var req struct {
			ID int `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
	}))
	defer server.Close()
	client := NewHTTPClient(ServerConfig{Name: "docs", URL: server.URL, OAuthAccessToken: "oauth-value"})
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOAuthCallbackRequiresLoopbackHost(t *testing.T) {
	if _, listener, err := oauthCallback("http://example.com/callback", 0); err == nil {
		_ = listener.Close()
		t.Fatal("accepted non-loopback OAuth callback")
	}
	callback, listener, err := oauthCallback("http://127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if !strings.HasSuffix(callback, "/") {
		t.Fatalf("callback = %q", callback)
	}
}

// runFakeServer 是测试辅助函数，用于复用测试准备或断言逻辑。
func runFakeServer() {
	reader := bufio.NewReader(os.Stdin)
	for {
		msg, err := readFramed(reader)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintln(os.Stderr, err)
			}
			return
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(msg, &req); err != nil {
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "fake", "version": "test"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "Echo text", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			var params struct {
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &params)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": params.Arguments["text"]}}}
		case "resources/list":
			result = map[string]any{"resources": []any{map[string]any{"uri": "file:///demo.md", "name": "demo"}}}
		case "resources/read":
			result = map[string]any{"contents": []any{map[string]any{"uri": "file:///demo.md", "text": "resource text"}}}
		case "prompts/list":
			result = map[string]any{"prompts": []any{map[string]any{"name": "review", "description": "Review code"}}}
		default:
			result = map[string]any{}
		}
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(data), data)
	}
}

// readFramed 是测试辅助函数，用于复用测试准备或断言逻辑。
func readFramed(reader *bufio.Reader) ([]byte, error) {
	var length int
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if _, err := fmt.Sscanf(line, "Content-Length: %d", &length); err != nil {
			return nil, err
		}
	}
	if length <= 0 {
		return nil, fmt.Errorf("missing content length")
	}
	data := make([]byte, length)
	_, err := io.ReadFull(reader, data)
	return data, err
}
