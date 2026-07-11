package tools

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"codeworld/internal/workspace"
)

func TestCommandSessionStreamsIncrementalOutput(t *testing.T) {
	manager := newTestCommandSessionManager(t)
	snapshot, err := manager.Start(context.Background(), "printf first; sleep 0.05; printf second", ".")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	output := snapshot.Output
	cursor := snapshot.Cursor
	deadline := time.Now().Add(3 * time.Second)
	for snapshot.Status == CommandSessionRunning && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		snapshot, err = manager.Poll(snapshot.ID, cursor)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		output += snapshot.Output
		cursor = snapshot.Cursor
	}
	if snapshot.Status != CommandSessionExited || snapshot.ExitCode != 0 {
		t.Fatalf("snapshot = %#v, want successful exit", snapshot)
	}
	if output != "firstsecond" {
		t.Fatalf("output = %q, want firstsecond", output)
	}
	next, err := manager.Poll(snapshot.ID, cursor)
	if err != nil || next.Output != "" || next.Cursor != cursor {
		t.Fatalf("second poll = %#v err=%v", next, err)
	}
}

func TestCommandSessionAcceptsStdin(t *testing.T) {
	manager := newTestCommandSessionManager(t)
	snapshot, err := manager.Start(context.Background(), `IFS= read -r line; printf 'got:%s' "$line"`, ".")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := manager.Write(snapshot.ID, "hello\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	finished := waitCommandSession(t, manager, snapshot.ID)
	if finished.Status != CommandSessionExited || !strings.Contains(finished.Output, "got:hello") {
		t.Fatalf("snapshot = %#v", finished)
	}
}

func TestCommandSessionTerminateAndClose(t *testing.T) {
	manager := newTestCommandSessionManager(t)
	snapshot, err := manager.Start(context.Background(), "sleep 10", ".")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := manager.Terminate(snapshot.ID); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	finished := waitCommandSession(t, manager, snapshot.ID)
	if finished.Status == CommandSessionRunning {
		t.Fatalf("session still running: %#v", finished)
	}
	second, err := manager.Start(context.Background(), "sleep 10", ".")
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	closed, err := manager.Poll(second.ID, 0)
	if err != nil || closed.Status == CommandSessionRunning {
		t.Fatalf("closed session = %#v err=%v", closed, err)
	}
}

func TestCommandSessionPTYAndResize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY is unsupported on Windows")
	}
	manager := newTestCommandSessionManager(t)
	snapshot, err := manager.StartWithOptions(context.Background(), `test -t 0 || exit 9; stty size; printf READY; IFS= read -r line; stty size`, CommandSessionStartOptions{PTY: true, Rows: 20, Cols: 70})
	if err != nil {
		t.Fatalf("StartWithOptions: %v", err)
	}
	if !snapshot.PTY || snapshot.Rows != 20 || snapshot.Cols != 70 {
		t.Fatalf("initial snapshot = %#v", snapshot)
	}
	output := snapshot.Output
	cursor := snapshot.Cursor
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(output, "READY") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		next, err := manager.Poll(snapshot.ID, cursor)
		if err != nil {
			t.Fatalf("Poll ready: %v", err)
		}
		output += next.Output
		cursor = next.Cursor
	}
	if !strings.Contains(output, "20 70") || !strings.Contains(output, "READY") {
		t.Fatalf("initial PTY output = %q", output)
	}
	if err := manager.Resize(snapshot.ID, 40, 100); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if _, err := manager.Write(snapshot.ID, "\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for time.Now().Before(deadline) {
		next, err := manager.Poll(snapshot.ID, cursor)
		if err != nil {
			t.Fatalf("Poll finish: %v", err)
		}
		output += next.Output
		cursor = next.Cursor
		if next.Status != CommandSessionRunning {
			if next.Status != CommandSessionExited || !strings.Contains(output, "40 100") {
				t.Fatalf("final snapshot=%#v output=%q", next, output)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("PTY session did not finish")
}

func newTestCommandSessionManager(t *testing.T) *CommandSessionManager {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	manager := NewCommandSessionManager(ws)
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func waitCommandSession(t *testing.T, manager *CommandSessionManager, id string) CommandSessionSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var output string
	var cursor int64
	for time.Now().Before(deadline) {
		snapshot, err := manager.Poll(id, cursor)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		output += snapshot.Output
		cursor = snapshot.Cursor
		if snapshot.Status != CommandSessionRunning {
			snapshot.Output = output
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %s did not finish", id)
	return CommandSessionSnapshot{}
}
