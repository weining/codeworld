package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
	"codeworld/internal/workspace"
)

const (
	defaultShellTimeout = 60 * time.Second
	maxShellTimeout     = 60 * time.Second
)

type shellTool struct {
	workspace workspace.Workspace
	sandbox   sandbox.Policy
}

type shellArgs struct {
	Command         string `json:"command"`
	Cwd             string `json:"cwd"`
	TimeoutMS       int64  `json:"timeout_ms"`
	Reason          string `json:"reason"`
	RequestApproval bool   `json:"request_approval"`
}

// NewShellTool 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewShellTool(ws workspace.Workspace) Tool {
	return NewShellToolWithSandbox(ws, sandbox.FullAccess(ws.Root))
}

// NewShellToolWithSandbox 创建由 OS 沙箱约束的 shell 工具。
func NewShellToolWithSandbox(ws workspace.Workspace, policy sandbox.Policy) Tool {
	return shellTool{workspace: ws, sandbox: policy}
}

// Definition 返回工具暴露给模型的名称、描述和参数 schema。
func (t shellTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "shell",
		Description: "Run a shell command in the workspace.",
		InputSchema: objectSchema(map[string]any{
			"command":    map[string]any{"type": "string"},
			"cwd":        map[string]any{"type": "string"},
			"timeout_ms": map[string]any{"type": "integer", "minimum": 1},
			"reason":     map[string]any{"type": "string"},
			"request_approval": map[string]any{
				"type": "boolean", "description": "Request user confirmation before running this command in on-request mode.",
			},
		}, []string{"command"}),
	}
}

// PermissionRequest 根据工具参数构造权限请求，供策略层在执行前判断。
func (t shellTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseShellArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	if parsed.Command == "" {
		return permissions.Request{}, fmt.Errorf("command is required")
	}
	reason := parsed.Reason
	if reason == "" {
		reason = "shell"
	}
	return permissions.Request{
		Action:            permissions.ActionShell,
		Target:            parsed.Command,
		Risk:              classifyShellRisk(parsed.Command),
		Reason:            reason,
		ApprovalRequested: parsed.RequestApproval,
	}, nil
}

// Execute 执行工具主体逻辑，并返回可序列化的工具结果。
func (t shellTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	parsed, err := parseShellArgs(args)
	if err != nil {
		return Result{}, err
	}
	if parsed.Command == "" {
		return Result{}, fmt.Errorf("command is required")
	}

	cwd := parsed.Cwd
	if cwd == "" {
		cwd = "."
	}
	resolvedCwd, err := t.workspace.Resolve(cwd)
	if err != nil {
		return Result{}, err
	}

	timeoutMS := defaultShellTimeout.Milliseconds()
	if parsed.TimeoutMS > 0 {
		timeoutMS = parsed.TimeoutMS
	}
	if timeoutMS > maxShellTimeout.Milliseconds() {
		timeoutMS = maxShellTimeout.Milliseconds()
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd, err := t.sandbox.Command(resolvedCwd, "sh", "-c", parsed.Command)
	if err != nil {
		return Result{}, err
	}
	// 独立进程组便于超时或取消时清理子进程，避免 shell 派生的命令残留。
	configureCommandProcessGroup(cmd)

	stdout := &cappedBuffer{limit: defaultReadLimit}
	stderr := &cappedBuffer{limit: defaultReadLimit}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return Result{}, err
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		return Result{}, err
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if err := runCtx.Err(); err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
		return Result{}, err
	}
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	var readers sync.WaitGroup
	readCommandOutput(&readers, stdoutReader, stdout)
	readCommandOutput(&readers, stderrReader, stderr)

	err = waitForShellCommand(runCtx, cmd)
	killCommandProcessGroup(cmd)
	waitForCommandReaders(&readers, stdoutReader, stderrReader)

	durationMS := time.Since(start).Milliseconds()
	timedOut := runCtx.Err() == context.DeadlineExceeded
	content, truncated := combineCommandOutput(stdout, stderr, defaultReadLimit)

	rel, relErr := t.workspace.Rel(resolvedCwd)
	if relErr != nil {
		rel = cwd
	}
	result := Result{
		Content: content,
		Metadata: map[string]any{
			"command":          parsed.Command,
			"cwd":              rel,
			"exit_code":        exitCode(err),
			"duration_ms":      durationMS,
			"timeout_ms":       timeoutMS,
			"timed_out":        timedOut,
			"truncated":        truncated,
			"stdout_truncated": stdout.truncated(),
			"stderr_truncated": stderr.truncated(),
		},
	}
	return result, err
}

