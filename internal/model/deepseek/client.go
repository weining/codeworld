package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"codeworld/internal/model"
)

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
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
	if c.apiKey == "" {
		return model.GenerateResponse{}, fmt.Errorf("DEEPSEEK_API_KEY is not set")
	}
	body := requestBody{
		Model:    firstNonEmpty(req.Model, c.model),
		Messages: toProviderMessages(req.Messages),
		Tools:    toProviderTools(req.Tools),
	}
	data, err := json.Marshal(body)
	if err != nil {
		return model.GenerateResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/chat/completions", bytes.NewReader(data))
	if err != nil {
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
		return model.GenerateResponse{}, err
	}
	defer httpResp.Body.Close()

	respData, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return model.GenerateResponse{}, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return model.GenerateResponse{}, fmt.Errorf("deepseek status %d: %s", httpResp.StatusCode, redactAPIKey(string(respData), c.apiKey))
	}

	var providerResp responseBody
	if err := json.Unmarshal(respData, &providerResp); err != nil {
		return model.GenerateResponse{}, err
	}
	if len(providerResp.Choices) == 0 {
		return model.GenerateResponse{}, fmt.Errorf("deepseek returned no choices")
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
	}, nil
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
