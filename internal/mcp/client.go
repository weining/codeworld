package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type ServerConfig struct {
	Name              string
	Command           string
	Args              []string
	Env               []string
	URL               string
	BearerTokenEnvVar string
	OAuthAccessToken  string
	HTTPHeaders       []string
}

type Client struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
	nextID int
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

type Prompt struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Text 提供对外可复用的能力，并隐藏内部实现细节。
func (r CallToolResult) Text() string {
	parts := make([]string, 0, len(r.Content))
	for _, content := range r.Content {
		if content.Text != "" {
			parts = append(parts, content.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// StartStdio 提供对外可复用的能力，并隐藏内部实现细节。
func StartStdio(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp server %q command is required", cfg.Name)
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Client{name: cfg.Name, cmd: cmd, stdin: stdin, reader: bufio.NewReader(stdout)}, nil
}

// Close 释放持有的资源，避免后台进程或句柄泄漏。
func (c *Client) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	if c.cmd != nil {
		_ = c.cmd.Wait()
	}
	return nil
}

// Initialize 提供对外可复用的能力，并隐藏内部实现细节。
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]any{"name": "codeworld", "version": "dev"},
		"capabilities":    map[string]any{},
	}
	var result map[string]any
	return c.request(ctx, "initialize", params, &result)
}

// ListTools 列出可用资源，并把 provider 结果转换为本地结构。
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.request(ctx, "tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool 调用外部能力，并把响应转换为统一结果。
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (CallToolResult, error) {
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

// ListResources 列出可用资源，并把 provider 结果转换为本地结构。
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var result struct {
		Resources []Resource `json:"resources"`
	}
	if err := c.request(ctx, "resources/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Resources, nil
}

// ReadResource 读取外部输入，并保持调用方可处理的错误语义。
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	var result struct {
		Contents []ResourceContent `json:"contents"`
	}
	if err := c.request(ctx, "resources/read", map[string]any{"uri": uri}, &result); err != nil {
		return nil, err
	}
	return result.Contents, nil
}

// ListPrompts 列出可用资源，并把 provider 结果转换为本地结构。
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var result struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := c.request(ctx, "prompts/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Prompts, nil
}

// request 封装局部逻辑，保持调用方流程清晰。
func (c *Client) request(ctx context.Context, method string, params any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// stdio MCP 是单连接 JSON-RPC；串行化请求可以避免响应乱序时误读同一个 stdout。
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	id := c.nextID
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := writeMessage(c.stdin, data); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		respData, err := readMessage(c.reader)
		if err != nil {
			return err
		}
		var resp struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal(respData, &resp); err != nil {
			return err
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("mcp %s error %d: %s", method, resp.Error.Code, resp.Error.Message)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
}

// writeMessage 写入输出数据，并保证必要的目录或权限约束。
func writeMessage(w io.Writer, data []byte) error {
	// 当前 MCP stdio transport 是一行一个 JSON-RPC 消息。
	data = append(append([]byte(nil), data...), '\n')
	_, err := w.Write(data)
	return err
}

// readMessage 优先读取 JSONL，同时兼容旧 server 的 Content-Length 响应帧。
func readMessage(reader *bufio.Reader) ([]byte, error) {
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		trimmed := bytesTrimLine(line)
		if len(trimmed) == 0 {
			continue
		}
		if json.Valid(trimmed) {
			return trimmed, nil
		}
		var length int
		if _, err := fmt.Sscanf(string(trimmed), "Content-Length: %d", &length); err != nil || length <= 0 {
			return nil, fmt.Errorf("invalid MCP stdio message header %q", trimmed)
		}
		for {
			header, err := reader.ReadString('\n')
			if err != nil {
				return nil, err
			}
			if strings.TrimRight(header, "\r\n") == "" {
				break
			}
		}
		data := make([]byte, length)
		_, err = io.ReadFull(reader, data)
		return data, err
	}
}

func bytesTrimLine(data []byte) []byte {
	return bytes.TrimRight(data, "\r\n")
}
