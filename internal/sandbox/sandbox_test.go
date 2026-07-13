package sandbox

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPolicyValidate(t *testing.T) {
	root := t.TempDir()
	for _, mode := range []Mode{ModeReadOnly, ModeWorkspaceWrite, ModeDangerFullAccess} {
		if err := (Policy{Mode: mode, Workspace: root}).Validate(); err != nil {
			t.Fatalf("validate %s: %v", mode, err)
		}
	}
	if err := (Policy{Mode: "unknown", Workspace: root}).Validate(); err == nil {
		t.Fatal("expected invalid mode error")
	}
	if err := (Policy{Mode: ModeReadOnly, Workspace: "relative"}).Validate(); err == nil {
		t.Fatal("expected relative workspace error")
	}
	if err := (Policy{Mode: ModeWorkspaceWrite, Workspace: root, WritableRoots: []string{"relative"}}).Validate(); err == nil {
		t.Fatal("relative writable root accepted")
	}
}

func TestFullAccessDoesNotWrapCommand(t *testing.T) {
	root := t.TempDir()
	cmd, err := FullAccess(root).CommandContext(context.Background(), root, "sh", "-c", "true")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cmd.Path) != "sh" {
		t.Fatalf("command path = %q", cmd.Path)
	}
}

func TestPlatformCommandIsFailClosedOrWrapped(t *testing.T) {
	root := t.TempDir()
	cmd, err := Default(root).Command(root, "sh", "-c", "true")
	if err != nil {
		if runtime.GOOS == "darwin" && !strings.Contains(err.Error(), "sandbox-exec") {
			t.Fatalf("unexpected macOS error: %v", err)
		}
		if runtime.GOOS == "linux" && !strings.Contains(err.Error(), "bwrap") {
			t.Fatalf("unexpected Linux error: %v", err)
		}
		return
	}
	if filepath.Base(cmd.Path) == "sh" {
		t.Fatalf("restricted command was not wrapped: %q", cmd.Path)
	}
}
