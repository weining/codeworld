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

func NewIndexWorkspaceTool(ws workspace.Workspace) Tool {
	return indexWorkspaceTool{workspace: ws}
}

func (t indexWorkspaceTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "index_workspace",
		Description: "Refresh the workspace file index.",
		InputSchema: objectSchema(map[string]any{}, nil),
	}
}

func (t indexWorkspaceTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	if err := parseEmptyArgs(args); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{
		Action: permissions.ActionRead,
		Target: "workspace index",
		Risk:   permissions.RiskRead,
		Reason: "refresh workspace index",
	}, nil
}

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
