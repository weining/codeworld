package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
	"codeworld/internal/workspace"
)

type gitStatusTool struct {
	workspace workspace.Workspace
}

// NewGitStatusTool 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewGitStatusTool(ws workspace.Workspace) Tool {
	return gitStatusTool{workspace: ws}
}

// Definition 返回工具暴露给模型的名称、描述和参数 schema。
func (t gitStatusTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "git_status",
		Description: "Run git status --short in the workspace.",
		InputSchema: objectSchema(map[string]any{}, nil),
	}
}

// PermissionRequest 根据工具参数构造权限请求，供策略层在执行前判断。
func (t gitStatusTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	if err := parseEmptyArgs(args); err != nil {
		return permissions.Request{}, err
	}
	return readRequest("git_status", "."), nil
}

// Execute 执行工具主体逻辑，并返回可序列化的工具结果。
func (t gitStatusTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := parseEmptyArgs(args); err != nil {
		return Result{}, err
	}
	return runGitTool(ctx, t.workspace, "git status --short", "status", "--short")
}

type gitDiffTool struct {
	workspace workspace.Workspace
}

// NewGitDiffTool 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewGitDiffTool(ws workspace.Workspace) Tool {
	return gitDiffTool{workspace: ws}
}

// Definition 返回工具暴露给模型的名称、描述和参数 schema。
func (t gitDiffTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "git_diff",
		Description: "Run git diff in the workspace.",
		InputSchema: objectSchema(map[string]any{}, nil),
	}
}

// PermissionRequest 根据工具参数构造权限请求，供策略层在执行前判断。
func (t gitDiffTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	if err := parseEmptyArgs(args); err != nil {
		return permissions.Request{}, err
	}
	return readRequest("git_diff", "."), nil
}

// Execute 执行工具主体逻辑，并返回可序列化的工具结果。
func (t gitDiffTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := parseEmptyArgs(args); err != nil {
		return Result{}, err
	}
	return runGitTool(ctx, t.workspace, "git diff", "diff")
}

// NewDefaultRegistry 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewDefaultRegistry(ws workspace.Workspace) *Registry {
	return NewDefaultRegistryWithSandbox(ws, sandbox.FullAccess(ws.Root))
}

// NewDefaultRegistryWithSandbox 创建所有本地进程共享同一沙箱策略的工具表。
func NewDefaultRegistryWithSandbox(ws workspace.Workspace, policy sandbox.Policy) *Registry {
	contextStore := NewContextStore(ws, 256*1024)
	registered := []Tool{
		NewListDirTool(ws),
		NewReadFileTool(ws),
		NewSearchTool(ws),
		NewShellToolWithSandbox(ws, policy),
		NewGitStatusTool(ws),
		NewGitDiffTool(ws),
		NewContextRefreshTool(contextStore),
		NewContextSearchTool(contextStore),
		NewContextOpenTool(contextStore),
	}
	if policy.Network {
		registered = append(registered, NewWebSearchTool())
		if policy.Mode != sandbox.ModeReadOnly {
			registered = append(registered, NewBrowserOpenTool(ws, policy))
		}
	}
	if policy.Mode != sandbox.ModeReadOnly {
		registered = append(registered,
			NewWriteFileTool(ws),
			NewApplyPatchTool(ws),
			NewIndexWorkspaceTool(ws),
		)
	}
	return NewRegistry(registered, nil)
}

// parseEmptyArgs 解析输入数据，并执行必要的格式校验。
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

// runGitTool 执行主要流程，并把运行结果或错误返回给调用方。
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

// exitCode 封装局部逻辑，保持调用方流程清晰。
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

// Write 写入输出数据，并保证必要的目录或权限约束。
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

// String 提供对外可复用的能力，并隐藏内部实现细节。
func (b *cappedBuffer) String() string {
	return b.buf.String()
}

// truncated 封装局部逻辑，保持调用方流程清晰。
func (b *cappedBuffer) truncated() bool {
	return b.total > b.limit
}

// combineCommandOutput 封装局部逻辑，保持调用方流程清晰。
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

// capString 封装局部逻辑，保持调用方流程清晰。
func capString(value string, limit int64) (string, bool) {
	if limit < 0 {
		limit = 0
	}
	if int64(len(value)) <= limit {
		return value, false
	}
	return value[:int(limit)], true
}
