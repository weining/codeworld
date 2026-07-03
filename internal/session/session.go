package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolEvent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type Approval struct {
	Kind    string `json:"kind"`
	Command string `json:"command,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	CacheTokens  int `json:"cache_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type Session struct {
	ID        string      `json:"id"`
	Workspace string      `json:"workspace"`
	Provider  string      `json:"provider"`
	Model     string      `json:"model"`
	Messages  []Message   `json:"messages"`
	Summary   string      `json:"summary,omitempty"`
	Tools     []ToolEvent `json:"tools"`
	Approvals []Approval  `json:"approvals,omitempty"`
	Usage     Usage       `json:"usage,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type Store struct {
	root string
}

// New 创建并返回对应组件，集中设置默认依赖和初始状态。
func New(workspace, provider, model string) Session {
	now := time.Now().UTC()
	return Session{
		ID:        newID(now),
		Workspace: workspace,
		Provider:  provider,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewStore 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewStore(root string) Store {
	return Store{root: filepath.Clean(root)}
}

// CurrentPath 提供对外可复用的能力，并隐藏内部实现细节。
func (s Store) CurrentPath() string {
	return filepath.Join(s.root, ".codeworld", "current-session.json")
}

// SessionsPath 返回归档 session 目录，供恢复会话和测试复用。
func (s Store) SessionsPath() string {
	return filepath.Join(s.root, ".codeworld", "sessions")
}

// SaveCurrent 持久化当前状态，并处理路径、权限或归档细节。
func (s Store) SaveCurrent(sess Session) error {
	currentPath := s.CurrentPath()
	archivePath, err := s.archivePath(sess.ID)
	if err != nil {
		return err
	}
	sess.UpdatedAt = time.Now().UTC()

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := writeSessionFile(currentPath, data); err != nil {
		return err
	}
	return writeSessionFile(archivePath, data)
}

// List 读取归档 session，并按更新时间从新到旧排序。
func (s Store) List() ([]Session, error) {
	entries, err := os.ReadDir(s.SessionsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sessions := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		sess, err := s.Load(id)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// Load 根据 session ID 读取归档会话，并复用 archivePath 的 ID 校验。
func (s Store) Load(id string) (Session, error) {
	path, err := s.archivePath(id)
	if err != nil {
		return Session{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// SetCurrent 把指定归档会话设为 current-session，供 resume 命令复用。
func (s Store) SetCurrent(id string) error {
	sess, err := s.Load(id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeSessionFile(s.CurrentPath(), data)
}

// archivePath 封装局部逻辑，保持调用方流程清晰。
func (s Store) archivePath(id string) (string, error) {
	if id == "" || id == "." || id == ".." || filepath.IsAbs(id) || filepath.Base(id) != id || strings.Contains(id, "\\") {
		return "", fmt.Errorf("invalid session ID %q", id)
	}
	return filepath.Join(s.root, ".codeworld", "sessions", id+".json"), nil
}

// LoadCurrent 加载外部或项目内配置，并把原始数据转换为内部结构。
func (s Store) LoadCurrent() (Session, error) {
	data, err := os.ReadFile(s.CurrentPath())
	if err != nil {
		return Session{}, err
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// writeSessionFile 写入输出数据，并保证必要的目录或权限约束。
func writeSessionFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// newID 封装局部逻辑，保持调用方流程清晰。
func newID(now time.Time) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("sess-%s", now.Format("20060102T150405.000000000Z"))
	}
	return fmt.Sprintf("sess-%s-%s", now.Format("20060102T150405.000000000Z"), hex.EncodeToString(b[:]))
}
