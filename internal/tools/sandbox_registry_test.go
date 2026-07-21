package tools

import (
	"testing"

	"codeworld/internal/sandbox"
	"codeworld/internal/workspace"
)

func TestReadOnlySandboxOmitsMutationAndNetworkTools(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := NewDefaultRegistryWithSandbox(ws, sandbox.Policy{Mode: sandbox.ModeReadOnly, Workspace: ws.Root})
	for _, name := range []string{"write_file", "apply_patch", "index_workspace", "web_search", "browser_open"} {
		if _, ok := registry.Get(name); ok {
			t.Fatalf("read-only registry unexpectedly contains %q", name)
		}
	}
	if _, ok := registry.Get("shell"); !ok {
		t.Fatal("read-only registry should retain OS-sandboxed shell")
	}
}

func TestWorkspaceSandboxRegistersWritesAndOptInNetwork(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	policy := sandbox.Policy{Mode: sandbox.ModeWorkspaceWrite, Workspace: ws.Root, Network: true}
	registry := NewDefaultRegistryWithSandbox(ws, policy)
	for _, name := range []string{"write_file", "apply_patch", "index_workspace", "web_search", "browser_open"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("workspace registry does not contain %q", name)
		}
	}
}
