package workspace

import (
	"container/heap"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Workspace struct {
	Root string
}

// New 创建并返回对应组件，集中设置默认依赖和初始状态。
func New(root string) (Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Workspace{}, err
	}
	canonicalRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{Root: filepath.Clean(canonicalRoot)}, nil
}

// Resolve 提供对外可复用的能力，并隐藏内部实现细节。
func (w Workspace) Resolve(path string) (string, error) {
	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Clean(filepath.Join(w.Root, path))
	}

	canonical, err := w.canonicalizeForSafety(resolved)
	if err != nil {
		return "", err
	}
	if !w.contains(canonical) {
		return "", fmt.Errorf("path %q escapes workspace %q", path, w.Root)
	}
	return canonical, nil
}

// Rel 提供对外可复用的能力，并隐藏内部实现细节。
func (w Workspace) Rel(path string) (string, error) {
	resolved, err := w.Resolve(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(w.Root, resolved)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// IsGitRepo 判断输入是否满足特定条件，并用于后续分支决策。
func (w Workspace) IsGitRepo() bool {
	cmd := exec.Command("git", "-C", w.Root, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(out)) == "true" {
		return true
	}

	info, err := os.Stat(filepath.Join(w.Root, ".git"))
	if err != nil {
		return false
	}
	if info.IsDir() {
		return true
	}
	if !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(filepath.Join(w.Root, ".git"))
	return err == nil && strings.HasPrefix(strings.TrimSpace(string(data)), "gitdir:")
}

// Summary 提供对外可复用的能力，并隐藏内部实现细节。
func (w Workspace) Summary(limit int) (string, error) {
	if limit <= 0 {
		limit = 100
	}

	items, err := summaryEntries(w.Root, "")
	if err != nil {
		return "", err
	}
	frontier := summaryHeap(items)
	heap.Init(&frontier)

	var files []string
	for frontier.Len() > 0 && len(files) <= limit {
		item := heap.Pop(&frontier).(summaryItem)
		if item.isDir {
			children, err := summaryEntries(item.path, item.rel)
			if err != nil {
				return "", err
			}
			for _, child := range children {
				heap.Push(&frontier, child)
			}
			continue
		}

		if _, err := w.Resolve(item.path); err != nil {
			return "", err
		}
		files = append(files, item.rel)
	}

	if len(files) > limit {
		files = files[:limit]
		files = append(files, "[truncated]")
	}
	return strings.Join(files, "\n"), nil
}

// contains 判断输入是否满足特定条件，并用于后续分支决策。
func (w Workspace) contains(path string) bool {
	rel, err := filepath.Rel(w.Root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// canonicalizeForSafety 封装局部逻辑，保持调用方流程清晰。
func (w Workspace) canonicalizeForSafety(path string) (string, error) {
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(w.Root, clean)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		clean, err = canonicalizeExistingPrefix(clean)
		if err != nil {
			return "", err
		}
		rel, err = filepath.Rel(w.Root, clean)
		if err != nil {
			return "", err
		}
	}
	if rel == "." {
		return w.Root, nil
	}

	// 逐段解析路径并在遇到符号链接时重新校验边界，防止通过 symlink 跳出工作区。
	current := w.Root
	components := strings.Split(rel, string(filepath.Separator))
	for i, component := range components {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			return "", fmt.Errorf("path %q escapes workspace %q", path, w.Root)
		}

		next := filepath.Join(current, component)
		info, err := os.Lstat(next)
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.Clean(filepath.Join(append([]string{current}, components[i:]...)...)), nil
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			evaluated, err := filepath.EvalSymlinks(next)
			if err != nil {
				return "", err
			}
			current = filepath.Clean(evaluated)
			if !w.contains(current) {
				return "", fmt.Errorf("path %q escapes workspace %q", path, w.Root)
			}
			continue
		}
		current = next
	}
	return filepath.Clean(current), nil
}

// canonicalizeExistingPrefix 封装局部逻辑，保持调用方流程清晰。
func canonicalizeExistingPrefix(path string) (string, error) {
	evaluated, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(evaluated), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	parent := filepath.Clean(path)
	var missing []string
	// 新文件可能还不存在；只解析已存在的父目录，再把缺失路径安全拼回去。
	for {
		missing = append(missing, filepath.Base(parent))
		next := filepath.Dir(parent)
		if next == parent {
			return "", err
		}
		parent = next

		info, statErr := os.Stat(parent)
		if statErr == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("path parent %q is not a directory", parent)
			}
			evaluated, evalErr := filepath.EvalSymlinks(parent)
			if evalErr != nil {
				return "", evalErr
			}
			for i := len(missing) - 1; i >= 0; i-- {
				evaluated = filepath.Join(evaluated, missing[i])
			}
			return filepath.Clean(evaluated), nil
		}
		if !os.IsNotExist(statErr) {
			return "", statErr
		}
	}
}

type summaryItem struct {
	rel   string
	path  string
	isDir bool
}

type summaryHeap []summaryItem

// Len 提供对外可复用的能力，并隐藏内部实现细节。
func (h summaryHeap) Len() int {
	return len(h)
}

// Less 提供对外可复用的能力，并隐藏内部实现细节。
func (h summaryHeap) Less(i, j int) bool {
	return h[i].rel < h[j].rel
}

// Swap 提供对外可复用的能力，并隐藏内部实现细节。
func (h summaryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Push 提供对外可复用的能力，并隐藏内部实现细节。
func (h *summaryHeap) Push(x any) {
	*h = append(*h, x.(summaryItem))
}

// Pop 提供对外可复用的能力，并隐藏内部实现细节。
func (h *summaryHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// summaryEntries 封装局部逻辑，保持调用方流程清晰。
func summaryEntries(path string, parentRel string) ([]summaryItem, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	items := make([]summaryItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && isHeavyDir(entry.Name()) {
			continue
		}

		rel := entry.Name()
		if parentRel != "" {
			rel = parentRel + "/" + entry.Name()
		}
		items = append(items, summaryItem{
			rel:   rel,
			path:  filepath.Join(path, entry.Name()),
			isDir: entry.IsDir(),
		})
	}
	return items, nil
}

// isHeavyDir 判断输入是否满足特定条件，并用于后续分支决策。
func isHeavyDir(name string) bool {
	switch name {
	case ".git", ".codeworld", "node_modules", "vendor", "dist", "build", "target", ".cache":
		return true
	default:
		return false
	}
}
