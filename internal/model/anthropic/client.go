package anthropic

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

const defaultVersion = "2023-06-01"

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	version    string
	httpClient *http.Client
	logger     CallLogger
}

func NewClient(apiKey, modelName string) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      modelName,
		baseURL:    "https://api.anthropic.com",
		version:    defaultVersion,
		httpClient: http.DefaultClient,
	}
}

func (c *Client) SetLogger(logger CallLogger) {
	c.logger = logger
}

func (c *Client) Generate(ctx context.Context, req model.GenerateRequest) (model.GenerateResponse, error) {
	system, messages := toProviderMessages(req.Messages)
	body := requestBody{
		Model:     firstNonEmpty(req.Model, c.model),
		MaxTokens: 4096,
		System:    system,
		Messages:  messages,
		Tools:     toProviderTools(req.Tools),
	}
	data, err := json.Marshal(body)
	if err != nil {
		return model.GenerateResponse{}, err
	}
	endpoint := strings.TrimRight(c.baseURL, "/") + "/v1/messages"
	redactedRequestBody := redactAPIKey(string(data), c.apiKey)
	logEntry := callLogEntry{
		Timestamp: time.Now().UTC(),
		Provider:  "anthropic",
		Request: httpRequestLog{
			BodyJSON: jsonBody(redactedRequestBody),
		},
	}

	if c.apiKey == "" {
		err := fmt.Errorf("ANTHROPIC_API_KEY is not set")
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
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", firstNonEmpty(c.version, defaultVersion))
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
		err := fmt.Errorf("anthropic status %d: %s", httpResp.StatusCode, redactAPIKey(string(respData), c.apiKey))
		logEntry.Error = err.Error()
		return model.GenerateResponse{}, err
	}

	var providerResp responseBody
	if err := json.Unmarshal(respData, &providerResp); err != nil {
		logEntry.Error = err.Error()
		return model.GenerateResponse{}, err
	}
	finalText, toolCalls := responseContent(providerResp.Content)
	if len(toolCalls) > 0 {
		return model.GenerateResponse{
			Message:   model.Message{Role: model.RoleAssistant, Content: finalText, ToolCalls: toolCalls},
			ToolCalls: toolCalls,
			Usage:     providerResp.Usage.toModelUsage(),
		}, nil
	}
	if finalText == "" {
		err := fmt.Errorf("anthropic returned no final text")
		logEntry.Error = err.Error()
		return model.GenerateResponse{}, err
	}
	return model.GenerateResponse{
		Message:   model.Message{Role: model.RoleAssistant, Content: finalText},
		FinalText: finalText,
		Usage:     providerResp.Usage.toModelUsage(),
	}, nil
}

func (c *Client) logCall(ctx context.Context, entry callLogEntry) {
	if c.logger != nil {
		_ = c.logger.LogCall(ctx, entry)
	}
}

type requestBody struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	System    string            `json:"system,omitempty"`
	Messages  []providerMessage `json:"messages"`
	Tools     []providerTool    `json:"tools,omitempty"`
}

type providerMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type providerTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type responseBody struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
	Usage   providerUsage  `json:"usage"`
}

type providerUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

func toProviderMessages(messages []model.Message) (string, []providerMessage) {
	var system []string
	out := make([]providerMessage, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case model.RoleSystem:
			if msg.Content != "" {
				system = append(system, msg.Content)
			}
		case model.RoleAssistant:
			out = append(out, providerMessage{Role: "assistant", Content: assistantContent(msg)})
		case model.RoleTool:
			out = append(out, providerMessage{
				Role: "user",
				Content: []contentBlock{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
				}},
			})
		default:
			out = append(out, providerMessage{
				Role:    "user",
				Content: textContent(msg.Content),
			})
		}
	}
	return strings.Join(system, "\n\n"), out
}

func textContent(text string) []contentBlock {
	if text == "" {
		return nil
	}
	return []contentBlock{{Type: "text", Text: text}}
}

func assistantContent(msg model.Message) []contentBlock {
	blocks := textContent(msg.Content)
	for _, call := range msg.ToolCalls {
		input := call.Arguments
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		blocks = append(blocks, contentBlock{
			Type:  "tool_use",
			ID:    call.ID,
			Name:  call.Name,
			Input: input,
		})
	}
	return blocks
}

func toProviderTools(defs []model.ToolDefinition) []providerTool {
	out := make([]providerTool, 0, len(defs))
	for _, def := range defs {
		out = append(out, providerTool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
		})
	}
	return out
}

func responseContent(blocks []contentBlock) (string, []model.ToolCall) {
	var texts []string
	var calls []model.ToolCall
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				texts = append(texts, block.Text)
			}
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			calls = append(calls, model.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: input,
			})
		}
	}
	return strings.Join(texts, "\n"), calls
}

func (u providerUsage) toModelUsage() model.Usage {
	return model.Usage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		CacheTokens:  u.CacheReadInputTokens,
		TotalTokens:  u.InputTokens + u.OutputTokens,
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
