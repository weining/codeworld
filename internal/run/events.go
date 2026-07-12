package run

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"codeworld/internal/agent"
)

type jsonEventWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func newJSONEventWriter(out io.Writer) *jsonEventWriter {
	return &jsonEventWriter{encoder: json.NewEncoder(out)}
}

func (w *jsonEventWriter) emit(event map[string]any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.encoder.Encode(event)
}

func (w *jsonEventWriter) ReportTool(_ context.Context, event agent.ToolEvent) {
	typeName := "item.completed"
	if event.Status == agent.ToolEventStart {
		typeName = "item.started"
	}
	item := map[string]any{
		"id":     event.CallID,
		"type":   toolItemType(event.Name),
		"name":   event.Name,
		"status": toolItemStatus(event.Status),
	}
	if event.Request.Target != "" {
		item["target"] = event.Request.Target
		switch item["type"] {
		case "command_execution":
			item["command"] = event.Request.Target
		case "file_change":
			item["path"] = event.Request.Target
		case "web_search":
			item["query"] = event.Request.Target
		}
	}
	if event.Request.Risk != "" {
		item["risk"] = event.Request.Risk
	}
	if event.Error != "" {
		item["error"] = event.Error
	}
	_ = w.emit(map[string]any{"type": typeName, "item": item})
}

func toolItemType(name string) string {
	switch {
	case name == "shell" || strings.HasPrefix(name, "shell_"):
		return "command_execution"
	case name == "write_file" || name == "apply_patch" || name == "index_workspace":
		return "file_change"
	case strings.HasPrefix(name, "mcp."):
		return "mcp_tool_call"
	case name == "web_search":
		return "web_search"
	default:
		return "tool_call"
	}
}

func toolItemStatus(status agent.ToolEventStatus) string {
	switch status {
	case agent.ToolEventStart:
		return "in_progress"
	case agent.ToolEventSuccess:
		return "completed"
	default:
		return "failed"
	}
}

func (w *jsonEventWriter) turnEvent(event agent.TurnEvent) error {
	switch event.Kind {
	case agent.TurnEventAssistantDelta:
		return w.emit(map[string]any{
			"type": "item.updated",
			"item": map[string]any{"type": "agent_message", "delta": event.Text},
		})
	case agent.TurnEventAssistantDone:
		return w.emit(map[string]any{
			"type": "item.completed",
			"item": map[string]any{"type": "agent_message", "text": event.Text},
		})
	case agent.TurnEventUsage:
		return w.emit(map[string]any{
			"type":  "usage.updated",
			"usage": event.Usage,
		})
	case agent.TurnEventError:
		message := event.Text
		if message == "" && event.Err != nil {
			message = event.Err.Error()
		}
		return w.emit(map[string]any{"type": "error", "message": message})
	default:
		return nil
	}
}
