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
	"sync/atomic"
	"time"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
	"codeworld/internal/workspace"
	"github.com/creack/pty"
)

const (
	commandSessionOutputLimit = 1024 * 1024
	commandSessionWriteLimit  = 32 * 1024
	maxRunningCommandSessions = 8
)

type CommandSessionStatus string

const (
	CommandSessionRunning CommandSessionStatus = "running"
	CommandSessionExited  CommandSessionStatus = "exited"
	CommandSessionFailed  CommandSessionStatus = "failed"
)

type CommandSessionSnapshot struct {
	ID         string               `json:"id"`
	Command    string               `json:"command"`
	CWD        string               `json:"cwd"`
	Status     CommandSessionStatus `json:"status"`
	ExitCode   int                  `json:"exit_code,omitempty"`
	Error      string               `json:"error,omitempty"`
	StartedAt  time.Time            `json:"started_at"`
	FinishedAt *time.Time           `json:"finished_at,omitempty"`
	Output     string               `json:"output,omitempty"`
	Cursor     int64                `json:"cursor"`
	Truncated  bool                 `json:"truncated,omitempty"`
	PTY        bool                 `json:"pty,omitempty"`
	Rows       uint16               `json:"rows,omitempty"`
	Cols       uint16               `json:"cols,omitempty"`
}

type CommandSessionStartOptions struct {
	CWD  string
	PTY  bool
	Rows uint16
	Cols uint16
}

type commandSession struct {
	mu         sync.Mutex
	id         string
	command    string
	cwd        string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	pty        *os.File
	ioWG       sync.WaitGroup
	output     *commandSessionBuffer
	status     CommandSessionStatus
	exitCode   int
	err        string
	startedAt  time.Time
	finishedAt *time.Time
	rows       uint16
	cols       uint16
}

type CommandSessionManager struct {
	workspace workspace.Workspace
	sandbox   sandbox.Policy
	mu        sync.Mutex
	sessions  map[string]*commandSession
	wg        sync.WaitGroup
	closed    bool
	nextID    atomic.Uint64
}

// NewCommandSessionManager 创建与 Runtime 同生命周期的后台命令管理器。
func NewCommandSessionManager(ws workspace.Workspace) *CommandSessionManager {
	return NewCommandSessionManagerWithSandbox(ws, sandbox.FullAccess(ws.Root))
}

// NewCommandSessionManagerWithSandbox 创建受统一 OS 沙箱约束的后台命令管理器。
func NewCommandSessionManagerWithSandbox(ws workspace.Workspace, policy sandbox.Policy) *CommandSessionManager {
	return &CommandSessionManager{workspace: ws, sandbox: policy, sessions: map[string]*commandSession{}}
}

// Start 启动后台命令并立即返回会话快照，命令输出由 Poll 增量读取。
func (m *CommandSessionManager) Start(ctx context.Context, command, cwd string) (CommandSessionSnapshot, error) {
	return m.StartWithOptions(ctx, command, CommandSessionStartOptions{CWD: cwd})
}