// readCommandOutput 读取外部输入，并保持调用方可处理的错误语义。
func readCommandOutput(wg *sync.WaitGroup, reader *os.File, buffer *cappedBuffer) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer reader.Close()
		_, _ = io.Copy(buffer, reader)
	}()
}

// waitForShellCommand 封装局部逻辑，保持调用方流程清晰。
func waitForShellCommand(ctx context.Context, cmd *exec.Cmd) error {
	type waitResult struct {
		state *os.ProcessState
		err   error
	}
	waitCh := make(chan waitResult, 1)
	go func() {
		state, err := cmd.Process.Wait()
		waitCh <- waitResult{state: state, err: err}
	}()

	var result waitResult
	select {
	case result = <-waitCh:
	case <-ctx.Done():
		killCommandProcessGroup(cmd)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitCh
		return ctx.Err()
	}
	if result.err != nil {
		return result.err
	}
	if result.state != nil && !result.state.Success() {
		return &exec.ExitError{ProcessState: result.state}
	}
	return nil
}

// waitForCommandReaders 封装局部逻辑，保持调用方流程清晰。
func waitForCommandReaders(wg *sync.WaitGroup, readers ...*os.File) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		for _, reader := range readers {
			_ = reader.Close()
		}
		<-done
	}
}

// parseShellArgs 解析输入数据，并执行必要的格式校验。
func parseShellArgs(args json.RawMessage) (shellArgs, error) {
	var parsed shellArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return shellArgs{}, err
	}
	return parsed, nil
}

// classifyShellRisk 封装局部逻辑，保持调用方流程清晰。
func classifyShellRisk(command string) permissions.Risk {
	risks := make([]permissions.Risk, 0, 2)
	if hasShellWriteRedirection(command) {
		risks = append(risks, permissions.RiskWrite)
	}
	for _, fields := range shellCommandSegments(command) {
		risks = append(risks, classifyShellCommandFields(fields))
	}
	if len(risks) == 0 {
		return permissions.RiskExecute
	}
	return highestShellRisk(risks)
}

// shellCommandSegments 封装局部逻辑，保持调用方流程清晰。
func shellCommandSegments(command string) [][]string {
	// 这里不是完整 shell parser，只做保守拆分：遇到管道、子 shell、逻辑操作符时按独立命令评估风险。
	normalized := normalizeShellCommandSeparators(command)

	var segments [][]string
	var current []string
	for _, field := range strings.Fields(normalized) {
		if field == ";" {
			if len(current) > 0 {
				segments = append(segments, current)
				current = nil
			}
			continue
		}
		current = append(current, field)
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}
	return segments
}

// normalizeShellCommandSeparators 规范化输入，减少等价写法对后续判断的影响。
func normalizeShellCommandSeparators(command string) string {
	var normalized strings.Builder
	for i := 0; i < len(command); i++ {
		if command[i] == '\\' && i+1 < len(command) {
			normalized.WriteByte(command[i])
			i++
			normalized.WriteByte(command[i])
			continue
		}
		if i+1 < len(command) {
			switch command[i : i+2] {
			case "$(", "&&", "||":
				normalized.WriteString(" ; ")
				i++
				continue
			}
		}
		switch command[i] {
		case ';', '|', '&', '(', ')', '`', '\n':
			normalized.WriteString(" ; ")
		default:
			normalized.WriteByte(command[i])
		}
	}
	return normalized.String()
}

