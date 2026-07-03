package openai

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

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
	logger     CallLogger
}

// NewClient 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewClient(apiKey, modelName, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &Client{
		apiKey:     apiKey,
		model:      modelName,
		baseURL:    baseURL,
		httpClient: http.DefaultClient,
	}
}

// SetLogger 提供对外可复用的能力，并隐藏内部实现细节。
func (c *Client) SetLogger(logger CallLogger) {
	c.logger = logger
}

// Generate 发起一次非流式模型调用，并解析文本、工具调用和用量信息。
func (c *Client) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	body := requestBody{
		Model:    firstNonEmpty(req.Model, c.model),
		Messages: toProviderMessages(req.Messages),
		Tools:    toProviderTools(req.Tools),
	}
	data, err := json.Marshal(body)
	if err != nil {
		return model.GenerateResponse{}, err
	}
	endpoint := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	redactedRequestBody := redactAPIKey(string(data), c.apiKey)
	logEntry := callLogEntry{
		Timestamp: time.Now().UTC(),
		Provider:  "openai",
		Request: httpRequestLog{
			BodyJSON: jsonBody(redactedRequestBody),
		},
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		logEntry.Error = err.Error()
		c.logCall(ctx, logEntry)
		return model.GenerateResponse{}, err
	}
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		logEntry.Error = redactAPIKey(err.Error(), c.apiKey)
		c.logCall(ctx, logEntry)
		return model.GenerateResponse{}, err
	}
	defer httpResp.Body.Close()

	respData, err := io.ReadAll(httpResp.Body)
	if err != nil {
		logEntry.Response = loggedHTTPResponse(nil, c.apiKey)
		logEntry.Error = redactAPIKey(err.Error(), c.apiKey)
		c.logCall(ctx, logEntry)
		return model.GenerateResponse{}, err
	}
	logEntry.Response = loggedHTTPResponse(respData, c.apiKey)
	defer func() {
		c.logCall(ctx, logEntry)
	}()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		err := fmt.Errorf("openai status %d: %s", httpResp.StatusCode, redactAPIKey(string(respData), c.apiKey))
		logEntry.Error = err.Error()
		return model.GenerateResponse{}, err
	}

	var providerResp responseBody
	if err := json.Unmarshal(respData, &providerResp); err != nil {
		logEntry.Error = err.Error()
		return model.GenerateResponse{}, err
	}
	if len(providerResp.Choices) == 0 {
		err := fmt.Errorf("openai returned no choices")
		logEntry.Error = err.Error()
		return model.GenerateResponse{}, err
	}

	msg := providerResp.Choices[0].Message
	toolCalls := fromProviderToolCalls(msg.ToolCalls)
	finalText := providerContentText(msg.Content)
	if len(toolCalls) > 0 {
		finalText = ""
	}
	return model.GenerateResponse{
		Message: model.Message{
			Role:      model.RoleAssistant,
			Content:   providerContentText(msg.Content),
			ToolCalls: toolCalls,
		},
		ToolCalls: toolCalls,
		FinalText: finalText,
		Usage:     providerResp.Usage.toModelUsage(),
	}, nil
}

// Stream 发起一次流式模型调用，并把增量事件转换为统一事件。
func (c *Client) Stream(ctx context.Context, req model.GenerateRequest, emit func(model.StreamEvent) error) error {
	body := requestBody{
		Model:    firstNonEmpty(req.Model, c.model),
		Messages: toProviderMessages(req.Messages),
		Tools:    toProviderTools(req.Tools),
		Stream:   true,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	logEntry := callLogEntry{
		Timestamp: time.Now().UTC(),
		Provider:  "openai",
		Request: httpRequestLog{
			BodyJSON: jsonBody(redactAPIKey(string(data), c.apiKey)),
		},
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		logEntry.Error = err.Error()
		c.logCall(ctx, logEntry)
		return err
	}
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		logEntry.Error = redactAPIKey(err.Error(), c.apiKey)
		c.logCall(ctx, logEntry)
		return err
	}
	defer httpResp.Body.Close()
	defer func() {
		c.logCall(ctx, logEntry)
	}()
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respData, _ := io.ReadAll(httpResp.Body)
		logEntry.Response = loggedHTTPResponse(respData, c.apiKey)
		err := fmt.Errorf("openai status %d: %s", httpResp.StatusCode, redactAPIKey(string(respData), c.apiKey))
		logEntry.Error = err.Error()
		return err
	}

	finalText, toolCalls, usage, err := parseStream(ctx, httpResp.Body, emit)
	if err != nil {
		logEntry.Error = redactAPIKey(err.Error(), c.apiKey)
		return err
	}
	logEntry.Response = &httpResponseLog{BodyJSON: jsonBody(redactAPIKey(streamLogBody(finalText, toolCalls, usage), c.apiKey))}
	return nil
}