// StartWithOptions 可以选择 PTY 和初始终端尺寸；PTY 不可用时直接返回错误。
func (m *CommandSessionManager) StartWithOptions(ctx context.Context, command string, opts CommandSessionStartOptions) (CommandSessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return CommandSessionSnapshot{}, err
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return CommandSessionSnapshot{}, fmt.Errorf("command is required")
	}
	resolvedCWD, err := m.workspace.Resolve(opts.CWD)
	if err != nil {
		return CommandSessionSnapshot{}, err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return CommandSessionSnapshot{}, fmt.Errorf("command session manager is closed")
	}
	if m.runningLocked() >= maxRunningCommandSessions {
		m.mu.Unlock()
		return CommandSessionSnapshot{}, fmt.Errorf("too many running command sessions")
	}
	id := fmt.Sprintf("cmd-%d-%d", time.Now().UTC().UnixMilli(), m.nextID.Add(1))
	m.mu.Unlock()

	cmd, err := m.sandbox.Command(resolvedCWD, "sh", "-c", command)
	if err != nil {
		return CommandSessionSnapshot{}, err
	}
	output := &commandSessionBuffer{limit: commandSessionOutputLimit}
	var stdin io.WriteCloser
	var ptyFile *os.File
	rows, cols := opts.Rows, opts.Cols
	if opts.PTY {
		if rows == 0 {
			rows = 24
		}
		if cols == 0 {
			cols = 80
		}
		if err := validateTerminalSize(rows, cols); err != nil {
			return CommandSessionSnapshot{}, err
		}
		ptyFile, err = pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
		if err != nil {
			return CommandSessionSnapshot{}, fmt.Errorf("start PTY: %w", err)
		}
		stdin = ptyFile
	} else {
		configureCommandProcessGroup(cmd)
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return CommandSessionSnapshot{}, err
		}
		cmd.Stdout = output
		cmd.Stderr = output
		if err := cmd.Start(); err != nil {
			_ = stdin.Close()
			return CommandSessionSnapshot{}, err
		}
	}
	now := time.Now().UTC()
	sess := &commandSession{
		id: id, command: command, cwd: resolvedCWD, cmd: cmd, stdin: stdin,
		output: output, status: CommandSessionRunning, exitCode: -1, startedAt: now,
		pty: ptyFile, rows: rows, cols: cols,
	}
	if ptyFile != nil {
		sess.ioWG.Add(1)
		go func() {
			defer sess.ioWG.Done()
			_, _ = io.Copy(output, ptyFile)
		}()
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		killCommandProcessGroup(cmd)
		_ = stdin.Close()
		_ = cmd.Wait()
		sess.ioWG.Wait()
		return CommandSessionSnapshot{}, fmt.Errorf("command session manager is closed")
	}
	m.sessions[id] = sess
	m.wg.Add(1)
	m.mu.Unlock()
	go m.wait(sess)
	return sess.snapshot(0), nil
}

// Resize 修改 PTY 会话的终端行列数。
func (m *CommandSessionManager) Resize(id string, rows, cols uint16) error {
	if err := validateTerminalSize(rows, cols); err != nil {
		return err
	}
	sess, err := m.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.status != CommandSessionRunning {
		return fmt.Errorf("command session %s is not running", id)
	}
	if sess.pty == nil {
		return fmt.Errorf("command session %s does not use a PTY", id)
	}
	if err := pty.Setsize(sess.pty, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		return fmt.Errorf("resize PTY: %w", err)
	}
	sess.rows, sess.cols = rows, cols
	return nil
}

// Poll 返回指定 cursor 之后的新输出和最新进程状态。
func (m *CommandSessionManager) Poll(id string, cursor int64) (CommandSessionSnapshot, error) {
	sess, err := m.get(id)
	if err != nil {
		return CommandSessionSnapshot{}, err
	}
	return sess.snapshot(cursor), nil
}

// Write 向仍在运行的命令 stdin 写入原始文本。
func (m *CommandSessionManager) Write(id, data string) (int, error) {
	if len(data) > commandSessionWriteLimit {
		return 0, fmt.Errorf("command stdin write exceeds %d bytes", commandSessionWriteLimit)
	}
	sess, err := m.get(id)
	if err != nil {
		return 0, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.status != CommandSessionRunning {
		return 0, fmt.Errorf("command session %s is not running", id)
	}
	n, err := io.WriteString(sess.stdin, data)
	if err != nil {
		return n, fmt.Errorf("write command stdin: %w", err)
	}
	return n, nil
}

// Terminate 终止后台命令及其子进程组。
func (m *CommandSessionManager) Terminate(id string) error {
	sess, err := m.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.status != CommandSessionRunning {
		return nil
	}
	killCommandProcessGroup(sess.cmd)
	return nil
}

// Close 终止所有仍在运行的命令，并等待回收进程资源。
func (m *CommandSessionManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.wg.Wait()
		return nil
	}
	m.closed = true
	sessions := make([]*commandSession, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, sess)
	}
	m.mu.Unlock()
	for _, sess := range sessions {
		sess.mu.Lock()
		if sess.status == CommandSessionRunning {
			killCommandProcessGroup(sess.cmd)
		}
		sess.mu.Unlock()
	}
	m.wg.Wait()
	return nil
}

func (m *CommandSessionManager) get(id string) (*commandSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("unknown command session %q", id)
	}
	return sess, nil
}

func (m *CommandSessionManager) runningLocked() int {
	running := 0
	for _, sess := range m.sessions {
		sess.mu.Lock()
		if sess.status == CommandSessionRunning {
			running++
		}
		sess.mu.Unlock()
	}
	return running
}

