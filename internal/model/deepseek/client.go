package deepseek

import (
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

func NewClient(apiKey, modelName string) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      modelName,
		baseURL:    "https://api.deepseek.com",
		httpClient: http.DefaultClient,
	}
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
		Provider:  "deepseek",
		Request: httpRequestLog{
			BodyJSON: jsonBody(redactedRequestBody),
		},
	}

	if c.apiKey == "" {
		err := fmt.Errorf("DEEPSEEK_API_KEY is not set")
		logEntry.Error = err.Error()
		c.logCall(ctx, logEntry)
		return model.GenerateResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		logEntry.Error = err.Error()
		c.logCall(ctx, logEntry)
		return model.GenerateResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
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
		logEntry.Response = loggedHTTPResponse(httpResp, nil, c.apiKey)
		logEntry.Error = redactAPIKey(err.Error(), c.apiKey)
		c.logCall(ctx, logEntry)
		return model.GenerateResponse{}, err
	}
	logEntry.Response = loggedHTTPResponse(httpResp, respData, c.apiKey)
	defer func() {
		c.logCall(ctx, logEntry)
	}()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		err := fmt.Errorf("deepseek status %d: %s", httpResp.StatusCode, redactAPIKey(string(respData), c.apiKey))
		logEntry.Error = err.Error()
		return model.GenerateResponse{}, err
	}

	var providerResp responseBody
	if err := json.Unmarshal(respData, &providerResp); err != nil {
		logEntry.Error = err.Error()
		return model.GenerateResponse{}, err
	}
	if len(providerResp.Choices) == 0 {
		err := fmt.Errorf("deepseek returned no choices")
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

func (c *Client) SetLogger(logger CallLogger) {
	c.logger = logger
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
}

type responseBody struct {
	Choices []struct {
		Message providerMessage `json:"message"`
	} `json:"choices"`
	Usage providerUsage `json:"usage"`
}

type providerUsage struct {
	PromptTokens          int                       `json:"prompt_tokens"`
	CompletionTokens      int                       `json:"completion_tokens"`
	TotalTokens           int                       `json:"total_tokens"`
	PromptCacheHitTokens  int                       `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int                       `json:"prompt_cache_miss_tokens"`
	PromptTokensDetails   providerPromptTokenDetail `json:"prompt_tokens_details"`
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

func (u providerUsage) toModelUsage() model.Usage {
	cacheTokens := u.PromptCacheHitTokens
	if cacheTokens == 0 {
		cacheTokens = u.PromptTokensDetails.CachedTokens
	}
	totalTokens := u.TotalTokens
	if totalTokens == 0 && (u.PromptTokens != 0 || u.CompletionTokens != 0) {
		totalTokens = u.PromptTokens + u.CompletionTokens
	}
	return model.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		CacheTokens:  cacheTokens,
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

func loggedHTTPResponse(resp *http.Response, body []byte, apiKey string) *httpResponseLog {
	if resp == nil {
		return nil
	}
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
