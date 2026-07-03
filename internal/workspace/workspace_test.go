package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResolveAllowsWorkspacePath 验证对应场景的行为，避免后续改动破坏既有约束。
func TestResolveAllowsWorkspacePath(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	got, err := ws.Resolve("src/main.go")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	want := filepath.Join(ws.Root, "src", "main.go")
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

// TestResolveRejectsPathEscape 验证对应场景的行为，避免后续改动破坏既有约束。
func TestResolveRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if _, err := ws.Resolve("../outside.txt"); err == nil {
		t.Fatalf("Resolve accepted path traversal")
	}
}

// TestResolveRejectsAbsolutePathOutsideWorkspaceWithSharedPrefix 验证对应场景的行为，避免后续改动破坏既有约束。
func TestResolveRejectsAbsolutePathOutsideWorkspaceWithSharedPrefix(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "work")
	outside := filepath.Join(parent, "workspace-other", "file.txt")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll root: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatalf("MkdirAll outside: %v", err)
	}

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if _, err := ws.Resolve(outside); err == nil {
		t.Fatalf("Resolve accepted absolute path outside workspace")
	}
}

// TestResolveRejectsSymlinkedFileEscapingWorkspace 验证对应场景的行为，避免后续改动破坏既有约束。
func TestResolveRejectsSymlinkedFileEscapingWorkspace(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "work")
	outside := filepath.Join(parent, "outside.txt")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("Mkdir root: %v", err)
	}
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, err := ws.Resolve("link.txt"); err == nil {
		t.Fatalf("Resolve accepted symlinked file escaping workspace")
	}
}

// TestResolveRejectsSymlinkedDirectoryEscapingWorkspace 验证对应场景的行为，避免后续改动破坏既有约束。
func TestResolveRejectsSymlinkedDirectoryEscapingWorkspace(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "work")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("Mkdir root: %v", err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatalf("Mkdir outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, err := ws.Resolve("linkdir/new.txt"); err == nil {
		t.Fatalf("Resolve accepted path created under symlinked directory escaping workspace")
	}
}

// TestResolveRejectsDanglingSymlinkLeafEscapingWorkspace 验证对应场景的行为，避免后续改动破坏既有约束。
func TestResolveRejectsDanglingSymlinkLeafEscapingWorkspace(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "work")
	outside := filepath.Join(parent, "outside", "missing.txt")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("Mkdir root: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, err := ws.Resolve("link.txt"); err == nil {
		t.Fatalf("Resolve accepted dangling symlink leaf escaping workspace")
	}
}

// TestResolveRejectsDanglingSymlinkParentEscapingWorkspace 验证对应场景的行为，避免后续改动破坏既有约束。
func TestResolveRejectsDanglingSymlinkParentEscapingWorkspace(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "work")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("Mkdir root: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, err := ws.Resolve("linkdir/new.txt"); err == nil {
		t.Fatalf("Resolve accepted child path under dangling symlink escaping workspace")
	}
}

// TestRelReturnsSlashNormalizedWorkspacePath 验证对应场景的行为，避免后续改动破坏既有约束。
func TestRelReturnsSlashNormalizedWorkspacePath(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	got, err := ws.Rel(filepath.Join(root, "dir", "file.txt"))
	if err != nil {
		t.Fatalf("Rel returned error: %v", err)
	}
	if got != "dir/file.txt" {
		t.Fatalf("Rel = %q, want dir/file.txt", got)
	}
}

// TestIsGitRepoDetectsDotGitDirectory 验证对应场景的行为，避免后续改动破坏既有约束。
func TestIsGitRepoDetectsDotGitDirectory(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if ws.IsGitRepo() {
		t.Fatalf("IsGitRepo = true before .git exists")
	}

	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir .git: %v", err)
	}
	if !ws.IsGitRepo() {
		t.Fatalf("IsGitRepo = false after .git directory exists")
	}
}

// TestIsGitRepoDetectsDotGitFile 验证对应场景的行为，避免后续改动破坏既有约束。
func TestIsGitRepoDetectsDotGitFile(t *testing.T) {
	git := requireGit(t)
	root := t.TempDir()
	gitDir := filepath.Join(t.TempDir(), "repo.git")
	runGit(t, git, "init", "--separate-git-dir", gitDir, root)

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if !ws.IsGitRepo() {
		t.Fatalf("IsGitRepo = false for worktree with .git file")
	}
}