// logCall 封装局部逻辑，保持调用方流程清晰。
func (c *Client) logCall(ctx context.Context, entry callLogEntry) {
	if c.logger != nil {
		_ = c.logger.LogCall(ctx, entry)
	}
}

type requestBody struct {
	Model    string            `json:"model"`
	Messages []providerMessage `json:"messages"`
	Tools    []providerTool    `json:"tools,omitempty"`
	Stream   bool              `json:"stream,omitempty"`
}

type responseBody struct {
	Choices []struct {
		Message providerMessage `json:"message"`
	} `json:"choices"`
	Usage providerUsage `json:"usage"`
}

type streamChunk struct {
	Choices []struct {
		Delta providerMessage `json:"delta"`
	} `json:"choices"`
	Usage providerUsage `json:"usage"`
}

type providerUsage struct {
	PromptTokens        int                       `json:"prompt_tokens"`
	CompletionTokens    int                       `json:"completion_tokens"`
	TotalTokens         int                       `json:"total_tokens"`
	PromptTokensDetails providerPromptTokenDetail `json:"prompt_tokens_details"`
}

type providerPromptTokenDetail struct {
	CachedTokens int `json:"cached_tokens"`
}

type providerMessage struct {
	Role       string             `json:"role"`
	Content    any                `json:"content,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolCalls  []providerToolCall `json:"tool_calls,omitempty"`
}

type providerTool struct {
	Type     string           `json:"type"`
	Function providerFunction `json:"function"`
}

type providerFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type providerToolCall struct {
	Index    int                  `json:"index,omitempty"`
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function providerCallFunction `json:"function"`
}

type providerCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// toProviderMessages 在不同层的数据结构之间做显式转换。
func toProviderMessages(messages []model.Message) []providerMessage {
	out := make([]providerMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, providerMessage{
			Role:       string(msg.Role),
			Content:    providerContent(msg),
			ToolCallID: msg.ToolCallID,
			ToolCalls:  toProviderToolCalls(msg.ToolCalls),
		})
	}
	return out
}

// providerContent 按 OpenAI-compatible 格式输出纯文本或多模态 content。
func providerContent(msg model.Message) any {
	if len(msg.Parts) == 0 {
		return msg.Content
	}
	blocks := make([]map[string]any, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		switch part.Type {
		case model.ContentPartText:
			if part.Text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": part.Text})
			}
		case model.ContentPartImage:
			if part.ImageURL != "" {
				blocks = append(blocks, map[string]any{"type": "image_url", "image_url": map[string]any{"url": part.ImageURL}})
			}
		}
	}
	return blocks
}

// providerContentText 从 provider 响应 content 中提取文本。
func providerContentText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	return ""
}

// toProviderTools 在不同层的数据结构之间做显式转换。
func toProviderTools(defs []model.ToolDefinition) []providerTool {
	out := make([]providerTool, 0, len(defs))
	for _, def := range defs {
		out = append(out, providerTool{
			Type: "function",
			Function: providerFunction{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.InputSchema,
			},
		})
	}
	return out
}

// toProviderToolCalls 在不同层的数据结构之间做显式转换。
func toProviderToolCalls(calls []model.ToolCall) []providerToolCall {
	out := make([]providerToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, providerToolCall{
			ID:   call.ID,
			Type: "function",
			Function: providerCallFunction{
				Name:      call.Name,
				Arguments: string(call.Arguments),
			},
		})
	}
	return out
}

// fromProviderToolCalls 在不同层的数据结构之间做显式转换。
func fromProviderToolCalls(calls []providerToolCall) []model.ToolCall {
	out := make([]model.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, model.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: json.RawMessage(call.Function.Arguments),
		})
	}
	return out
}

type streamToolCall struct {
	id        string
	name      string
	arguments string
}

// appendStreamToolCallDeltas 封装局部逻辑，保持调用方流程清晰。
func appendStreamToolCallDeltas(order *[]int, calls map[int]*streamToolCall, deltas []providerToolCall) {
	for _, delta := range deltas {
		call, ok := calls[delta.Index]
		if !ok {
			call = &streamToolCall{}
			calls[delta.Index] = call
			*order = append(*order, delta.Index)
		}
		if delta.ID != "" {
			call.id = delta.ID
		}
		if delta.Function.Name != "" {
			call.name = delta.Function.Name
		}
		call.arguments += delta.Function.Arguments
	}
}

// streamToolCalls 封装局部逻辑，保持调用方流程清晰。
func streamToolCalls(order []int, calls map[int]*streamToolCall) []model.ToolCall {
	out := make([]model.ToolCall, 0, len(order))
	for _, index := range order {
		call := calls[index]
		if call == nil || (call.id == "" && call.name == "" && call.arguments == "") {
			continue
		}
		out = append(out, model.ToolCall{
			ID:        call.id,
			Name:      call.name,
			Arguments: json.RawMessage(call.arguments),
		})
	}
	return out
}

// parseStream 解析输入数据，并执行必要的格式校验。
func parseStream(ctx context.Context, body io.Reader, emit func(model.StreamEvent) error) (string, []model.ToolCall, model.Usage, error) {
	scanner := bufio.NewScanner(body)
	var finalText string
	var usage model.Usage
	toolCallDeltas := map[int]*streamToolCall{}
	var toolCallOrder []int
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return finalText, nil, usage, err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			toolCalls := streamToolCalls(toolCallOrder, toolCallDeltas)
			if emit != nil {
				if err := emit(model.StreamEvent{Kind: model.StreamEventDone, Message: model.Message{Role: model.RoleAssistant, Content: finalText, ToolCalls: toolCalls}, ToolCalls: toolCalls}); err != nil {
					return finalText, toolCalls, usage, err
				}
			}
			return finalText, toolCalls, usage, nil
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return finalText, nil, usage, err
		}
		for _, choice := range chunk.Choices {
			if len(choice.Delta.ToolCalls) > 0 {
				appendStreamToolCallDeltas(&toolCallOrder, toolCallDeltas, choice.Delta.ToolCalls)
			}
			if deltaText := providerContentText(choice.Delta.Content); deltaText != "" {
				finalText += deltaText
				if emit != nil {
					if err := emit(model.StreamEvent{Kind: model.StreamEventTextDelta, Delta: deltaText}); err != nil {
						return finalText, nil, usage, err
					}
				}
			}
		}
		if chunk.Usage != (providerUsage{}) {
			nextUsage := chunk.Usage.toModelUsage()
			usage = usage.Add(nextUsage)
			if emit != nil {
				if err := emit(model.StreamEvent{Kind: model.StreamEventUsage, Usage: nextUsage}); err != nil {
					return finalText, nil, usage, err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return finalText, nil, usage, err
	}
	toolCalls := streamToolCalls(toolCallOrder, toolCallDeltas)
	if emit != nil {
		if err := emit(model.StreamEvent{Kind: model.StreamEventDone, Message: model.Message{Role: model.RoleAssistant, Content: finalText, ToolCalls: toolCalls}, ToolCalls: toolCalls}); err != nil {
			return finalText, toolCalls, usage, err
		}
	}
	return finalText, toolCalls, usage, nil
}

// streamLogBody 封装局部逻辑，保持调用方流程清晰。
func streamLogBody(finalText string, toolCalls []model.ToolCall, usage model.Usage) string {
	message := map[string]any{"role": "assistant", "content": finalText}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toProviderToolCalls(toolCalls)
	}
	data, err := json.Marshal(map[string]any{
		"choices": []map[string]any{{"message": message}},
		"usage": map[string]any{
			"prompt_tokens":     usage.InputTokens,
			"completion_tokens": usage.OutputTokens,
			"total_tokens":      usage.TotalTokens,
			"prompt_tokens_details": map[string]any{
				"cached_tokens": usage.CacheTokens,
			},
		},
	})
	if err != nil {
		return ""
	}
	return string(data)
}

// toModelUsage 在不同层的数据结构之间做显式转换。
func (u providerUsage) toModelUsage() model.Usage {
	totalTokens := u.TotalTokens
	if totalTokens == 0 && (u.PromptTokens != 0 || u.CompletionTokens != 0) {
		totalTokens = u.PromptTokens + u.CompletionTokens
	}
	return model.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		CacheTokens:  u.PromptTokensDetails.CachedTokens,
		TotalTokens:  totalTokens,
	}
}

// firstNonEmpty 从候选值中选择满足条件的结果。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// redactAPIKey 封装局部逻辑，保持调用方流程清晰。
func redactAPIKey(text, apiKey string) string {
	if apiKey == "" {
		return text
	}
	return strings.ReplaceAll(text, apiKey, "[redacted]")
}

// loggedHTTPResponse 封装局部逻辑，保持调用方流程清晰。
func loggedHTTPResponse(body []byte, apiKey string) *httpResponseLog {
	redactedBody := redactAPIKey(string(body), apiKey)
	return &httpResponseLog{
		BodyJSON: jsonBody(redactedBody),
	}
}

// jsonBody 封装局部逻辑，保持调用方流程清晰。
func jsonBody(body string) json.RawMessage {
	if body == "" || !json.Valid([]byte(body)) {
		return nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(body)); err != nil {
		return json.RawMessage(body)
	}
	return json.RawMessage(compact.Bytes())
}