func (m *CommandSessionManager) wait(sess *commandSession) {
	defer m.wg.Done()
	err := sess.cmd.Wait()
	finished := time.Now().UTC()
	sess.mu.Lock()
	_ = sess.stdin.Close()
	sess.mu.Unlock()
	sess.ioWG.Wait()
	sess.mu.Lock()
	sess.finishedAt = &finished
	sess.exitCode = exitCode(err)
	if err != nil {
		sess.status = CommandSessionFailed
		sess.err = err.Error()
	} else {
		sess.status = CommandSessionExited
	}
	sess.mu.Unlock()
}

func (s *commandSession) snapshot(cursor int64) CommandSessionSnapshot {
	output, nextCursor, truncated := s.output.readFrom(cursor)
	s.mu.Lock()
	defer s.mu.Unlock()
	return CommandSessionSnapshot{
		ID: s.id, Command: s.command, CWD: s.cwd, Status: s.status,
		ExitCode: s.exitCode, Error: s.err, StartedAt: s.startedAt, FinishedAt: s.finishedAt,
		Output: output, Cursor: nextCursor, Truncated: truncated,
		PTY: s.pty != nil, Rows: s.rows, Cols: s.cols,
	}
}

func validateTerminalSize(rows, cols uint16) error {
	if rows == 0 || cols == 0 || rows > 1000 || cols > 1000 {
		return fmt.Errorf("terminal size must be between 1 and 1000 rows/cols")
	}
	return nil
}

type commandSessionBuffer struct {
	mu    sync.Mutex
	data  []byte
	base  int64
	limit int
}

func (b *commandSessionBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	b.data = append(b.data, p...)
	if overflow := len(b.data) - b.limit; overflow > 0 {
		b.data = append([]byte(nil), b.data[overflow:]...)
		b.base += int64(overflow)
	}
	return written, nil
}

func (b *commandSessionBuffer) readFrom(cursor int64) (string, int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	truncated := cursor < b.base
	if cursor < b.base {
		cursor = b.base
	}
	end := b.base + int64(len(b.data))
	if cursor > end {
		cursor = end
	}
	return string(b.data[cursor-b.base:]), end, truncated
}

type commandSessionStartTool struct{ manager *CommandSessionManager }
type commandSessionPollTool struct{ manager *CommandSessionManager }
type commandSessionWriteTool struct{ manager *CommandSessionManager }
type commandSessionTerminateTool struct{ manager *CommandSessionManager }
type commandSessionResizeTool struct{ manager *CommandSessionManager }

type commandSessionStartArgs struct {
	Command         string `json:"command"`
	CWD             string `json:"cwd"`
	PTY             bool   `json:"pty"`
	Rows            uint16 `json:"rows"`
	Cols            uint16 `json:"cols"`
	RequestApproval bool   `json:"request_approval"`
}

// NewCommandSessionTools 返回共享同一后台进程管理器的命令会话工具。
func NewCommandSessionTools(manager *CommandSessionManager) []Tool {
	return []Tool{
		commandSessionStartTool{manager}, commandSessionPollTool{manager},
		commandSessionWriteTool{manager}, commandSessionResizeTool{manager}, commandSessionTerminateTool{manager},
	}
}

func (t commandSessionStartTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "shell_start", Description: "Start a long-running shell command and return a session ID without waiting for completion.", InputSchema: objectSchema(map[string]any{
		"command": map[string]any{"type": "string"}, "cwd": map[string]any{"type": "string"},
		"pty": map[string]any{"type": "boolean"}, "rows": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
		"cols":             map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
		"request_approval": map[string]any{"type": "boolean", "description": "Request user confirmation before starting this command in on-request mode."},
	}, []string{"command"})}
}

func (t commandSessionStartTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var parsed commandSessionStartArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return permissions.Request{}, err
	}
	if strings.TrimSpace(parsed.Command) == "" {
		return permissions.Request{}, fmt.Errorf("command is required")
	}
	return permissions.Request{Action: permissions.ActionShell, Target: parsed.Command, Risk: classifyShellRisk(parsed.Command), Reason: "start background shell command", ApprovalRequested: parsed.RequestApproval}, nil
}

func (t commandSessionStartTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	var parsed commandSessionStartArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return Result{}, err
	}
	snapshot, err := t.manager.StartWithOptions(ctx, parsed.Command, CommandSessionStartOptions{CWD: parsed.CWD, PTY: parsed.PTY, Rows: parsed.Rows, Cols: parsed.Cols})
	return commandSessionResult(snapshot), err
}

func (t commandSessionPollTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "shell_poll", Description: "Read new output and status from a background shell session.", InputSchema: objectSchema(map[string]any{
		"session_id": map[string]any{"type": "string"}, "cursor": map[string]any{"type": "integer", "minimum": 0},
	}, []string{"session_id"})}
}

