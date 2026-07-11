package config

import (
	"path/filepath"
	"testing"
)

func TestManagedMCPRoundTrip(t *testing.T) {
	home := t.TempDir()
	want := []MCPServer{
		{Name: "local", Command: "node", Args: []string{"server.js", "a b", "a,b"}},
		{Name: "remote", URL: "https://example.test/mcp", BearerTokenEnvVar: "TOKEN", HTTPHeaders: []string{"X-Test: a b"}},
	}
	if err := SaveManagedMCP(home, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManagedMCP(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Args[1] != "a b" || got[0].Args[2] != "a,b" || got[1].HTTPHeaders[0] != "X-Test: a b" {
		t.Fatalf("servers = %#v", got)
	}
	info, err := filepath.Glob(filepath.Join(home, "mcp.toml"))
	if err != nil || len(info) != 1 {
		t.Fatalf("managed config missing: %v %v", info, err)
	}
}

func TestManagedMCPLayerOverridesUserConfig(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	writeFile(t, filepath.Join(home, "config.toml"), "[[mcp_servers]]\nname = \"demo\"\ncommand = \"old\"\n")
	if err := SaveManagedMCP(home, []MCPServer{{Name: "demo", Command: "new"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithOptions(root, LoadOptions{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Command != "new" {
		t.Fatalf("servers = %#v", cfg.MCPServers)
	}
}
