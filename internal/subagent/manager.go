package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusRunning     Status = "running"
	StatusDone        Status = "done"
	StatusError       Status = "error"
	StatusInterrupted Status = "interrupted"
	StatusTerminated  Status = "terminated"
)

type Task struct {
	ID         string              `json:"id"`
	Prompt     string              `json:"prompt"`
	Role       string              `json:"role,omitempty"`
	Messages   []string            `json:"messages,omitempty"`
	Status     Status              `json:"status"`
	Result     string              `json:"result,omitempty"`
	Error      string              `json:"error,omitempty"`
	Transcript []TranscriptMessage `json:"transcript,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

type RunResult struct {
	Content    string
	Transcript []TranscriptMessage
}

type TranscriptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
}

type RunnerFunc func(ctx context.Context, prompt string) (RunResult, error)

type Role struct {
	Name         string
	Description  string
	Instructions string
}

type Options struct {
	MaxConcurrent int
	Roles         []Role
}

type StartOptions struct {
	Role string
}

type taskControl struct {
	cancel     context.CancelFunc
	generation uint64
}

type Manager struct {
	root      string
	dir       string
	run       RunnerFunc
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	tasks     map[string]Task
	controls  map[string]taskControl
	roles     map[string]Role
	maxRuns   int
	wg        sync.WaitGroup
	closeOnce sync.Once
	closing   bool
}

// NewManager 创建本地子代理管理器，并恢复已持久化的历史任务。
func NewManager(root string, run RunnerFunc) (*Manager, error) {
	return NewManagerWithContext(context.Background(), root, run)
}

// NewManagerWithContext binds all background tasks to the runtime lifecycle.
func NewManagerWithContext(ctx context.Context, root string, run RunnerFunc) (*Manager, error) {
	return NewManagerWithOptions(ctx, root, run, Options{})
}

// NewManagerWithOptions 创建带并发上限和角色定义的本地子代理管理器。
func NewManagerWithOptions(ctx context.Context, root string, run RunnerFunc, opts Options) (*Manager, error) {
	runCtx, cancel := context.WithCancel(ctx)
	maxRuns := opts.MaxConcurrent
	if maxRuns <= 0 {
		maxRuns = 4
	}
	manager := &Manager{
		root:     root,
		dir:      filepath.Join(root, ".codeworld", "subagents"),
		run:      run,
		ctx:      runCtx,
		cancel:   cancel,
		tasks:    map[string]Task{},
		controls: map[string]taskControl{},
		roles:    map[string]Role{},
		maxRuns:  maxRuns,
	}
	for _, role := range opts.Roles {
		role.Name = strings.TrimSpace(role.Name)
		if role.Name != "" {
			manager.roles[role.Name] = role
		}
	}
	if err := manager.load(); err != nil {
		cancel()
		return nil, err
	}
	return manager, nil
}

// Start 创建后台子任务，任务结果会写入 .codeworld/subagents。
func (m *Manager) Start(prompt string) (Task, error) {
	return m.StartWithOptions(prompt, StartOptions{})
}

// StartWithOptions 使用可选角色创建后台子任务。
func (m *Manager) StartWithOptions(prompt string, opts StartOptions) (Task, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Task{}, fmt.Errorf("subagent prompt is empty")
	}
	if m.run == nil {
		return Task{}, fmt.Errorf("subagent runner is not configured")
	}
	roleName := strings.TrimSpace(opts.Role)
	if roleName != "" {
		if _, ok := m.roles[roleName]; !ok {
			return Task{}, fmt.Errorf("unknown subagent role %q", roleName)
		}
	}
	now := time.Now().UTC()
	task := Task{
		ID:        newTaskID(now),
		Prompt:    prompt,
		Role:      roleName,
		Status:    StatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return Task{}, context.Canceled
	}
	if m.runningLocked() >= m.maxRuns {
		m.mu.Unlock()
		return Task{}, fmt.Errorf("subagent concurrency limit reached (%d)", m.maxRuns)
	}
	for {
		if _, exists := m.tasks[task.ID]; !exists {
			break
		}
		task.ID = newTaskID(time.Now().UTC())
	}
	m.tasks[task.ID] = task
	runPrompt := m.promptFor(task)
	runCtx, runCancel := context.WithCancel(m.ctx)
	m.controls[task.ID] = taskControl{cancel: runCancel, generation: 1}
	m.wg.Add(1)
	m.mu.Unlock()
	if err := m.save(task); err != nil {
		m.mu.Lock()
		runCancel()
		delete(m.controls, task.ID)
		delete(m.tasks, task.ID)
		m.mu.Unlock()
		m.wg.Done()
		return Task{}, err
	}

	go m.runTask(runCtx, task.ID, 1, runPrompt)
	return task, nil
}

// Send 追加指令并重新启动该子任务；已终止任务不可恢复。
func (m *Manager) Send(id, message string) (Task, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return Task{}, fmt.Errorf("subagent message is empty")
	}
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return Task{}, context.Canceled
	}
	task, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return Task{}, fmt.Errorf("unknown subagent task %q", id)
	}
	if task.Status == StatusTerminated {
		m.mu.Unlock()
		return Task{}, fmt.Errorf("subagent task %s is terminated", id)
	}
	if task.Status != StatusRunning && m.runningLocked() >= m.maxRuns {
		m.mu.Unlock()
		return Task{}, fmt.Errorf("subagent concurrency limit reached (%d)", m.maxRuns)
	}
	generation := uint64(1)
	if control, exists := m.controls[id]; exists {
		control.cancel()
		generation = control.generation + 1
	}
	task.Messages = append(task.Messages, message)
	task.Status = StatusRunning
	task.Error = ""
	task.UpdatedAt = time.Now().UTC()
	m.tasks[id] = task
	runPrompt := m.promptFor(task)
	runCtx, cancel := context.WithCancel(m.ctx)
	m.controls[id] = taskControl{cancel: cancel, generation: generation}
	m.wg.Add(1)
	m.mu.Unlock()
	if err := m.save(task); err != nil {
		cancel()
		m.mu.Lock()
		delete(m.controls, id)
		task.Status = StatusError
		task.Error = "save subagent task: " + err.Error()
		m.tasks[id] = task
		m.mu.Unlock()
		m.wg.Done()
		return Task{}, err
	}
	go m.runTask(runCtx, id, generation, runPrompt)
	return task, nil
}

// Interrupt 暂停运行中的子任务，后续可用 Send 追加指令并恢复。
func (m *Manager) Interrupt(id string) (Task, error) {
	return m.stop(id, StatusInterrupted)
}

// Terminate 永久终止子任务，拒绝后续恢复。
func (m *Manager) Terminate(id string) (Task, error) {
	return m.stop(id, StatusTerminated)
}

// Close cancels running tasks and waits for their final state to be saved.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closing = true
		m.cancel()
		m.mu.Unlock()
	})
	m.wg.Wait()
	return nil
}

// Get 返回指定任务的快照。
func (m *Manager) Get(id string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	return task, ok
}

// List 按更新时间倒序返回任务快照，limit <= 0 时返回全部。
func (m *Manager) List(limit int) []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	tasks := make([]Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
	})
	if limit > 0 && len(tasks) > limit {
		tasks = tasks[:limit]
	}
	return tasks
}

// Roles 返回按名称排序的可选角色定义。
func (m *Manager) Roles() []Role {
	m.mu.Lock()
	defer m.mu.Unlock()
	roles := make([]Role, 0, len(m.roles))
	for _, role := range m.roles {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles
}

// runTask 在后台执行模型任务，并把最终状态写回内存和磁盘。
func (m *Manager) runTask(ctx context.Context, id string, generation uint64, prompt string) {
	defer m.wg.Done()
	result, err := m.run(ctx, prompt)
	m.mu.Lock()
	control, active := m.controls[id]
	if !active || control.generation != generation {
		m.mu.Unlock()
		return
	}
	delete(m.controls, id)
	task := m.tasks[id]
	task.UpdatedAt = time.Now().UTC()
	if err != nil {
		task.Status = StatusError
		task.Error = err.Error()
	} else {
		task.Status = StatusDone
		task.Result = result.Content
		task.Transcript = result.Transcript
	}
	m.tasks[id] = task
	m.mu.Unlock()
	_ = m.save(task)
}

func (m *Manager) stop(id string, status Status) (Task, error) {
	m.mu.Lock()
	task, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return Task{}, fmt.Errorf("unknown subagent task %q", id)
	}
	if task.Status != StatusRunning && status != StatusTerminated {
		m.mu.Unlock()
		return Task{}, fmt.Errorf("subagent task %s is not running", id)
	}
	if task.Status == StatusTerminated {
		m.mu.Unlock()
		return Task{}, fmt.Errorf("subagent task %s is terminated", id)
	}
	if control, exists := m.controls[id]; exists {
		control.cancel()
		delete(m.controls, id)
	}
	task.Status = status
	task.Error = ""
	task.UpdatedAt = time.Now().UTC()
	m.tasks[id] = task
	m.mu.Unlock()
	return task, m.save(task)
}

func (m *Manager) runningLocked() int {
	running := 0
	for _, task := range m.tasks {
		if task.Status == StatusRunning {
			running++
		}
	}
	return running
}

func (m *Manager) promptFor(task Task) string {
	parts := make([]string, 0, len(task.Messages)+3)
	if role, ok := m.roles[task.Role]; ok && strings.TrimSpace(role.Instructions) != "" {
		parts = append(parts, "Subagent role "+role.Name+":\n"+strings.TrimSpace(role.Instructions))
	}
	parts = append(parts, task.Prompt)
	if task.Result != "" {
		parts = append(parts, "Previous result:\n"+task.Result)
	}
	for _, message := range task.Messages {
		parts = append(parts, "Follow-up instruction:\n"+message)
	}
	return strings.Join(parts, "\n\n")
}

// load 读取历史任务；重启后仍处于 running 的任务会标记为 error。
func (m *Manager) load() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			return err
		}
		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			return err
		}
		if task.Status == StatusRunning {
			task.Status = StatusError
			task.Error = "interrupted by process restart"
			task.UpdatedAt = time.Now().UTC()
			_ = m.save(task)
		}
		m.tasks[task.ID] = task
	}
	return nil
}

// save 原子性要求不高，直接覆盖单任务 JSON，便于人工排查。
func (m *Manager) save(task Task) error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(m.dir, task.ID+".json")
	tmp, err := os.CreateTemp(m.dir, ".subagent-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// newTaskID 生成时间有序的短 ID，方便 TUI 和日志里阅读。
func newTaskID(t time.Time) string {
	return "agent-" + strings.ReplaceAll(t.Format("20060102T150405.000000000"), ".", "")
}
