package codex

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CallLogger interface {
	LogCall(ctx context.Context, entry callLogEntry) error
}

type JSONLLogger struct {
	mu  sync.Mutex
	out io.Writer
}

// NewJSONLLogger 创建 JSONL logger；只记录 body_json，不记录 header 或 token。
func NewJSONLLogger(out io.Writer) *JSONLLogger {
	return &JSONLLogger{out: out}
}

// LogCall 写入单条模型调用日志。
func (l *JSONLLogger) LogCall(ctx context.Context, entry callLogEntry) error {
	if l == nil || l.out == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = l.out.Write(append(data, '\n'))
	return err
}

type FileJSONLLogger struct {
	mu   sync.Mutex
	path string
}

// NewFileJSONLLogger 创建文件 JSONL logger，日志文件权限固定为 0600。
func NewFileJSONLLogger(path string) *FileJSONLLogger {
	return &FileJSONLLogger{path: filepath.Clean(path)}
}

// LogCall 追加写入模型调用日志文件。
func (l *FileJSONLLogger) LogCall(ctx context.Context, entry callLogEntry) error {
	if l == nil || l.path == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := os.Chmod(l.path, 0o600); err != nil {
		return err
	}
	return NewJSONLLogger(file).LogCall(ctx, entry)
}

type callLogEntry struct {
	Timestamp time.Time        `json:"timestamp"`
	Provider  string           `json:"provider"`
	Request   httpRequestLog   `json:"request"`
	Response  *httpResponseLog `json:"response,omitempty"`
	Error     string           `json:"error,omitempty"`
}

type httpRequestLog struct {
	BodyJSON json.RawMessage `json:"body_json,omitempty"`
}

type httpResponseLog struct {
	BodyJSON json.RawMessage `json:"body_json,omitempty"`
}
