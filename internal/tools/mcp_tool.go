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

// NewMCPTool 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewMCPTool(server string, spec mcp.Tool, client MCPCaller) Tool {
	return mcpTool{server: server, spec: spec, client: client}
}

// Definition 返回工具暴露给模型的名称、描述和参数 schema。
func (t mcpTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "mcp." + t.server + "." + t.spec.Name,
		Description: t.spec.Description,
		InputSchema: t.spec.InputSchema,
	}
}

// PermissionRequest 根据工具参数构造权限请求，供策略层在执行前判断。
func (t mcpTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	return permissions.Request{
		Action: permissions.ActionShell,
		Target: t.Definition().Name,
		Risk:   permissions.RiskExecute,
		Reason: "mcp tool " + t.Definition().Name,
	}, nil
}

// Execute 执行工具主体逻辑，并返回可序列化的工具结果。
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
