package tools

import (
	"context"
	"encoding/json"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
)

type Result struct {
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Tool interface {
	Definition() model.ToolDefinition
	PermissionRequest(args json.RawMessage) (permissions.Request, error)
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}
