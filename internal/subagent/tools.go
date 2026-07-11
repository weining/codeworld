package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/tools"
)

type startArgs struct {
	Prompt string `json:"prompt"`
	Role   string `json:"role"`
}

type statusArgs struct {
	ID string `json:"id"`
}

type startTool struct {
	manager *Manager
}

type statusTool struct {
	manager *Manager
}

type messageArgs struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type taskArgs struct {
	ID string `json:"id"`
}

type controlTool struct {
	manager *Manager
	name    string
}

// NewStartTool 返回用于派发本地子代理任务的模型工具。
func NewStartTool(manager *Manager) tools.Tool {
	return startTool{manager: manager}
}

// NewStatusTool 返回用于查询本地子代理任务的模型工具。
func NewStatusTool(manager *Manager) tools.Tool {
	return statusTool{manager: manager}
}

// NewControlTools 返回消息追加、中断和终止工具。
func NewControlTools(manager *Manager) []tools.Tool {
	return []tools.Tool{
		controlTool{manager: manager, name: "subagent_send"},
		controlTool{manager: manager, name: "subagent_interrupt"},
		controlTool{manager: manager, name: "subagent_terminate"},
	}
}

// Definition 返回工具暴露给模型的名称、描述和参数 schema。
func (t startTool) Definition() model.ToolDefinition {
	roleSchema := map[string]any{"type": "string", "description": "Optional configured subagent role name."}
	roles := t.manager.Roles()
	if len(roles) > 0 {
		names := make([]string, 0, len(roles))
		for _, role := range roles {
			names = append(names, role.Name)
		}
		roleSchema["enum"] = names
	}
	return model.ToolDefinition{
		Name:        "subagent_start",
		Description: "Start a local read-only subagent task for independent codebase investigation.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "The investigation task for the subagent."},
				"role":   roleSchema,
			},
			"required":             []string{"prompt"},
			"additionalProperties": false,
		},
	}
}

// PermissionRequest 根据工具参数构造权限请求，供策略层在执行前判断。
func (t startTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseStartArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{
		Action: permissions.ActionRead,
		Target: parsed.Prompt,
		Risk:   permissions.RiskExecute,
		Reason: "start a local read-only subagent",
	}, nil
}

// Execute 启动后台子代理任务，并立即返回任务 ID。
func (t startTool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	if err := ctx.Err(); err != nil {
		return tools.Result{}, err
	}
	parsed, err := parseStartArgs(args)
	if err != nil {
		return tools.Result{}, err
	}
	task, err := t.manager.StartWithOptions(parsed.Prompt, StartOptions{Role: parsed.Role})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Content: fmt.Sprintf("subagent %s started", task.ID),
		Metadata: map[string]any{
			"id":     task.ID,
			"status": string(task.Status),
		},
	}, nil
}

// Definition 返回子代理控制工具的 schema。
func (t controlTool) Definition() model.ToolDefinition {
	definitions := map[string]model.ToolDefinition{
		"subagent_send": {
			Name: "subagent_send", Description: "Append an instruction and restart or resume a local subagent task.",
			InputSchema: objectSchema(map[string]any{
				"id":      map[string]any{"type": "string"},
				"message": map[string]any{"type": "string"},
			}, []string{"id", "message"}),
		},
		"subagent_interrupt": {
			Name: "subagent_interrupt", Description: "Interrupt a running local subagent task so it can be resumed later.",
			InputSchema: objectSchema(map[string]any{"id": map[string]any{"type": "string"}}, []string{"id"}),
		},
		"subagent_terminate": {
			Name: "subagent_terminate", Description: "Permanently terminate a running local subagent task.",
			InputSchema: objectSchema(map[string]any{"id": map[string]any{"type": "string"}}, []string{"id"}),
		},
	}
	return definitions[t.name]
}

// PermissionRequest 为子代理生命周期操作声明执行风险。
func (t controlTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	id, err := controlTarget(t.name, args)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionRead, Target: id, Risk: permissions.RiskExecute, Reason: t.name}, nil
}

// Execute 执行对应的子代理生命周期操作。
func (t controlTool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	if err := ctx.Err(); err != nil {
		return tools.Result{}, err
	}
	var task Task
	var err error
	switch t.name {
	case "subagent_send":
		parsed, parseErr := parseMessageArgs(args)
		if parseErr != nil {
			return tools.Result{}, parseErr
		}
		task, err = t.manager.Send(parsed.ID, parsed.Message)
	case "subagent_interrupt":
		parsed, parseErr := parseTaskArgs(args)
		if parseErr != nil {
			return tools.Result{}, parseErr
		}
		task, err = t.manager.Interrupt(parsed.ID)
	case "subagent_terminate":
		parsed, parseErr := parseTaskArgs(args)
		if parseErr != nil {
			return tools.Result{}, parseErr
		}
		task, err = t.manager.Terminate(parsed.ID)
	default:
		return tools.Result{}, fmt.Errorf("unknown subagent control %q", t.name)
	}
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Content: fmt.Sprintf("subagent %s %s", task.ID, task.Status), Metadata: map[string]any{"id": task.ID, "status": string(task.Status)}}, nil
}

