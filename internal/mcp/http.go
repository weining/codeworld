package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

type HTTPClient struct {
	name         string
	url          string
	bearerEnv    string
	oauthToken   string
	headers      []string
	httpClient   *http.Client
	mu           sync.Mutex
	nextID       int
	instructions string
}

// NewHTTPClient 创建 HTTP MCP client，使用 JSON-RPC POST 与 streamable HTTP server 通信。
func NewHTTPClient(cfg ServerConfig) *HTTPClient {
	return &HTTPClient{
		name:       cfg.Name,
		url:        cfg.URL,
		bearerEnv:  cfg.BearerTokenEnvVar,
		oauthToken: cfg.OAuthAccessToken,
		headers:    append([]string(nil), cfg.HTTPHeaders...),
		httpClient: http.DefaultClient,
	}
}

// Initialize 初始化 HTTP MCP server，并缓存 server instructions。
func (c *HTTPClient) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]any{"name": "codeworld", "version": "dev"},
		"capabilities":    map[string]any{},
	}
	var result struct {
		Instructions string `json:"instructions"`
	}
	if err := c.request(ctx, "initialize", params, &result); err != nil {
		return err
	}
	c.instructions = result.Instructions
	return nil
}

// Instructions 返回 MCP server 初始化时提供的跨工具说明。
func (c *HTTPClient) Instructions() string {
	return c.instructions
}

// ListTools 列出 HTTP MCP server 暴露的 tools。
func (c *HTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.request(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool 调用 HTTP MCP tool。
func (c *HTTPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (CallToolResult, error) {
	var arguments map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return CallToolResult{}, err
		}
	}
	params := map[string]any{"name": name, "arguments": arguments}
	var result CallToolResult
	if err := c.request(ctx, "tools/call", params, &result); err != nil {
		return CallToolResult{}, err
	}
	return result, nil
}

// request 发送一次 HTTP JSON-RPC 请求，并解析对应响应。
func (c *HTTPClient) request(ctx context.Context, method string, params any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.url == "" {
		return fmt.Errorf("mcp server %q url is required", c.name)
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	reqBody := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.bearerEnv != "" {
		if token := os.Getenv(c.bearerEnv); token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+token)
		}
	}
	if httpReq.Header.Get("Authorization") == "" && c.oauthToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.oauthToken)
	}
	for _, header := range c.headers {
		name, value, ok := strings.Cut(header, ":")
		if !ok {
			continue
		}
		httpReq.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mcp http %s status %d", method, resp.StatusCode)
	}
	var rpcResp struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return err
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("mcp %s error %d: %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(rpcResp.Result, out)
}
