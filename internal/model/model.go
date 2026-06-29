package model

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type GenerateRequest struct {
	Model    string           `json:"model"`
	Messages []Message        `json:"messages"`
	Tools    []ToolDefinition `json:"tools"`
}

type GenerateResponse struct {
	Message   Message    `json:"message"`
	ToolCalls []ToolCall `json:"tool_calls"`
	FinalText string     `json:"final_text"`
	Usage     Usage      `json:"usage,omitempty"`
}

type Client interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}

type StreamClient interface {
	Stream(ctx context.Context, req GenerateRequest, emit func(StreamEvent) error) error
}

type StreamEventKind string

const (
	StreamEventTextDelta StreamEventKind = "text_delta"
	StreamEventToolCall  StreamEventKind = "tool_call"
	StreamEventUsage     StreamEventKind = "usage"
	StreamEventDone      StreamEventKind = "done"
	StreamEventError     StreamEventKind = "error"
)

type StreamEvent struct {
	Kind      StreamEventKind
	Delta     string
	Message   Message
	ToolCalls []ToolCall
	Usage     Usage
	Err       error
}

type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	CacheTokens  int `json:"cache_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

func (u Usage) Add(next Usage) Usage {
	return Usage{
		InputTokens:  u.InputTokens + next.InputTokens,
		OutputTokens: u.OutputTokens + next.OutputTokens,
		CacheTokens:  u.CacheTokens + next.CacheTokens,
		TotalTokens:  u.TotalTokens + next.TotalTokens,
	}
}

func (u Usage) IsZero() bool {
	return u == Usage{}
}