// classifyShellCommandFields 封装局部逻辑，保持调用方流程清晰。
func classifyShellCommandFields(fields []string) permissions.Risk {
	fields = trimLeadingEnvAssignments(fields)
	if len(fields) == 0 {
		return permissions.RiskExecute
	}
	first := shellCommandName(fields[0])
	if risk, ok := classifyShellWrapper(first, fields); ok {
		return risk
	}

	if isDestructiveShellCommand(first, fields) {
		return permissions.RiskDestructive
	}
	if isNetworkShellCommand(first, fields) {
		return permissions.RiskNetwork
	}
	if isWriteShellCommand(first, fields) {
		return permissions.RiskWrite
	}
	if isReadShellCommand(first, fields) {
		return permissions.RiskRead
	}
	return permissions.RiskExecute
}

// trimLeadingEnvAssignments 封装局部逻辑，保持调用方流程清晰。
func trimLeadingEnvAssignments(fields []string) []string {
	for len(fields) > 0 && isShellAssignmentField(fields[0]) {
		fields = fields[1:]
	}
	return fields
}

// isShellAssignmentField 判断输入是否满足特定条件，并用于后续分支决策。
func isShellAssignmentField(field string) bool {
	equal := strings.IndexByte(field, '=')
	if equal <= 0 {
		return false
	}
	for i, r := range field[:equal] {
		if i == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// shellCommandName 封装局部逻辑，保持调用方流程清晰。
func shellCommandName(token string) string {
	token = strings.Trim(token, "'\"`")
	if strings.Contains(token, "/") {
		parts := strings.Split(token, "/")
		token = parts[len(parts)-1]
	}
	return strings.Trim(token, "'\"`")
}

// classifyShellWrapper 封装局部逻辑，保持调用方流程清晰。
func classifyShellWrapper(first string, fields []string) (permissions.Risk, bool) {
	switch first {
	case "sudo", "doas", "command", "time", "nohup":
		if next := firstSudoCommandField(fields, 1); next < len(fields) {
			return classifyShellCommandFields(fields[next:]), true
		}
	case "env":
		if next := firstEnvCommandField(fields, 1); next < len(fields) {
			return classifyShellCommandFields(fields[next:]), true
		}
	case "timeout", "gtimeout":
		if next := firstTimeoutCommandField(fields, 1); next < len(fields) {
			return classifyShellCommandFields(fields[next:]), true
		}
	case "sh", "bash", "zsh":
		for i := 1; i < len(fields)-1; i++ {
			if isShellCommandStringOption(fields[i]) {
				return classifyShellRisk(strings.Join(fields[i+1:], " ")), true
			}
		}
	case "xargs":
		if next := firstXargsCommandField(fields, 1); next < len(fields) {
			return classifyShellCommandFields(fields[next:]), true
		}
	}
	return permissions.RiskExecute, false
}

// firstSudoCommandField 从候选值中选择满足条件的结果。
func firstSudoCommandField(fields []string, start int) int {
	optionsWithOperand := map[string]bool{
		"-u": true, "--user": true,
		"-g": true, "--group": true,
		"-h": true, "--host": true,
		"-p": true, "--prompt": true,
		"-C": true, "--close-from": true,
	}
	for i := start; i < len(fields); i++ {
		field := fields[i]
		if field == "--" {
			return i + 1
		}
		if optionsWithOperand[field] && i+1 < len(fields) {
			i++
			continue
		}
		if strings.HasPrefix(field, "-") {
			continue
		}
		return i
	}
	return len(fields)
}

// firstEnvCommandField 从候选值中选择满足条件的结果。
func firstEnvCommandField(fields []string, start int) int {
	for i := start; i < len(fields); i++ {
		field := fields[i]
		if field == "--" {
			return i + 1
		}
		if strings.Contains(field, "=") {
			continue
		}
		if field == "-u" || field == "--unset" || field == "-C" || field == "--chdir" {
			if i+1 < len(fields) {
				i++
			}
			continue
		}
		if strings.HasPrefix(field, "-u") && len(field) > len("-u") {
			continue
		}
		if strings.HasPrefix(field, "--unset=") || strings.HasPrefix(field, "--chdir=") {
			continue
		}
		if strings.HasPrefix(field, "-") {
			continue
		}
		return i
	}
	return len(fields)
}

// firstTimeoutCommandField 从候选值中选择满足条件的结果。
func firstTimeoutCommandField(fields []string, start int) int {
	i := start
	for i < len(fields) {
		field := fields[i]
		if field == "--" {
			i++
			break
		}
		switch field {
		case "--foreground", "--preserve-status", "-v", "--verbose":
			i++
			continue
		case "-k", "--kill-after", "-s", "--signal":
			if i+1 >= len(fields) {
				return len(fields)
			}
			i += 2
			continue
		}
		if strings.HasPrefix(field, "--kill-after=") || strings.HasPrefix(field, "--signal=") {
			i++
			continue
		}
		if (strings.HasPrefix(field, "-k") || strings.HasPrefix(field, "-s")) && len(field) > 2 {
			i++
			continue
		}
		if strings.HasPrefix(field, "-") {
			i++
			continue
		}
		break
	}
	if i >= len(fields) {
		return len(fields)
	}
	return i + 1
}

// isShellCommandStringOption 判断输入是否满足特定条件，并用于后续分支决策。
func isShellCommandStringOption(field string) bool {
	return strings.HasPrefix(field, "-") && strings.Contains(field, "c")
}

// firstXargsCommandField 从候选值中选择满足条件的结果。
func firstXargsCommandField(fields []string, start int) int {
	optionsWithOperand := map[string]bool{
		"-a": true, "--arg-file": true,
		"-E": true, "-e": true, "--eof": true,
		"-I": true, "-i": true, "--replace": true,
		"-L": true, "-l": true, "--max-lines": true,
		"-n": true, "--max-args": true,
		"-P": true, "--max-procs": true,
		"-s": true, "--max-chars": true,
	}
	for i := start; i < len(fields); i++ {
		field := fields[i]
		if field == "--" {
			return i + 1
		}
		if optionsWithOperand[field] && i+1 < len(fields) {
			i++
			continue
		}
		if strings.HasPrefix(field, "--") && strings.Contains(field, "=") {
			continue
		}
		if strings.HasPrefix(field, "-") {
			continue
		}
		return i
	}
	return len(fields)
}

// highestShellRisk 封装局部逻辑，保持调用方流程清晰。
func highestShellRisk(risks []permissions.Risk) permissions.Risk {
	highest := risks[0]
	for _, risk := range risks[1:] {
		if shellRiskRank(risk) > shellRiskRank(highest) {
			highest = risk
		}
	}
	return highest
}

// shellRiskRank 封装局部逻辑，保持调用方流程清晰。
func shellRiskRank(risk permissions.Risk) int {
	switch risk {
	case permissions.RiskDestructive:
		return 5
	case permissions.RiskNetwork:
		return 4
	case permissions.RiskWrite:
		return 3
	case permissions.RiskExecute:
		return 2
	case permissions.RiskRead:
		return 1
	default:
		return 0
	}
}

// isDestructiveShellCommand 判断输入是否满足特定条件，并用于后续分支决策。
func isDestructiveShellCommand(first string, fields []string) bool {
	switch first {
	case "rm", "rmdir", "mv", "chmod", "chown", "dd", "mkfs":
		return true
	case "find":
		return hasField(fields, "-delete") || findExecsDestructiveCommand(fields)
	case "git":
		switch gitSubcommand(fields) {
		case "reset", "clean", "rm":
			return true
		case "branch":
			return hasShortOption(fields, "D") || hasShortOption(fields, "d") || hasLongOption(fields, "--delete")
		}
	}
	return false
}

// isNetworkShellCommand 判断输入是否满足特定条件，并用于后续分支决策。
func isNetworkShellCommand(first string, fields []string) bool {
	switch first {
	case "curl", "wget", "ssh", "scp", "rsync", "nc", "telnet":
		return true
	case "git":
		switch gitSubcommand(fields) {
		case "clone", "fetch", "pull", "push":
			return true
		case "submodule":
			return gitSubmoduleCommandIsNetwork(fields)
		}
	case "find":
		return findExecRisk(fields) == permissions.RiskNetwork
	case "go":
		return goCommandIsNetwork(fields)
	case "pip", "pip3":
		return len(fields) > 1 && isPackageInstallSubcommand(fields[1])
	case "python", "python3":
		return pythonRunsPipInstall(fields)
	case "brew", "apt", "apt-get", "dnf", "yum", "cargo":
		return len(fields) > 1 && isPackageInstallSubcommand(fields[1])
	case "npm", "pnpm", "yarn", "bun":
		if len(fields) > 1 {
			if isPackageInstallSubcommand(fields[1]) || fields[1] == "add" {
				return true
			}
		}
	}
	return false
}

// isWriteShellCommand 判断输入是否满足特定条件，并用于后续分支决策。
func isWriteShellCommand(first string, fields []string) bool {
	switch first {
	case "touch", "mkdir", "cp", "tee":
		return true
	case "go":
		return goCommandIsWrite(fields)
	case "sed":
		return hasShortOption(fields, "i") || hasLongOption(fields, "--in-place")
	case "find":
		return findExecRisk(fields) == permissions.RiskWrite
	case "tar":
		return tarExtracts(fields)
	case "git":
		switch gitSubcommand(fields) {
		case "add", "apply", "commit", "checkout", "restore", "switch", "merge", "rebase", "stash":
			return true
		}
	}
	return false
}

// gitSubmoduleCommandIsNetwork 封装局部逻辑，保持调用方流程清晰。
func gitSubmoduleCommandIsNetwork(fields []string) bool {
	submoduleSeen := false
	for _, field := range fields[1:] {
		if !submoduleSeen {
			if field == "submodule" {
				submoduleSeen = true
			}
			continue
		}
		switch field {
		case "update", "init", "sync", "foreach":
			return true
		}
	}
	return false
}

// isPackageInstallSubcommand 判断输入是否满足特定条件，并用于后续分支决策。
func isPackageInstallSubcommand(field string) bool {
	switch field {
	case "install", "update", "upgrade", "download", "sync", "add":
		return true
	default:
		return false
	}
}

// goCommandIsNetwork 封装局部逻辑，保持调用方流程清晰。
func goCommandIsNetwork(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	if fields[1] == "get" {
		return true
	}
	return fields[1] == "install" && commandContainsVersionSuffix(fields[2:])
}

// goCommandIsWrite 封装局部逻辑，保持调用方流程清晰。
func goCommandIsWrite(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	if fields[1] == "fmt" || fields[1] == "generate" {
		return true
	}
	return len(fields) > 2 && fields[1] == "mod" && fields[2] == "tidy"
}

// commandContainsVersionSuffix 封装局部逻辑，保持调用方流程清晰。
func commandContainsVersionSuffix(fields []string) bool {
	for _, field := range fields {
		if strings.Contains(field, "@") {
			return true
		}
	}
	return false
}

// pythonRunsPipInstall 封装局部逻辑，保持调用方流程清晰。
func pythonRunsPipInstall(fields []string) bool {
	for i := 1; i < len(fields)-2; i++ {
		if fields[i] == "-m" && fields[i+1] == "pip" {
			return isPackageInstallSubcommand(fields[i+2])
		}
	}
	return false
}

// tarExtracts 封装局部逻辑，保持调用方流程清晰。
func tarExtracts(fields []string) bool {
	for _, field := range fields[1:] {
		if field == "--extract" || field == "--get" {
			return true
		}
		if strings.HasPrefix(field, "-") && !strings.HasPrefix(field, "--") && strings.Contains(field[1:], "x") {
			return true
		}
		if !strings.HasPrefix(field, "-") && strings.Contains(field, "x") {
			return true
		}
	}
	return false
}

// isReadShellCommand 判断输入是否满足特定条件，并用于后续分支决策。
func isReadShellCommand(first string, fields []string) bool {
	switch first {
	case "ls", "pwd", "cat", "grep", "rg", "find", "sed", "awk", "head", "tail", "wc":
		return true
	case "git":
		switch gitSubcommand(fields) {
		case "status", "diff", "show", "log", "branch":
			return true
		}
	}
	return false
}

// gitSubcommand 封装局部逻辑，保持调用方流程清晰。
func gitSubcommand(fields []string) string {
	optionsWithOperand := map[string]bool{
		"-C": true, "-c": true, "--exec-path": true, "--git-dir": true, "--work-tree": true,
		"--namespace": true, "--config-env": true,
	}
	for i := 1; i < len(fields); i++ {
		field := fields[i]
		if field == "--" {
			continue
		}
		if optionsWithOperand[field] && i+1 < len(fields) {
			i++
			continue
		}
		if strings.HasPrefix(field, "--exec-path=") ||
			strings.HasPrefix(field, "--git-dir=") ||
			strings.HasPrefix(field, "--work-tree=") ||
			strings.HasPrefix(field, "--namespace=") ||
			strings.HasPrefix(field, "--config-env=") {
			continue
		}
		if strings.HasPrefix(field, "-") {
			continue
		}
		return field
	}
	return ""
}

// hasField 判断输入是否满足特定条件，并用于后续分支决策。
func hasField(fields []string, needle string) bool {
	for _, field := range fields[1:] {
		if field == needle {
			return true
		}
	}
	return false
}

// hasShortOption 判断输入是否满足特定条件，并用于后续分支决策。
func hasShortOption(fields []string, option string) bool {
	for _, field := range fields[1:] {
		if field == "-"+option || (strings.HasPrefix(field, "-") && !strings.HasPrefix(field, "--") && strings.Contains(field[1:], option)) {
			return true
		}
	}
	return false
}

// hasLongOption 判断输入是否满足特定条件，并用于后续分支决策。
func hasLongOption(fields []string, option string) bool {
	for _, field := range fields[1:] {
		if field == option || strings.HasPrefix(field, option+"=") {
			return true
		}
	}
	return false
}

// findExecsDestructiveCommand 封装局部逻辑，保持调用方流程清晰。
func findExecsDestructiveCommand(fields []string) bool {
	return findExecRisk(fields) == permissions.RiskDestructive
}

// findExecRisk 封装局部逻辑，保持调用方流程清晰。
func findExecRisk(fields []string) permissions.Risk {
	var risks []permissions.Risk
	for i := 1; i < len(fields)-1; i++ {
		if fields[i] != "-exec" && fields[i] != "-execdir" {
			continue
		}
		risks = append(risks, classifyShellCommandFields(fields[i+1:]))
	}
	if len(risks) == 0 {
		return permissions.RiskRead
	}
	return highestShellRisk(risks)
}

// hasShellWriteRedirection 判断输入是否满足特定条件，并用于后续分支决策。
func hasShellWriteRedirection(command string) bool {
	for _, token := range strings.Fields(command) {
		if isFDRedirect(token) {
			continue
		}
		if strings.HasPrefix(token, ">") || strings.Contains(token, ">") || strings.Contains(token, "2>") {
			return true
		}
	}
	return false
}

// isFDRedirect 判断输入是否满足特定条件，并用于后续分支决策。
func isFDRedirect(token string) bool {
	return token == "2>&1" || token == "1>&2" || token == ">&2" || token == "&>-" || strings.HasPrefix(token, "2>&") || strings.HasPrefix(token, "1>&")
}
