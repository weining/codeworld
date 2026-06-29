package tools

import (
	"context"
	"encoding/json"

	"codeworld/internal/mcp"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
)

type MCPCaller interface {
	CallTool(ctx context.Context, name string, args json.RawMessage) (mcp.CallToolResult, error)
}

type mcpTool struct {
	server string
	spec   mcp.Tool
	client MCPCaller
}

func NewMCPTool(server string, spec mcp.Tool, client MCPCaller) Tool {
	return mcpTool{server: server, spec: spec, client: client}
}

func (t mcpTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "mcp." + t.server + "." + t.spec.Name,
		Description: t.spec.Description,
		InputSchema: t.spec.InputSchema,
	}
}

func (t mcpTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	return permissions.Request{
		Action: permissions.ActionShell,
		Target: t.Definition().Name,
		Risk:   permissions.RiskExecute,
		Reason: "mcp tool " + t.Definition().Name,
	}, nil
}

func (t mcpTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	result, err := t.client.CallTool(ctx, t.spec.Name, args)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Content: result.Text(),
		Metadata: map[string]any{
			"server":   t.server,
			"tool":     t.spec.Name,
			"is_error": result.IsError,
		},
	}, nil
}
