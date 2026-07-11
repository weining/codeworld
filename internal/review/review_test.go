package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPromptForUncommittedChanges(t *testing.T) {
	root := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nconst value = 2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	prompt, err := BuildPrompt(context.Background(), root, Target{}, "check compatibility")
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	for _, want := range []string{"uncommitted changes", "check compatibility", "const value = 2", "Do not modify files"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPromptForBaseAndCommit(t *testing.T) {
	root := newGitRepo(t)
	base, err := git(context.Background(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nconst value = 2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit(t, root, "add", "main.go")
	runGit(t, root, "commit", "-m", "change value")
	commit, err := git(context.Background(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	basePrompt, err := BuildPrompt(context.Background(), root, Target{Base: strings.TrimSpace(base)}, "")
	if err != nil || !strings.Contains(basePrompt, "changes against base") || !strings.Contains(basePrompt, "const value = 2") {
		t.Fatalf("base prompt err=%v:\n%s", err, basePrompt)
	}
	commitPrompt, err := BuildPrompt(context.Background(), root, Target{Commit: strings.TrimSpace(commit)}, "")
	if err != nil || !strings.Contains(commitPrompt, "commit ") || !strings.Contains(commitPrompt, "change value") {
		t.Fatalf("commit prompt err=%v:\n%s", err, commitPrompt)
	}
}

func TestBuildPromptRejectsUnsafeRevision(t *testing.T) {
	_, err := BuildPrompt(context.Background(), t.TempDir(), Target{Base: "--output=/tmp/x"}, "")
	if err == nil || !strings.Contains(err.Error(), "unsafe revision") {
		t.Fatalf("err = %v, want unsafe revision", err)
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nconst value = 1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit(t, root, "add", "main.go")
	runGit(t, root, "-c", "user.name=Codeworld Tests", "-c", "user.email=tests@example.com", "commit", "-m", "initial")
	return root
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Codeworld Tests", "GIT_AUTHOR_EMAIL=tests@example.com", "GIT_COMMITTER_NAME=Codeworld Tests", "GIT_COMMITTER_EMAIL=tests@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}
