package subagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeworld/internal/tools"
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
	info, err := os.Stat(filepath.Join(root, ".codeworld", "subagents", task.ID+".json"))
	if err != nil {
		t.Fatalf("Stat task file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("task file mode = %o, want 600", info.Mode().Perm())
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

func TestManagerCloseCancelsRunningTasks(t *testing.T) {
	manager, err := NewManager(t.TempDir(), func(ctx context.Context, prompt string) (RunResult, error) {
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	task, err := manager.Start("wait")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	done, ok := manager.Get(task.ID)
	if !ok || done.Status != StatusError || !strings.Contains(done.Error, "canceled") {
		t.Fatalf("task after Close = %#v, ok=%v", done, ok)
	}
}

func TestManagerEnforcesConcurrencyLimit(t *testing.T) {
	started := make(chan struct{}, 1)
	manager, err := NewManagerWithOptions(context.Background(), t.TempDir(), func(ctx context.Context, prompt string) (RunResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}, Options{MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start("first"); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := manager.Start("second"); err == nil || !strings.Contains(err.Error(), "concurrency limit") {
		t.Fatalf("second Start error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerInterruptSendAndRole(t *testing.T) {
	prompts := make(chan string, 2)
	manager, err := NewManagerWithOptions(context.Background(), t.TempDir(), func(ctx context.Context, prompt string) (RunResult, error) {
		prompts <- prompt
		if strings.Contains(prompt, "Follow-up instruction") {
			return RunResult{Content: "resumed"}, nil
		}
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}, Options{Roles: []Role{{Name: "reviewer", Instructions: "Focus on regressions."}}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := manager.StartWithOptions("inspect", StartOptions{Role: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if prompt := <-prompts; !strings.Contains(prompt, "Focus on regressions") {
		t.Fatalf("role prompt = %q", prompt)
	}
	interrupted, err := manager.Interrupt(task.ID)
	if err != nil || interrupted.Status != StatusInterrupted {
		t.Fatalf("Interrupt = %#v, %v", interrupted, err)
	}
	if _, err := manager.Send(task.ID, "check tests too"); err != nil {
		t.Fatal(err)
	}
	if prompt := <-prompts; !strings.Contains(prompt, "check tests too") {
		t.Fatalf("follow-up prompt = %q", prompt)
	}
	waitForTask(t, manager, task.ID, StatusDone)
	done, _ := manager.Get(task.ID)
	if done.Result != "resumed" || len(done.Messages) != 1 {
		t.Fatalf("resumed task = %#v", done)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerTerminateIsFinal(t *testing.T) {
	started := make(chan struct{}, 1)
	manager, err := NewManager(t.TempDir(), func(ctx context.Context, prompt string) (RunResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := manager.Start("wait")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	terminated, err := manager.Terminate(task.ID)
	if err != nil || terminated.Status != StatusTerminated {
		t.Fatalf("Terminate = %#v, %v", terminated, err)
	}
	if _, err := manager.Send(task.ID, "resume"); err == nil {
		t.Fatal("terminated task resumed")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
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

func TestControlToolsExposeRoleAndTerminateTask(t *testing.T) {
	started := make(chan struct{}, 1)
	manager, err := NewManagerWithOptions(context.Background(), t.TempDir(), func(ctx context.Context, prompt string) (RunResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}, Options{Roles: []Role{{Name: "reviewer"}}})
	if err != nil {
		t.Fatal(err)
	}
	definition := NewStartTool(manager).Definition()
	role := definition.InputSchema["properties"].(map[string]any)["role"].(map[string]any)
	if len(role["enum"].([]string)) != 1 || role["enum"].([]string)[0] != "reviewer" {
		t.Fatalf("role schema = %#v", role)
	}
	task, err := manager.Start("wait")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	var terminate tools.Tool
	for _, tool := range NewControlTools(manager) {
		if tool.Definition().Name == "subagent_terminate" {
			terminate = tool
		}
	}
	if terminate == nil {
		t.Fatal("terminate tool missing")
	}
	if _, err := terminate.Execute(context.Background(), []byte(`{"id":"`+task.ID+`"}`)); err != nil {
		t.Fatal(err)
	}
	terminated, _ := manager.Get(task.ID)
	if terminated.Status != StatusTerminated {
		t.Fatalf("task = %#v", terminated)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
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
