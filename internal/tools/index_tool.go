package tools

import (
	"context"
	"encoding/json"

	"codeworld/internal/context/indexer"
	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

type indexWorkspaceTool struct {
	workspace workspace.Workspace
}

// NewIndexWorkspaceTool 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewIndexWorkspaceTool(ws workspace.Workspace) Tool {
	return indexWorkspaceTool{workspace: ws}
}

// Definition 返回工具暴露给模型的名称、描述和参数 schema。
func (t indexWorkspaceTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "index_workspace",
		Description: "Refresh the workspace file index.",
		InputSchema: objectSchema(map[string]any{}, nil),
	}
}

// PermissionRequest 根据工具参数构造权限请求，供策略层在执行前判断。
func (t indexWorkspaceTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	if err := parseEmptyArgs(args); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{
		Action: permissions.ActionWrite,
		Target: "workspace index",
		Risk:   permissions.RiskWrite,
		Reason: "refresh workspace index",
	}, nil
}

// Execute 执行工具主体逻辑，并返回可序列化的工具结果。
func (t indexWorkspaceTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := parseEmptyArgs(args); err != nil {
		return Result{}, err
	}
	idx, err := indexer.Build(t.workspace.Root, 256*1024)
	if err != nil {
		return Result{}, err
	}
	path := indexer.DefaultPath(t.workspace.Root)
	if err := indexer.Save(path, idx); err != nil {
		return Result{}, err
	}
	skipped := 0
	for _, entry := range idx.Entries {
		if entry.Skipped {
			skipped++
		}
	}
	return Result{
		Content: indexer.Summary(idx, 80),
		Metadata: map[string]any{
			"path":    path,
			"files":   len(idx.Entries),
			"skipped": skipped,
		},
	}, nil
}
