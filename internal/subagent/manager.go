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
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusError   Status = "error"
)

type Task struct {
	ID         string              `json:"id"`
	Prompt     string              `json:"prompt"`
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

type Manager struct {
	root  string
	dir   string
	run   RunnerFunc
	mu    sync.Mutex
	tasks map[string]Task
}

// NewManager 创建本地子代理管理器，并恢复已持久化的历史任务。
func NewManager(root string, run RunnerFunc) (*Manager, error) {
	manager := &Manager{
		root:  root,
		dir:   filepath.Join(root, ".codeworld", "subagents"),
		run:   run,
		tasks: map[string]Task{},
	}
	if err := manager.load(); err != nil {
		return nil, err
	}
	return manager, nil
}

// Start 创建后台子任务，任务结果会写入 .codeworld/subagents。
func (m *Manager) Start(prompt string) (Task, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Task{}, fmt.Errorf("subagent prompt is empty")
	}
	if m.run == nil {
		return Task{}, fmt.Errorf("subagent runner is not configured")
	}
	now := time.Now().UTC()
	task := Task{
		ID:        newTaskID(now),
		Prompt:    prompt,
		Status:    StatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.mu.Lock()
	for {
		if _, exists := m.tasks[task.ID]; !exists {
			break
		}
		task.ID = newTaskID(time.Now().UTC())
	}
	m.tasks[task.ID] = task
	m.mu.Unlock()
	if err := m.save(task); err != nil {
		return Task{}, err
	}

	go m.runTask(task.ID, prompt)
	return task, nil
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

// runTask 在后台执行模型任务，并把最终状态写回内存和磁盘。
func (m *Manager) runTask(id, prompt string) {
	result, err := m.run(context.Background(), prompt)
	m.mu.Lock()
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
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.dir, task.ID+".json"), append(data, '\n'), 0o644)
}

// newTaskID 生成时间有序的短 ID，方便 TUI 和日志里阅读。
func newTaskID(t time.Time) string {
	return "agent-" + strings.ReplaceAll(t.Format("20060102T150405.000000000"), ".", "")
}
