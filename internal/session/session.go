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
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	Parts      []ContentPart `json:"parts,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
}

type ContentPart struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ImageURL  string `json:"image_url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	Path      string `json:"path,omitempty"`
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
	ID           string      `json:"id"`
	Workspace    string      `json:"workspace"`
	Provider     string      `json:"provider"`
	Model        string      `json:"model"`
	Messages     []Message   `json:"messages"`
	Summary      string      `json:"summary,omitempty"`
	Goal         string      `json:"goal,omitempty"`
	Mode         string      `json:"mode,omitempty"`
	ApprovalMode string      `json:"approval_mode,omitempty"`
	Tools        []ToolEvent `json:"tools"`
	Approvals    []Approval  `json:"approvals,omitempty"`
	Usage        Usage       `json:"usage,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
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

// ArchivedSessionsPath 返回已归档会话目录。
func (s Store) ArchivedSessionsPath() string {
	return filepath.Join(s.root, ".codeworld", "archived-sessions")
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

	// Publish the archive first so current-session never points at a state that
	// was not durably archived.
	if err := writeSessionFile(archivePath, data); err != nil {
		return err
	}
	return writeSessionFile(currentPath, data)
}

// List 读取归档 session，并按更新时间从新到旧排序。
func (s Store) List() ([]Session, error) {
	return s.listDir(s.SessionsPath())
}

// ListArchived 按更新时间倒序返回已归档会话。
func (s Store) ListArchived() ([]Session, error) {
	return s.listDir(s.ArchivedSessionsPath())
}

func (s Store) listDir(dir string) ([]Session, error) {
	entries, err := os.ReadDir(dir)
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
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var sess Session
		if err := json.Unmarshal(data, &sess); err != nil {
			return nil, err
		}
		if sess.ID != id {
			return nil, fmt.Errorf("session file %q contains mismatched ID %q", entry.Name(), sess.ID)
		}
		sessions = append(sessions, sess)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// Fork 复制指定活动或归档会话，并把副本设为当前会话。
func (s Store) Fork(id string) (Session, error) {
	original, err := s.Load(id)
	if err != nil {
		if !os.IsNotExist(err) {
			return Session{}, err
		}
		original, err = s.LoadArchived(id)
		if err != nil {
			return Session{}, err
		}
	}
	now := time.Now().UTC()
	forked := original
	forked.ID = newID(now)
	forked.Approvals = nil
	forked.ApprovalMode = ""
	forked.CreatedAt = now
	forked.UpdatedAt = now
	if err := s.SaveCurrent(forked); err != nil {
		return Session{}, err
	}
	return forked, nil
}

// Archive 把活动会话移入归档目录；若它是当前会话则同时清除 current 指针。
func (s Store) Archive(id string) error {
	sess, err := s.Load(id)
	if err != nil {
		return err
	}
	path, err := s.archivedPath(id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	if err := writeSessionFile(path, append(data, '\n')); err != nil {
		return err
	}
	activePath, _ := s.archivePath(id)
	if err := os.Remove(activePath); err != nil {
		return err
	}
	return s.removeCurrentIf(id)
}

// Unarchive 把归档会话恢复到活动会话列表，但不改变 current 指针。
func (s Store) Unarchive(id string) error {
	if _, err := s.Load(id); err == nil {
		return fmt.Errorf("session %q is already active", id)
	} else if !os.IsNotExist(err) {
		return err
	}
	sess, err := s.LoadArchived(id)
	if err != nil {
		return err
	}
	path, err := s.archivePath(id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	if err := writeSessionFile(path, append(data, '\n')); err != nil {
		return err
	}
	archivedPath, _ := s.archivedPath(id)
	return os.Remove(archivedPath)
}

// Delete 永久删除活动或归档会话，并清理可能指向它的 current 文件。
func (s Store) Delete(id string) error {
	activePath, err := s.archivePath(id)
	if err != nil {
		return err
	}
	archivedPath, err := s.archivedPath(id)
	if err != nil {
		return err
	}
	removed := false
	for _, path := range []string{activePath, archivedPath} {
		if err := os.Remove(path); err == nil {
			removed = true
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if !removed {
		return fmt.Errorf("session %q not found", id)
	}
	return s.removeCurrentIf(id)
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

// LoadArchived 读取已归档会话。
func (s Store) LoadArchived(id string) (Session, error) {
	path, err := s.archivedPath(id)
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
	if err := validateSessionID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.root, ".codeworld", "sessions", id+".json"), nil
}

func (s Store) archivedPath(id string) (string, error) {
	if err := validateSessionID(id); err != nil {
		return "", err
	}
	return filepath.Join(s.ArchivedSessionsPath(), id+".json"), nil
}

func validateSessionID(id string) error {
	if id == "" || id == "." || id == ".." || filepath.IsAbs(id) || filepath.Base(id) != id || strings.Contains(id, "\\") {
		return fmt.Errorf("invalid session ID %q", id)
	}
	return nil
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

func (s Store) removeCurrentIf(id string) error {
	current, err := s.LoadCurrent()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if current.ID != id {
		return nil
	}
	if err := os.Remove(s.CurrentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// writeSessionFile 写入输出数据，并保证必要的目录或权限约束。
func writeSessionFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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

// newID 封装局部逻辑，保持调用方流程清晰。
func newID(now time.Time) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("sess-%s", now.Format("20060102T150405.000000000Z"))
	}
	return fmt.Sprintf("sess-%s-%s", now.Format("20060102T150405.000000000Z"), hex.EncodeToString(b[:]))
}
