package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"codeworld/internal/mcp"
)

func TestMCPCommandsManageUserServers(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("CODEWORLD_HOME", home)
	var out bytes.Buffer
	if err := runMCPCommand(&out, root, globalOptions{}, []string{"add", "demo", "--", "node", "server.js"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runMCPCommand(&out, root, globalOptions{}, []string{"list", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name": "demo"`) || !strings.Contains(out.String(), `"command": "node"`) {
		t.Fatalf("list = %s", out.String())
	}
	out.Reset()
	if err := runMCPCommand(&out, root, globalOptions{}, []string{"remove", "demo"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runMCPCommand(&out, root, globalOptions{}, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no MCP servers") {
		t.Fatalf("list after remove = %q", out.String())
	}
}

func TestMCPAddHTTPValidatesURL(t *testing.T) {
	t.Setenv("CODEWORLD_HOME", t.TempDir())
	if err := runMCPCommand(&bytes.Buffer{}, t.TempDir(), globalOptions{}, []string{"add", "remote", "--url", "file:///tmp/mcp"}); err == nil {
		t.Fatal("invalid URL accepted")
	}
}

func TestMCPLogoutIsIdempotent(t *testing.T) {
	root := t.TempDir()
	if err := mcp.SaveOAuthToken(root, mcp.OAuthToken{ServerName: "remote", AccessToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runMCPCommand(&out, root, globalOptions{}, []string{"logout", "remote"}); err != nil {
		t.Fatal(err)
	}
	if err := runMCPCommand(&out, root, globalOptions{}, []string{"logout", "remote"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mcp.LoadOAuthToken(root, "remote"); !os.IsNotExist(err) {
		t.Fatalf("token remains after logout: %v", err)
	}
}
