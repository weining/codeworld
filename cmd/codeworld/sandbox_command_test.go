package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSandboxCommandArgs(t *testing.T) {
	opts, err := parseSandboxCommandArgs([]string{"--sandbox", "read-only", "--no-network", "-C", "subdir", "--", "git", "status", "--short"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.mode != "read-only" || opts.network == nil || *opts.network || opts.workingDir != "subdir" || strings.Join(opts.command, " ") != "git status --short" {
		t.Fatalf("options = %#v", opts)
	}
	for _, args := range [][]string{{}, {"--sandbox"}, {"--sandbox", "invalid", "echo"}, {"--unknown", "echo"}} {
		if _, err := parseSandboxCommandArgs(args); err == nil {
			t.Fatalf("args %#v accepted", args)
		}
	}
}

func TestParseSandboxCommandArgsAcceptsEqualsSyntax(t *testing.T) {
	opts, err := parseSandboxCommandArgs([]string{"--sandbox=workspace-write", "--cd=subdir", "--", "echo", "--sandbox=tool-value"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.mode != "workspace-write" || opts.workingDir != "subdir" || strings.Join(opts.command, " ") != "echo --sandbox=tool-value" {
		t.Fatalf("options=%#v", opts)
	}
}

func TestSandboxCommandPassesStreamsAndWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("sandbox_mode = \"danger-full-access\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEWORLD_HOME", home)
	t.Setenv("GO_WANT_SANDBOX_HELPER_PROCESS", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	err = runSandboxCommand(context.Background(), strings.NewReader("from stdin"), &out, &stderr, root, globalOptions{}, []string{
		"-C", "subdir", "--", executable, "-test.run=TestSandboxHelperProcess", "--",
	})
	if err != nil {
		t.Fatalf("runSandboxCommand: %v\nstderr: %s", err, stderr.String())
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stdin=from stdin") || !strings.Contains(out.String(), "cwd="+filepath.Join(canonicalRoot, "subdir")) {
		t.Fatalf("stdout = %q", out.String())
	}
	if stderr.String() != "helper stderr\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSandboxCommandRejectsEscapingWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("sandbox_mode = \"danger-full-access\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEWORLD_HOME", home)
	err := runSandboxCommand(context.Background(), strings.NewReader(""), io.Discard, io.Discard, root, globalOptions{}, []string{"-C", "..", "--", "echo"})
	if err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("err = %v", err)
	}
}

func TestSandboxCommandRejectsMisleadingFullAccessNetworkFlag(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("sandbox_mode = \"danger-full-access\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEWORLD_HOME", home)
	err := runSandboxCommand(context.Background(), strings.NewReader(""), io.Discard, io.Discard, root, globalOptions{}, []string{"--no-network", "--", "echo"})
	if err == nil || !strings.Contains(err.Error(), "cannot restrict") {
		t.Fatalf("err = %v", err)
	}
}

func TestSandboxHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_SANDBOX_HELPER_PROCESS") != "1" {
		return
	}
	data, _ := io.ReadAll(os.Stdin)
	cwd, _ := os.Getwd()
	_, _ = os.Stdout.WriteString("stdin=" + string(data) + "\ncwd=" + cwd + "\n")
	_, _ = os.Stderr.WriteString("helper stderr\n")
	os.Exit(0)
}