func (t commandSessionPollTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var parsed struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeArgs(args, &parsed); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionRead, Target: parsed.SessionID, Risk: permissions.RiskRead, Reason: "read background command output"}, nil
}

func (t commandSessionPollTool) Execute(_ context.Context, args json.RawMessage) (Result, error) {
	var parsed struct {
		SessionID string `json:"session_id"`
		Cursor    int64  `json:"cursor"`
	}
	if err := decodeArgs(args, &parsed); err != nil {
		return Result{}, err
	}
	if parsed.Cursor < 0 {
		return Result{}, fmt.Errorf("cursor must be non-negative")
	}
	snapshot, err := t.manager.Poll(parsed.SessionID, parsed.Cursor)
	return commandSessionResult(snapshot), err
}

func (t commandSessionWriteTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "shell_write", Description: "Write raw text to a running background shell session stdin.", InputSchema: objectSchema(map[string]any{
		"session_id": map[string]any{"type": "string"}, "data": map[string]any{"type": "string"},
	}, []string{"session_id", "data"})}
}

func (t commandSessionWriteTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var parsed struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeArgs(args, &parsed); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionWrite, Target: parsed.SessionID, Risk: permissions.RiskWrite, Reason: "write background command stdin"}, nil
}

func (t commandSessionWriteTool) Execute(_ context.Context, args json.RawMessage) (Result, error) {
	var parsed struct {
		SessionID string `json:"session_id"`
		Data      string `json:"data"`
	}
	if err := decodeArgs(args, &parsed); err != nil {
		return Result{}, err
	}
	n, err := t.manager.Write(parsed.SessionID, parsed.Data)
	return Result{Content: fmt.Sprintf("wrote %d bytes", n), Metadata: map[string]any{"session_id": parsed.SessionID, "bytes": n}}, err
}

func (t commandSessionResizeTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "shell_resize", Description: "Resize a running PTY shell session.", InputSchema: objectSchema(map[string]any{
		"session_id": map[string]any{"type": "string"}, "rows": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
		"cols": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
	}, []string{"session_id", "rows", "cols"})}
}

func (t commandSessionResizeTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var parsed struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeArgs(args, &parsed); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionWrite, Target: parsed.SessionID, Risk: permissions.RiskWrite, Reason: "resize background PTY"}, nil
}

func (t commandSessionResizeTool) Execute(_ context.Context, args json.RawMessage) (Result, error) {
	var parsed struct {
		SessionID string `json:"session_id"`
		Rows      uint16 `json:"rows"`
		Cols      uint16 `json:"cols"`
	}
	if err := decodeArgs(args, &parsed); err != nil {
		return Result{}, err
	}
	err := t.manager.Resize(parsed.SessionID, parsed.Rows, parsed.Cols)
	return Result{Content: "PTY resized", Metadata: map[string]any{"session_id": parsed.SessionID, "rows": parsed.Rows, "cols": parsed.Cols}}, err
}

func (t commandSessionTerminateTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "shell_terminate", Description: "Terminate a running background shell session and its child processes.", InputSchema: objectSchema(map[string]any{
		"session_id": map[string]any{"type": "string"},
	}, []string{"session_id"})}
}

func (t commandSessionTerminateTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	var parsed struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeArgs(args, &parsed); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionWrite, Target: parsed.SessionID, Risk: permissions.RiskWrite, Reason: "terminate background shell command"}, nil
}

func (t commandSessionTerminateTool) Execute(_ context.Context, args json.RawMessage) (Result, error) {
	var parsed struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeArgs(args, &parsed); err != nil {
		return Result{}, err
	}
	err := t.manager.Terminate(parsed.SessionID)
	return Result{Content: "termination requested", Metadata: map[string]any{"session_id": parsed.SessionID}}, err
}

func commandSessionResult(snapshot CommandSessionSnapshot) Result {
	return Result{Content: snapshot.Output, Metadata: map[string]any{
		"session_id": snapshot.ID, "command": snapshot.Command, "cwd": snapshot.CWD,
		"status": snapshot.Status, "exit_code": snapshot.ExitCode, "error": snapshot.Error,
		"cursor": snapshot.Cursor, "truncated": snapshot.Truncated,
		"pty": snapshot.PTY, "rows": snapshot.Rows, "cols": snapshot.Cols,
	}}
}
