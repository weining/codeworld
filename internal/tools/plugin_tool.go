package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/plugin"
	"codeworld/internal/workspace"
)

type pluginTool struct {
	workspace workspace.Workspace
	spec      plugin.Tool
}

// NewPluginTool 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewPluginTool(ws workspace.Workspace, spec plugin.Tool) Tool {
	return pluginTool{workspace: ws, spec: spec}
}

// Definition 返回工具暴露给模型的名称、描述和参数 schema。
func (t pluginTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        t.spec.Name,
		Description: t.spec.Description,
		InputSchema: t.spec.InputSchema,
	}
}

// PermissionRequest 根据工具参数构造权限请求，供策略层在执行前判断。
func (t pluginTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	command, err := t.renderCommand(args)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{
		Action: permissions.ActionShell,
		Target: command,
		Risk:   pluginRisk(t.spec.Risk),
		Reason: "plugin tool " + t.spec.Name,
	}, nil
}

// Execute 执行工具主体逻辑，并返回可序列化的工具结果。
func (t pluginTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	rendered, err := t.renderArgs(args)
	if err != nil {
		return Result{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, defaultShellTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, t.spec.Command, rendered...)
	cmd.Dir = t.workspace.Root
	configureCommandProcessGroup(cmd)
	stdout := &cappedBuffer{limit: defaultReadLimit}
	stderr := &cappedBuffer{limit: defaultReadLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	start := time.Now()
	err = cmd.Run()
	killCommandProcessGroup(cmd)
	content, truncated := combineCommandOutput(stdout, stderr, defaultReadLimit)
	return Result{
		Content: content,
		Metadata: map[string]any{
			"command":          quotedCommand(t.spec.Command, rendered),
			"exit_code":        exitCode(err),
			"duration_ms":      time.Since(start).Milliseconds(),
			"truncated":        truncated,
			"stdout_truncated": stdout.truncated(),
			"stderr_truncated": stderr.truncated(),
		},
	}, err
}

// renderCommand 封装局部逻辑，保持调用方流程清晰。
func (t pluginTool) renderCommand(args json.RawMessage) (string, error) {
	rendered, err := t.renderArgs(args)
	if err != nil {
		return "", err
	}
	return quotedCommand(t.spec.Command, rendered), nil
}

func quotedCommand(command string, args []string) string {
	parts := append([]string{command}, args...)
	for i := range parts {
		parts[i] = strconv.Quote(parts[i])
	}
	return strings.Join(parts, " ")
}

// renderArgs 封装局部逻辑，保持调用方流程清晰。
func (t pluginTool) renderArgs(args json.RawMessage) ([]string, error) {
	values := map[string]any{}
	if err := decodeArgs(args, &values); err != nil {
		return nil, err
	}
	rendered := make([]string, 0, len(t.spec.Args))
	for _, arg := range t.spec.Args {
		value, ok := renderTemplate(arg, values)
		if !ok {
			return nil, fmt.Errorf("missing template value for %q", arg)
		}
		rendered = append(rendered, value)
	}
	return rendered, nil
}

// renderTemplate 封装局部逻辑，保持调用方流程清晰。
func renderTemplate(template string, values map[string]any) (string, bool) {
	if !strings.HasPrefix(template, "{{") || !strings.HasSuffix(template, "}}") {
		return template, true
	}
	key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(template, "{{"), "}}"))
	value, ok := values[key]
	if !ok {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return typed, true
	case float64, bool:
		return fmt.Sprint(typed), true
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
}

// pluginRisk 封装局部逻辑，保持调用方流程清晰。
func pluginRisk(risk string) permissions.Risk {
	switch risk {
	case "read":
		return permissions.RiskRead
	case "write":
		return permissions.RiskWrite
	case "execute":
		return permissions.RiskExecute
	default:
		return permissions.RiskExecute
	}
}
