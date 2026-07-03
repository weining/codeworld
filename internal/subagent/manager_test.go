package subagent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestManagerStartsAndPersistsTask 验证子代理任务能异步完成并持久化状态。
func TestManagerStartsAndPersistsTask(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(root, func(ctx context.Context, prompt string) (RunResult, error) {
		return RunResult{
			Content:    "result for " + prompt,
			Transcript: []TranscriptMessage{{Role: "user", Content: prompt}, {Role: "assistant", Content: "result for " + prompt}},
		}, nil
	})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	task, err := manager.Start("inspect files")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	waitForTask(t, manager, task.ID, StatusDone)
	done, ok := manager.Get(task.ID)
	if !ok {
		t.Fatalf("task %s missing", task.ID)
	}
	if done.Result != "result for inspect files" {
		t.Fatalf("result = %q, want expected result", done.Result)
	}
	if len(done.Transcript) != 2 {
		t.Fatalf("transcript = %#v, want two messages", done.Transcript)
	}

	restored, err := NewManager(root, func(ctx context.Context, prompt string) (RunResult, error) {
		return RunResult{}, nil
	})
	if err != nil {
		t.Fatalf("NewManager restore returned error: %v", err)
	}
	loaded, ok := restored.Get(task.ID)
	if !ok || loaded.Status != StatusDone || loaded.Result == "" {
		t.Fatalf("restored task = %#v, ok=%v", loaded, ok)
	}
}

// TestStatusToolListsAndReadsTasks 验证 status 工具可列出任务并读取详情。
func TestStatusToolListsAndReadsTasks(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(root, func(ctx context.Context, prompt string) (RunResult, error) {
		return RunResult{Content: "done"}, nil
	})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	task, err := manager.Start("collect context")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	waitForTask(t, manager, task.ID, StatusDone)

	status := NewStatusTool(manager)
	list, err := status.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("Execute list returned error: %v", err)
	}
	if !strings.Contains(list.Content, task.ID) {
		t.Fatalf("list content = %q, want task id", list.Content)
	}
	detail, err := status.Execute(context.Background(), []byte(`{"id":"`+task.ID+`"}`))
	if err != nil {
		t.Fatalf("Execute detail returned error: %v", err)
	}
	if !strings.Contains(detail.Content, "result:\ndone") {
		t.Fatalf("detail content = %q, want result", detail.Content)
	}
}

// waitForTask 是测试辅助函数，用于等待异步任务完成。
func waitForTask(t *testing.T, manager *Manager, id string, want Status) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := manager.Get(id)
		if ok && task.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := manager.Get(id)
	t.Fatalf("task status = %s, want %s", task.Status, want)
}
