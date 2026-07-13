//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

func TestDarwinProfileRestrictsWritesAndNetwork(t *testing.T) {
	root := t.TempDir()
	_, args, err := platformCommand(Default(root), root, "sh", []string{"-c", "true"})
	if err != nil {
		t.Skip(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"deny file-write", "deny network", "allow file-write", root} {
		if !strings.Contains(joined, want) {
			t.Fatalf("sandbox arguments %q do not contain %q", joined, want)
		}
	}
}

func TestDarwinReadOnlyProfileDoesNotAllowWorkspaceWrites(t *testing.T) {
	root := t.TempDir()
	_, args, err := platformCommand(Policy{Mode: ModeReadOnly, Workspace: root}, root, "sh", nil)
	if err != nil {
		t.Skip(err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "allow file-write") {
		t.Fatalf("read-only profile unexpectedly allows writes: %q", joined)
	}
}

func TestDarwinProfileAllowsAdditionalWritableRoot(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()
	_, args, err := platformCommand(Policy{Mode: ModeWorkspaceWrite, Workspace: root, WritableRoots: []string{extra}}, root, "sh", nil)
	if err != nil {
		t.Skip(err)
	}
	if joined := strings.Join(args, " "); !strings.Contains(joined, extra) {
		t.Fatalf("sandbox arguments %q do not contain additional root %q", joined, extra)
	}
}
