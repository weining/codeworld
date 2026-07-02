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
	contextStore := NewContextStore(ws, 256*1024)
	return NewRegistry([]Tool{
		NewListDirTool(ws),
		NewReadFileTool(ws),
		NewSearchTool(ws),
		NewWriteFileTool(ws),
		NewApplyPatchTool(ws),
		NewShellTool(ws),
		NewGitStatusTool(ws),
		NewGitDiffTool(ws),
		NewWebSearchTool(),
		NewIndexWorkspaceTool(ws),
		NewContextRefreshTool(contextStore),
		NewContextSearchTool(contextStore),
		NewContextOpenTool(contextStore),
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
	stdout := &cappedBuffer{limit: defaultReadLimit}
	stderr := &cappedBuffer{limit: defaultReadLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	content, truncated := combineCommandOutput(stdout, stderr, defaultReadLimit)

	metadata := map[string]any{
		"command":          command,
		"exit_code":        exitCode(err),
		"truncated":        truncated,
		"stdout_truncated": stdout.truncated(),
		"stderr_truncated": stderr.truncated(),
	}
	return Result{Content: content, Metadata: metadata}, err
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

func combineCommandOutput(stdout *cappedBuffer, stderr *cappedBuffer, limit int64) (string, bool) {
	truncated := stdout.truncated() || stderr.truncated()
	stdoutText := stdout.String()
	stderrText := stderr.String()
	if stderrText == "" {
		content, clipped := capString(stdoutText, limit)
		return content, truncated || clipped
	}
	if stdoutText == "" {
		content, clipped := capString(stderrText, limit)
		return content, truncated || clipped
	}

	separator := "\nstderr:\n"
	errBudget := limit - int64(len(separator))
	if errBudget < 0 {
		errBudget = 0
	}
	stderrPart, stderrClipped := capString(stderrText, errBudget)
	stdoutBudget := limit - int64(len(separator)) - int64(len(stderrPart))
	if stdoutBudget < 0 {
		stdoutBudget = 0
	}
	stdoutPart, stdoutClipped := capString(stdoutText, stdoutBudget)
	return stdoutPart + separator + stderrPart, truncated || stdoutClipped || stderrClipped
}

func capString(value string, limit int64) (string, bool) {
	if limit < 0 {
		limit = 0
	}
	if int64(len(value)) <= limit {
		return value, false
	}
	return value[:int(limit)], true
}
