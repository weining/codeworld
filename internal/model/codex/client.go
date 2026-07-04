package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codeworld/internal/model"
)

const defaultBaseURL = "https://chatgpt.com/backend-api"

type Config struct {
	AccessToken string
	AccountID   string
	Model       string
	BaseURL     string
	HTTPClient  *http.Client
}

type Client struct {
	accessToken string
	accountID   string
	model       string
	baseURL     string
	httpClient  *http.Client
	logger      CallLogger
}

type requestBody struct {
	Model      string        `json:"model"`
	Stream     bool          `json:"stream"`
	Input      []any         `json:"input"`
	Tools      []toolPayload `json:"tools,omitempty"`
	Store      bool          `json:"store"`
	ToolChoice string        `json:"tool_choice,omitempty"`
}

type toolPayload struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      bool           `json:"strict"`
}

// NewClient 创建 ChatGPT/Codex OAuth Responses 客户端。
func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		accessToken: cfg.AccessToken,
		accountID:   cfg.AccountID,
		model:       cfg.Model,
		baseURL:     baseURL,
		httpClient:  firstHTTPClient(cfg.HTTPClient),
	}
}

// SetLogger 设置模型调用日志记录器。
func (c *Client) SetLogger(logger CallLogger) {
	c.logger = logger
}

// Generate 通过 SSE Responses 请求聚合出非流式结果。
func (c *Client) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	var text string
	var usage model.Usage
	var toolCalls []model.ToolCall
	err := c.Stream(ctx, req, func(event model.StreamEvent) error {
		switch event.Kind {
		case model.StreamEventTextDelta:
			text += event.Delta
		case model.StreamEventUsage:
			usage = usage.Add(event.Usage)
		case model.StreamEventToolCall:
			toolCalls = append(toolCalls, event.ToolCalls...)
		}
		return nil
	})
	if err != nil {
		return model.GenerateResponse{}, err
	}
	message := model.Message{Role: model.RoleAssistant, Content: text, ToolCalls: toolCalls}
	finalText := text
	if len(toolCalls) > 0 {
		finalText = ""
	}
	return model.GenerateResponse{Message: message, ToolCalls: toolCalls, FinalText: finalText, Usage: usage}, nil
}

// Stream 调用 ChatGPT backend 的 /codex/responses SSE 接口。
func (c *Client) Stream(ctx context.Context, req model.GenerateRequest, emit func(model.StreamEvent) error) error {
	body := requestBody{
		Model:  firstNonEmpty(req.Model, c.model),
		Stream: true,
		Input:  toResponsesInput(req.Messages),
		Tools:  toResponsesTools(req.Tools),
		Store:  false,
	}
	if len(body.Tools) > 0 {
		body.ToolChoice = "auto"
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	logEntry := callLogEntry{
		Timestamp: time.Now().UTC(),
		Provider:  "codex",
		Request:   httpRequestLog{BodyJSON: jsonBody(string(data))},
	}
	var text string
	var usage model.Usage
	var toolCalls []model.ToolCall
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(data))
	if err != nil {
		logEntry.Error = err.Error()
		c.logCall(ctx, logEntry)
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.accessToken)
	httpReq.Header.Set("chatgpt-account-id", c.accountID)
	httpReq.Header.Set("originator", "codeworld")
	httpReq.Header.Set("User-Agent", "codeworld")
	httpReq.Header.Set("OpenAI-Beta", "responses=experimental")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		logEntry.Error = err.Error()
		c.logCall(ctx, logEntry)
		return err
	}
	defer resp.Body.Close()
	defer func() {
		logEntry.Response = &httpResponseLog{BodyJSON: jsonBody(streamLogBody(text, toolCalls, usage))}
		c.logCall(ctx, logEntry)
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		err := fmt.Errorf("codex status %d: %s", resp.StatusCode, string(data))
		logEntry.Error = err.Error()
		return err
	}
	return parseSSE(ctx, resp.Body, func(event model.StreamEvent) error {
		switch event.Kind {
		case model.StreamEventTextDelta:
			text += event.Delta
		case model.StreamEventUsage:
			usage = usage.Add(event.Usage)
		case model.StreamEventToolCall:
			toolCalls = append(toolCalls, event.ToolCalls...)
		}
		if emit != nil {
			return emit(event)
		}
		return nil
	})
}

// logCall 写入可选模型调用日志。
func (c *Client) logCall(ctx context.Context, entry callLogEntry) {
	if c.logger != nil {
		_ = c.logger.LogCall(ctx, entry)
	}
}

