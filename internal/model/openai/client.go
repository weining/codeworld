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

func (c *Client) SetLogger(logger CallLogger) {
	c.logger = logger
}

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
	finalText := msg.Content
	if len(toolCalls) > 0 {
		finalText = ""
	}
	return model.GenerateResponse{
		Message: model.Message{
			Role:      model.RoleAssistant,
			Content:   msg.Content,
			ToolCalls: toolCalls,
		},
		ToolCalls: toolCalls,
		FinalText: finalText,
		Usage:     providerResp.Usage.toModelUsage(),
	}, nil
}

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
	Content    string             `json:"content,omitempty"`
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

func toProviderMessages(messages []model.Message) []providerMessage {
	out := make([]providerMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, providerMessage{
			Role:       string(msg.Role),
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  toProviderToolCalls(msg.ToolCalls),
		})
	}
	return out
}

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
			if choice.Delta.Content != "" {
				finalText += choice.Delta.Content
				if emit != nil {
					if err := emit(model.StreamEvent{Kind: model.StreamEventTextDelta, Delta: choice.Delta.Content}); err != nil {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func redactAPIKey(text, apiKey string) string {
	if apiKey == "" {
		return text
	}
	return strings.ReplaceAll(text, apiKey, "[redacted]")
}

func loggedHTTPResponse(body []byte, apiKey string) *httpResponseLog {
	redactedBody := redactAPIKey(string(body), apiKey)
	return &httpResponseLog{
		BodyJSON: jsonBody(redactedBody),
	}
}

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
