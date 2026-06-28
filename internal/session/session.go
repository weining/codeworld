package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Message struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type ToolEvent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type Session struct {
	ID        string      `json:"id"`
	Workspace string      `json:"workspace"`
	Provider  string      `json:"provider"`
	Model     string      `json:"model"`
	Messages  []Message   `json:"messages"`
	Tools     []ToolEvent `json:"tools"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type Store struct {
	root string
}

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

func NewStore(root string) Store {
	return Store{root: filepath.Clean(root)}
}

func (s Store) CurrentPath() string {
	return filepath.Join(s.root, ".codeworld", "current-session.json")
}

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

func (s Store) archivePath(id string) (string, error) {
	if id == "" || id == "." || id == ".." || filepath.IsAbs(id) || filepath.Base(id) != id || strings.Contains(id, "\\") {
		return "", fmt.Errorf("invalid session ID %q", id)
	}
	return filepath.Join(s.root, ".codeworld", "sessions", id+".json"), nil
}

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

func writeSessionFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func newID(now time.Time) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("sess-%s", now.Format("20060102T150405.000000000Z"))
	}
	return fmt.Sprintf("sess-%s-%s", now.Format("20060102T150405.000000000Z"), hex.EncodeToString(b[:]))
}
