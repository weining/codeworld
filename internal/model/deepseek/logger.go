package deepseek

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

// NewJSONLLogger 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewJSONLLogger(out io.Writer) *JSONLLogger {
	return &JSONLLogger{out: out}
}

// LogCall 提供对外可复用的能力，并隐藏内部实现细节。
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
	if _, err := l.out.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

type FileJSONLLogger struct {
	mu   sync.Mutex
	path string
}

// NewFileJSONLLogger 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewFileJSONLLogger(path string) *FileJSONLLogger {
	return &FileJSONLLogger{path: filepath.Clean(path)}
}

// LogCall 提供对外可复用的能力，并隐藏内部实现细节。
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