// endpoint 兼容传入 backend-api、backend-api/codex 或完整 responses URL。
func (c *Client) endpoint() string {
	base := strings.TrimRight(c.baseURL, "/")
	if strings.HasSuffix(base, "/codex/responses") {
		return base
	}
	if strings.HasSuffix(base, "/codex") {
		return base + "/responses"
	}
	return base + "/codex/responses"
}

// parseSSE 解析 Codex Responses SSE 事件，转换为统一流式事件。
func parseSSE(ctx context.Context, reader io.Reader, emit func(model.StreamEvent) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if data == "" || data == "[DONE]" {
			return nil
		}
		return handleSSEData(data, emit)
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

// handleSSEData 处理单个 JSON SSE 事件。
func handleSSEData(data string, emit func(model.StreamEvent) error) error {
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return err
	}
	switch event["type"] {
	case "response.output_text.delta":
		delta, _ := event["delta"].(string)
		if delta != "" && emit != nil {
			return emit(model.StreamEvent{Kind: model.StreamEventTextDelta, Delta: delta})
		}
	case "response.output_item.done":
		if call, ok := parseFunctionCallItem(event["item"]); ok && emit != nil {
			return emit(model.StreamEvent{Kind: model.StreamEventToolCall, ToolCalls: []model.ToolCall{call}})
		}
	case "response.completed":
		if usage := parseUsage(event["response"]); !usage.IsZero() && emit != nil {
			return emit(model.StreamEvent{Kind: model.StreamEventUsage, Usage: usage})
		}
	}
	return nil
}

// parseFunctionCallItem 提取 Responses function_call item。
func parseFunctionCallItem(value any) (model.ToolCall, bool) {
	item, ok := value.(map[string]any)
	if !ok || item["type"] != "function_call" {
		return model.ToolCall{}, false
	}
	name, _ := item["name"].(string)
	callID, _ := item["call_id"].(string)
	if callID == "" {
		callID, _ = item["id"].(string)
	}
	args, _ := item["arguments"].(string)
	if args == "" {
		args = "{}"
	}
	if name == "" || callID == "" {
		return model.ToolCall{}, false
	}
	return model.ToolCall{ID: callID, Name: name, Arguments: json.RawMessage(args)}, true
}

// parseUsage 提取 Responses usage 字段。
func parseUsage(value any) model.Usage {
	response, ok := value.(map[string]any)
	if !ok {
		return model.Usage{}
	}
	usage, _ := response["usage"].(map[string]any)
	input := intFromAny(usage["input_tokens"])
	output := intFromAny(usage["output_tokens"])
	total := intFromAny(usage["total_tokens"])
	var cached int
	if details, ok := usage["input_tokens_details"].(map[string]any); ok {
		cached = intFromAny(details["cached_tokens"])
	}
	return model.Usage{InputTokens: input, OutputTokens: output, CacheTokens: cached, TotalTokens: total}
}

// toResponsesInput 将当前模型消息转换为 Responses input。
func toResponsesInput(messages []model.Message) []any {
	out := make([]any, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case model.RoleSystem:
			out = append(out, messageItem("developer", "input_text", msg.Content))
		case model.RoleUser:
			out = append(out, messageItem("user", "input_text", msg.Content))
		case model.RoleAssistant:
			if msg.Content != "" {
				out = append(out, messageItem("assistant", "output_text", msg.Content))
			}
			for _, call := range msg.ToolCalls {
				out = append(out, map[string]any{
					"type":      "function_call",
					"call_id":   call.ID,
					"name":      call.Name,
					"arguments": string(call.Arguments),
				})
			}
		case model.RoleTool:
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  msg.Content,
			})
		}
	}
	return out
}

// messageItem 构造 Responses message item。
func messageItem(role, contentType, text string) map[string]any {
	return map[string]any{
		"type": "message",
		"role": role,
		"content": []map[string]any{{
			"type": contentType,
			"text": text,
		}},
	}
}

// toResponsesTools 转换工具 schema。
func toResponsesTools(defs []model.ToolDefinition) []toolPayload {
	out := make([]toolPayload, 0, len(defs))
	for _, def := range defs {
		out = append(out, toolPayload{
			Type:        "function",
			Name:        def.Name,
			Description: def.Description,
			Parameters:  def.InputSchema,
			Strict:      false,
		})
	}
	return out
}

func firstHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func streamLogBody(finalText string, toolCalls []model.ToolCall, usage model.Usage) string {
	data, err := json.Marshal(map[string]any{
		"final_text": finalText,
		"tool_calls": toolCalls,
		"usage":      usage,
	})
	if err != nil {
		return "{}"
	}
	return string(data)
}

func jsonBody(body string) json.RawMessage {
	if json.Valid([]byte(body)) {
		return json.RawMessage(body)
	}
	data, _ := json.Marshal(map[string]string{"raw": body})
	return data
}