// Definition 返回工具暴露给模型的名称、描述和参数 schema。
func (t statusTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "subagent_status",
		Description: "List local subagent tasks or inspect one task result by id.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Optional subagent task id."},
			},
			"additionalProperties": false,
		},
	}
}

// PermissionRequest 根据工具参数构造权限请求，供策略层在执行前判断。
func (t statusTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseStatusArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	target := "subagents"
	if parsed.ID != "" {
		target = parsed.ID
	}
	return permissions.Request{Action: permissions.ActionRead, Target: target, Risk: permissions.RiskRead, Reason: "read local subagent status"}, nil
}

// Execute 返回任务列表或单个任务详情。
func (t statusTool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	if err := ctx.Err(); err != nil {
		return tools.Result{}, err
	}
	parsed, err := parseStatusArgs(args)
	if err != nil {
		return tools.Result{}, err
	}
	if parsed.ID != "" {
		task, ok := t.manager.Get(parsed.ID)
		if !ok {
			return tools.Result{}, fmt.Errorf("unknown subagent task %q", parsed.ID)
		}
		return tools.Result{Content: FormatTask(task), Metadata: map[string]any{"id": task.ID, "status": string(task.Status)}}, nil
	}
	tasks := t.manager.List(10)
	lines := make([]string, 0, len(tasks))
	for _, task := range tasks {
		lines = append(lines, FormatTaskLine(task))
	}
	if len(lines) == 0 {
		return tools.Result{Content: "no subagent tasks"}, nil
	}
	return tools.Result{Content: strings.Join(lines, "\n"), Metadata: map[string]any{"tasks": len(tasks)}}, nil
}

// FormatTaskLine 输出适合 TUI/REPL 摘要列表展示的一行任务状态。
func FormatTaskLine(task Task) string {
	role := ""
	if task.Role != "" {
		role = " role=" + task.Role
	}
	return fmt.Sprintf("%s %s%s updated=%s prompt=%s", task.ID, task.Status, role, task.UpdatedAt.Local().Format("2006-01-02 15:04:05"), truncate(task.Prompt, 80))
}

// FormatTask 输出单个任务详情，包含完成结果或错误。
func FormatTask(task Task) string {
	lines := []string{
		"id: " + task.ID,
		"status: " + string(task.Status),
		"prompt: " + task.Prompt,
	}
	if task.Role != "" {
		lines = append(lines, "role: "+task.Role)
	}
	if len(task.Messages) > 0 {
		lines = append(lines, "follow-ups:\n- "+strings.Join(task.Messages, "\n- "))
	}
	if task.Result != "" {
		lines = append(lines, "result:\n"+task.Result)
	}
	if task.Error != "" {
		lines = append(lines, "error: "+task.Error)
	}
	return strings.Join(lines, "\n")
}

// parseStartArgs 解析启动参数并校验 prompt。
func parseStartArgs(args json.RawMessage) (startArgs, error) {
	var parsed startArgs
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return startArgs{}, err
	}
	parsed.Prompt = strings.TrimSpace(parsed.Prompt)
	parsed.Role = strings.TrimSpace(parsed.Role)
	if parsed.Prompt == "" {
		return startArgs{}, fmt.Errorf("prompt is required")
	}
	return parsed, nil
}

func parseMessageArgs(args json.RawMessage) (messageArgs, error) {
	var parsed messageArgs
	if err := json.Unmarshal(args, &parsed); err != nil {
		return messageArgs{}, err
	}
	parsed.ID = strings.TrimSpace(parsed.ID)
	parsed.Message = strings.TrimSpace(parsed.Message)
	if parsed.ID == "" || parsed.Message == "" {
		return messageArgs{}, fmt.Errorf("id and message are required")
	}
	return parsed, nil
}

func parseTaskArgs(args json.RawMessage) (taskArgs, error) {
	var parsed taskArgs
	if err := json.Unmarshal(args, &parsed); err != nil {
		return taskArgs{}, err
	}
	parsed.ID = strings.TrimSpace(parsed.ID)
	if parsed.ID == "" {
		return taskArgs{}, fmt.Errorf("id is required")
	}
	return parsed, nil
}

func controlTarget(name string, args json.RawMessage) (string, error) {
	if name == "subagent_send" {
		parsed, err := parseMessageArgs(args)
		return parsed.ID, err
	}
	parsed, err := parseTaskArgs(args)
	return parsed.ID, err
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

// parseStatusArgs 解析查询参数，空对象表示列出任务。
func parseStatusArgs(args json.RawMessage) (statusArgs, error) {
	var parsed statusArgs
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return statusArgs{}, err
	}
	parsed.ID = strings.TrimSpace(parsed.ID)
	return parsed, nil
}

// truncate 控制单行展示长度，避免长 prompt 挤坏终端布局。
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}
