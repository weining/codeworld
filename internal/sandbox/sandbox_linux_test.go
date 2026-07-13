//go:build linux

package sandbox

import (
	"strings"
	"testing"
)

func TestLinuxWorkspaceAndNetworkIsolationArguments(t *testing.T) {
	root := t.TempDir()
	_, args, err := platformCommand(Default(root), root, "sh", []string{"-c", "true"})
	if err != nil {
		t.Skip(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--ro-bind / /", "--bind " + root + " " + root, "--unshare-net", "--die-with-parent"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("sandbox arguments %q do not contain %q", joined, want)
		}
	}
}

func TestLinuxBindsAdditionalWritableRoot(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()
	_, args, err := platformCommand(Policy{Mode: ModeWorkspaceWrite, Workspace: root, WritableRoots: []string{extra}}, root, "sh", nil)
	if err != nil {
		t.Skip(err)
	}
	if joined := strings.Join(args, " "); !strings.Contains(joined, "--bind "+extra+" "+extra) {
		t.Fatalf("sandbox arguments %q do not bind additional root", joined)
	}
}
