package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const browserDiagnosticLimit = 200

func (c *cdpClient) recordDiagnostic(message cdpMessage) {
	var line string
	switch message.Method {
	case "Network.requestWillBeSent":
		var event struct {
			RequestID string `json:"requestId"`
			Request   struct {
				URL string `json:"url"`
			} `json:"request"`
		}
		if json.Unmarshal(message.Params, &event) == nil && event.RequestID != "" {
			if c.requestURLs == nil {
				c.requestURLs = map[string]string{}
			}
			c.requestURLs[event.RequestID] = event.Request.URL
		}
		return
	case "Runtime.consoleAPICalled":
		var event struct {
			Type string `json:"type"`
			Args []struct {
				Value       any    `json:"value"`
				Description string `json:"description"`
			} `json:"args"`
		}
		if json.Unmarshal(message.Params, &event) == nil {
			parts := make([]string, 0, len(event.Args))
			for _, arg := range event.Args {
				value := arg.Description
				if arg.Value != nil {
					if encoded, err := json.Marshal(arg.Value); err == nil {
						value = strings.Trim(string(encoded), `"`)
					}
				}
				if value != "" {
					parts = append(parts, value)
				}
			}
			line = "console." + firstNonEmpty(event.Type, "log") + ": " + strings.Join(parts, " ")
		}
	case "Runtime.exceptionThrown":
		var event struct {
			Exception struct {
				Text      string `json:"text"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(message.Params, &event) == nil {
			line = "javascript exception: " + firstNonEmpty(event.Exception.Exception.Description, event.Exception.Text)
		}
	case "Log.entryAdded":
		var event struct {
			Entry struct {
				Level string `json:"level"`
				Text  string `json:"text"`
				URL   string `json:"url"`
			} `json:"entry"`
		}
		if json.Unmarshal(message.Params, &event) == nil {
			line = "log." + firstNonEmpty(event.Entry.Level, "info") + ": " + event.Entry.Text
			if event.Entry.URL != "" {
				line += " (" + event.Entry.URL + ")"
			}
		}
	case "Network.responseReceived":
		var event struct {
			Response struct {
				URL        string  `json:"url"`
				Status     float64 `json:"status"`
				StatusText string  `json:"statusText"`
			} `json:"response"`
		}
		if json.Unmarshal(message.Params, &event) == nil && event.Response.Status >= 400 {
			line = fmt.Sprintf("HTTP %.0f %s: %s", event.Response.Status, event.Response.StatusText, event.Response.URL)
		}
	case "Network.loadingFinished":
		var event struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(message.Params, &event) == nil {
			delete(c.requestURLs, event.RequestID)
		}
		return
	case "Network.loadingFailed":
		var event struct {
			RequestID string `json:"requestId"`
			ErrorText string `json:"errorText"`
			Canceled  bool   `json:"canceled"`
		}
		if json.Unmarshal(message.Params, &event) == nil && !event.Canceled {
			line = "network failed: " + event.ErrorText
			if target := c.requestURLs[event.RequestID]; target != "" {
				line += ": " + target
			}
		}
		delete(c.requestURLs, event.RequestID)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if len(line) > 2000 {
		line = line[:2000]
	}
	c.diagnostics = append(c.diagnostics, line)
	if len(c.diagnostics) > browserDiagnosticLimit {
		c.diagnostics = append([]string(nil), c.diagnostics[len(c.diagnostics)-browserDiagnosticLimit:]...)
	}
}

func (m *BrowserSessionManager) diagnostics(ctx context.Context, id string, clear bool) (string, error) {
	_, client, err := m.active(id)
	if err != nil {
		return "", err
	}
	// CDP events share the command socket, so a harmless evaluation drains events
	// already queued ahead of its response before the snapshot is copied.
	if _, err := client.call(ctx, "Runtime.evaluate", map[string]any{"expression": "void 0"}); err != nil {
		return "", err
	}
	client.mu.Lock()
	lines := append([]string(nil), client.diagnostics...)
	if clear {
		client.diagnostics = nil
	}
	client.mu.Unlock()
	if len(lines) == 0 {
		return "No browser diagnostics.", nil
	}
	return strings.Join(lines, "\n"), nil
}

func (m *BrowserSessionManager) upload(ctx context.Context, id, selector, filePath string) error {
	_, client, err := m.active(id)
	if err != nil {
		return err
	}
	result, err := client.call(ctx, "DOM.getDocument", map[string]any{"depth": 0})
	if err != nil {
		return err
	}
	var document struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(result, &document); err != nil {
		return err
	}
	result, err = client.call(ctx, "DOM.querySelector", map[string]any{"nodeId": document.Root.NodeID, "selector": selector})
	if err != nil {
		return err
	}
	var match struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := json.Unmarshal(result, &match); err != nil {
		return err
	}
	if match.NodeID == 0 {
		return fmt.Errorf("browser upload selector not found")
	}
	_, err = client.call(ctx, "DOM.setFileInputFiles", map[string]any{"nodeId": match.NodeID, "files": []string{filePath}})
	return err
}

func (m *BrowserSessionManager) download(ctx context.Context, id, selector, downloadDir string, waitMS int) ([]string, error) {
	_, client, err := m.active(id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return nil, err
	}
	before, err := browserDirectoryFiles(downloadDir)
	if err != nil {
		return nil, err
	}
	if _, err := client.call(ctx, "Page.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": downloadDir}); err != nil {
		return nil, err
	}
	result, err := client.call(ctx, "Runtime.evaluate", map[string]any{"expression": clickExpression(selector), "awaitPromise": true, "returnByValue": true})
	if err != nil {
		return nil, err
	}
	if err := evaluationError(result); err != nil {
		return nil, err
	}
	if waitMS == 0 {
		waitMS = 5000
	}
	deadline := time.Now().Add(time.Duration(waitMS) * time.Millisecond)
	for {
		current, err := browserDirectoryFiles(downloadDir)
		if err != nil {
			return nil, err
		}
		var downloaded []string
		for name := range current {
			if !before[name] && !strings.HasSuffix(name, ".crdownload") {
				path := filepath.Join(downloadDir, name)
				if _, err := m.workspace.Resolve(path); err != nil {
					return nil, err
				}
				rel, err := m.workspace.Rel(path)
				if err != nil {
					return nil, err
				}
				downloaded = append(downloaded, rel)
			}
		}
		if len(downloaded) > 0 {
			sort.Strings(downloaded)
			return downloaded, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("browser download did not finish within %dms", waitMS)
		}
		if err := waitBrowserAction(ctx, 25); err != nil {
			return nil, err
		}
	}
}

func browserDirectoryFiles(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			files[entry.Name()] = true
		}
	}
	return files, nil
}
