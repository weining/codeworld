package model

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role          `json:"role"`
	Content    string        `json:"content,omitempty"`
	Parts      []ContentPart `json:"parts,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
}

type ContentPartType string

const (
	ContentPartText  ContentPartType = "text"
	ContentPartImage ContentPartType = "image"
)

type ContentPart struct {
	Type      ContentPartType `json:"type"`
	Text      string          `json:"text,omitempty"`
	ImageURL  string          `json:"image_url,omitempty"`
	MediaType string          `json:"media_type,omitempty"`
	Data      string          `json:"data,omitempty"`
	Path      string          `json:"path,omitempty"`
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

// TextPart 构造文本 content part，便于多模态消息同时保留旧 Content 字段。
func TextPart(text string) ContentPart {
	return ContentPart{Type: ContentPartText, Text: text}
}

// ImagePartFromFile 把本地图片读成 base64 data URL，供支持多模态的 provider 使用。
func ImagePartFromFile(path string) (ContentPart, error) {
	if path == "" {
		return ContentPart{}, fmt.Errorf("image path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ContentPart{}, err
	}
	mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return ContentPart{
		Type:      ContentPartImage,
		ImageURL:  "data:" + mediaType + ";base64," + encoded,
		MediaType: mediaType,
		Data:      encoded,
		Path:      path,
	}, nil
}

// HasImageParts 判断消息中是否包含图片，用于 provider 能力校验。
func HasImageParts(messages []Message) bool {
	for _, msg := range messages {
		for _, part := range msg.Parts {
			if part.Type == ContentPartImage {
				return true
			}
		}
	}
	return false
}

// Add 提供对外可复用的能力，并隐藏内部实现细节。
func (u Usage) Add(next Usage) Usage {
	return Usage{
		InputTokens:  u.InputTokens + next.InputTokens,
		OutputTokens: u.OutputTokens + next.OutputTokens,
		CacheTokens:  u.CacheTokens + next.CacheTokens,
		TotalTokens:  u.TotalTokens + next.TotalTokens,
	}
}

// IsZero 判断输入是否满足特定条件，并用于后续分支决策。
func (u Usage) IsZero() bool {
	return u == Usage{}
}
