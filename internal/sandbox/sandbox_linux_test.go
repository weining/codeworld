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
