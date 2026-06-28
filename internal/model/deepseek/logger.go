package deepseek

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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

func NewJSONLLogger(out io.Writer) *JSONLLogger {
	return &JSONLLogger{out: out}
}

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

func NewFileJSONLLogger(path string) *FileJSONLLogger {
	return &FileJSONLLogger{path: filepath.Clean(path)}
}

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
	Method   string          `json:"method"`
	URL      string          `json:"url"`
	Headers  http.Header     `json:"headers"`
	Body     string          `json:"body,omitempty"`
	BodyJSON json.RawMessage `json:"body_json,omitempty"`
}

type httpResponseLog struct {
	StatusCode int             `json:"status_code"`
	Status     string          `json:"status"`
	Headers    http.Header     `json:"headers"`
	Body       string          `json:"body,omitempty"`
	BodyJSON   json.RawMessage `json:"body_json,omitempty"`
}