// TestIsGitRepoDetectsSubdirectoryInsideWorktree 验证对应场景的行为，避免后续改动破坏既有约束。
func TestIsGitRepoDetectsSubdirectoryInsideWorktree(t *testing.T) {
	git := requireGit(t)
	root := t.TempDir()
	runGit(t, git, "-C", root, "init")
	subdir := filepath.Join(root, "nested")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("Mkdir subdir: %v", err)
	}

	ws, err := New(subdir)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if !ws.IsGitRepo() {
		t.Fatalf("IsGitRepo = false for subdirectory inside git worktree")
	}
}

// TestSummarySkipsHeavyDirectories 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSummarySkipsHeavyDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "readme")
	writeFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, ".git", "config"), "git")
	writeFile(t, filepath.Join(root, "node_modules", "dep", "index.js"), "dep")
	writeFile(t, filepath.Join(root, "vendor", "module.go"), "vendor")
	writeFile(t, filepath.Join(root, "dist", "bundle.js"), "dist")
	writeFile(t, filepath.Join(root, "build", "out"), "build")
	writeFile(t, filepath.Join(root, "target", "debug", "bin"), "target")
	writeFile(t, filepath.Join(root, ".cache", "entry"), "cache")
	writeFile(t, filepath.Join(root, ".codeworld", "config.toml"), "model = \"x\"\n")

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	got, err := ws.Summary(10)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}

	for _, want := range []string{"README.md", "src/main.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Summary missing %q in:\n%s", want, got)
		}
	}
	for _, skipped := range []string{".git", "node_modules", "vendor", "dist", "build", "target", ".cache", ".codeworld"} {
		if strings.Contains(got, skipped) {
			t.Fatalf("Summary included skipped directory %q in:\n%s", skipped, got)
		}
	}
}

// TestSummaryStopsAfterLimitPlusOneIncludedFiles 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSummaryStopsAfterLimitPlusOneIncludedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod unreadable directory behavior is platform-specific on windows")
	}

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "b.txt"), "b")
	unreadable := filepath.Join(root, "z_unreadable")
	if err := os.Mkdir(unreadable, 0o755); err != nil {
		t.Fatalf("Mkdir unreadable: %v", err)
	}
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatalf("Chmod unreadable: %v", err)
	}
	defer os.Chmod(unreadable, 0o755)

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	got, err := ws.Summary(1)
	if err != nil {
		t.Fatalf("Summary returned error after collecting limit+1 files: %v", err)
	}

	want := "a.txt\n[truncated]"
	if got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}

// TestSummaryLimitsAfterGlobalPathSort 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSummaryLimitsAfterGlobalPathSort(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "y.txt"), "nested")
	writeFile(t, filepath.Join(root, "a", "z.txt"), "nested")
	writeFile(t, filepath.Join(root, "a.txt"), "root")

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	got, err := ws.Summary(1)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}

	want := "a.txt\n[truncated]"
	if got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}

// TestSummarySortsFilesWithinLimit 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSummarySortsFilesWithinLimit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "c.txt"), "c")
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "b.txt"), "b")

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	got, err := ws.Summary(3)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}

	want := "a.txt\nb.txt\nc.txt"
	if got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}

// TestSummaryDefaultsNonPositiveLimitTo100 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSummaryDefaultsNonPositiveLimitTo100(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		writeFile(t, filepath.Join(root, string(rune('a'+i))+".txt"), "content")
	}

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	got, err := ws.Summary(0)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}

	want := "a.txt\nb.txt\nc.txt"
	if got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}

// TestSummaryAppendsTruncatedMarkerWhenFilesExceedLimit 验证对应场景的行为，避免后续改动破坏既有约束。
func TestSummaryAppendsTruncatedMarkerWhenFilesExceedLimit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "c.txt"), "c")
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "b.txt"), "b")

	ws, err := New(root)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	got, err := ws.Summary(2)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}

	want := "a.txt\nb.txt\n[truncated]"
	if got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}

// requireGit 是测试辅助函数，用于复用测试准备或断言逻辑。
func requireGit(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	return git
}

// runGit 是测试辅助函数，用于复用测试准备或断言逻辑。
func runGit(t *testing.T, git string, args ...string) {
	t.Helper()
	cmd := exec.Command(git, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// writeFile 是测试辅助函数，用于复用测试准备或断言逻辑。
func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
