package run

import (
	"context"
	"encoding/json"
	"io"
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
		"type":   "tool_call",
		"name":   event.Name,
		"status": string(event.Status),
	}
	if event.Request.Target != "" {
		item["target"] = event.Request.Target
	}
	if event.Request.Risk != "" {
		item["risk"] = event.Request.Risk
	}
	if event.Error != "" {
		item["error"] = event.Error
	}
	_ = w.emit(map[string]any{"type": typeName, "item": item})
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
