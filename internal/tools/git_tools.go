package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

type gitStatusTool struct {
	workspace workspace.Workspace
}

func NewGitStatusTool(ws workspace.Workspace) Tool {
	return gitStatusTool{workspace: ws}
}

func (t gitStatusTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "git_status",
		Description: "Run git status --short in the workspace.",
		InputSchema: objectSchema(map[string]any{}, nil),
	}
}

func (t gitStatusTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	if err := parseEmptyArgs(args); err != nil {
		return permissions.Request{}, err
	}
	return readRequest("git_status", "."), nil
}

func (t gitStatusTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := parseEmptyArgs(args); err != nil {
		return Result{}, err
	}
	return runGitTool(ctx, t.workspace, "git status --short", "status", "--short")
}

type gitDiffTool struct {
	workspace workspace.Workspace
}

func NewGitDiffTool(ws workspace.Workspace) Tool {
	return gitDiffTool{workspace: ws}
}

func (t gitDiffTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "git_diff",
		Description: "Run git diff in the workspace.",
		InputSchema: objectSchema(map[string]any{}, nil),
	}
}

func (t gitDiffTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	if err := parseEmptyArgs(args); err != nil {
		return permissions.Request{}, err
	}
	return readRequest("git_diff", "."), nil
}

func (t gitDiffTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := parseEmptyArgs(args); err != nil {
		return Result{}, err
	}
	return runGitTool(ctx, t.workspace, "git diff", "diff")
}

func NewDefaultRegistry(ws workspace.Workspace) *Registry {
	return NewRegistry([]Tool{
		NewListDirTool(ws),
		NewReadFileTool(ws),
		NewSearchTool(ws),
		NewGitStatusTool(ws),
		NewGitDiffTool(ws),
	}, nil)
}

func parseEmptyArgs(args json.RawMessage) error {
	var parsed map[string]json.RawMessage
	if err := decodeArgs(args, &parsed); err != nil {
		return err
	}
	if len(parsed) > 0 {
		return fmt.Errorf("invalid tool arguments: expected empty object")
	}
	return nil
}

func runGitTool(ctx context.Context, ws workspace.Workspace, command string, args ...string) (Result, error) {
	cmdArgs := append([]string{"-C", ws.Root}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out := &cappedBuffer{limit: defaultReadLimit}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()

	metadata := map[string]any{
		"command":   command,
		"exit_code": exitCode(err),
		"truncated": out.truncated(),
	}
	return Result{Content: out.String(), Metadata: metadata}, err
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

type cappedBuffer struct {
	buf   bytes.Buffer
	limit int64
	total int64
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	b.total += int64(written)
	remaining := b.limit - int64(b.buf.Len())
	if remaining > 0 {
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
		_, _ = b.buf.Write(p)
	}
	return written, nil
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}

func (b *cappedBuffer) truncated() bool {
	return b.total > b.limit
}
