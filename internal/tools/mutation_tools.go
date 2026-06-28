package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

type writeFileTool struct {
	workspace workspace.Workspace
}

type writeFileArgs struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
	Reason  string  `json:"reason"`
}

func NewWriteFileTool(ws workspace.Workspace) Tool {
	return writeFileTool{workspace: ws}
}

func (t writeFileTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "write_file",
		Description: "Write content to a file in the workspace, creating parent directories as needed.",
		InputSchema: objectSchema(map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
			"reason":  map[string]any{"type": "string"},
		}, []string{"path", "content"}),
	}
}

func (t writeFileTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseWriteFileArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	if parsed.Path == "" {
		return permissions.Request{}, fmt.Errorf("path is required")
	}
	if parsed.Content == nil {
		return permissions.Request{}, fmt.Errorf("content is required")
	}
	if _, err := t.workspace.Resolve(parsed.Path); err != nil {
		return permissions.Request{}, err
	}
	reason := parsed.Reason
	if reason == "" {
		reason = "write_file"
	}
	return permissions.Request{
		Action: permissions.ActionWrite,
		Target: parsed.Path,
		Risk:   permissions.RiskWrite,
		Reason: reason,
	}, nil
}

func (t writeFileTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	parsed, err := parseWriteFileArgs(args)
	if err != nil {
		return Result{}, err
	}
	if parsed.Path == "" {
		return Result{}, fmt.Errorf("path is required")
	}
	if parsed.Content == nil {
		return Result{}, fmt.Errorf("content is required")
	}
	resolved, err := t.workspace.Resolve(parsed.Path)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(resolved, []byte(*parsed.Content), 0o644); err != nil {
		return Result{}, err
	}
	rel, err := t.workspace.Rel(resolved)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Content: "",
		Metadata: map[string]any{
			"path":  rel,
			"bytes": len(*parsed.Content),
		},
	}, nil
}

type applyPatchTool struct {
	workspace workspace.Workspace
}

type applyPatchArgs struct {
	Patch  string `json:"patch"`
	Reason string `json:"reason"`
}

func NewApplyPatchTool(ws workspace.Workspace) Tool {
	return applyPatchTool{workspace: ws}
}

func (t applyPatchTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "apply_patch",
		Description: "Apply a unified diff patch in the workspace with git apply.",
		InputSchema: objectSchema(map[string]any{
			"patch":  map[string]any{"type": "string"},
			"reason": map[string]any{"type": "string"},
		}, []string{"patch"}),
	}
}

func (t applyPatchTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseApplyPatchArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	if parsed.Patch == "" {
		return permissions.Request{}, fmt.Errorf("patch is required")
	}
	if err := validatePatchPaths(t.workspace, parsed.Patch); err != nil {
		return permissions.Request{}, err
	}
	reason := parsed.Reason
	if reason == "" {
		reason = "apply_patch"
	}
	return permissions.Request{
		Action:  permissions.ActionWrite,
		Target:  "patch",
		Risk:    permissions.RiskWrite,
		Reason:  reason,
		Preview: preview(parsed.Patch, defaultReadLimit),
	}, nil
}

func (t applyPatchTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	parsed, err := parseApplyPatchArgs(args)
	if err != nil {
		return Result{}, err
	}
	if parsed.Patch == "" {
		return Result{}, fmt.Errorf("patch is required")
	}
	if err := validatePatchPaths(t.workspace, parsed.Patch); err != nil {
		return Result{}, err
	}

	if result, err := runGitApply(ctx, t.workspace, parsed.Patch, true); err != nil {
		return result, err
	}
	return runGitApply(ctx, t.workspace, parsed.Patch, false)
}

func runGitApply(ctx context.Context, ws workspace.Workspace, patch string, check bool) (Result, error) {
	args := []string{"-C", ws.Root, "apply"}
	command := "git apply"
	if check {
		args = append(args, "--check")
		command = "git apply --check"
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdin = strings.NewReader(patch)
	stdout := &cappedBuffer{limit: defaultReadLimit}
	stderr := &cappedBuffer{limit: defaultReadLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	content, truncated := combineCommandOutput(stdout, stderr, defaultReadLimit)

	result := Result{
		Content: content,
		Metadata: map[string]any{
			"command":          command,
			"exit_code":        exitCode(err),
			"truncated":        truncated,
			"stdout_truncated": stdout.truncated(),
			"stderr_truncated": stderr.truncated(),
		},
	}
	return result, err
}

func parseWriteFileArgs(args json.RawMessage) (writeFileArgs, error) {
	var parsed writeFileArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return writeFileArgs{}, err
	}
	return parsed, nil
}

func parseApplyPatchArgs(args json.RawMessage) (applyPatchArgs, error) {
	var parsed applyPatchArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return applyPatchArgs{}, err
	}
	return parsed, nil
}

func validatePatchPaths(ws workspace.Workspace, patch string) error {
	hunk := hunkState{}
	for _, line := range strings.Split(patch, "\n") {
		if hunk.active {
			hunk.consume(line)
			continue
		}
		if strings.HasPrefix(line, "diff --git ") {
			paths, err := patchHeaderPathTokens(strings.TrimPrefix(line, "diff --git "))
			if err != nil {
				return err
			}
			if len(paths) < 2 {
				return fmt.Errorf("diff --git header must include at least two paths")
			}
			for _, path := range paths {
				if err := validatePatchPath(ws, path, true); err != nil {
					return err
				}
			}
			continue
		}
		if strings.HasPrefix(line, "@@") {
			parsed, err := parseHunkState(line)
			if err != nil {
				return err
			}
			hunk = parsed
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			paths, err := patchHeaderPathTokens(line[4:])
			if err != nil {
				return err
			}
			if len(paths) == 0 {
				return fmt.Errorf("patch path is empty")
			}
			for _, path := range paths {
				if err := validatePatchPath(ws, path, true); err != nil {
					return err
				}
			}
			continue
		}
		for _, prefix := range []string{"rename from ", "rename to ", "copy from ", "copy to "} {
			path, ok := strings.CutPrefix(line, prefix)
			if !ok {
				continue
			}
			if err := validatePatchPath(ws, strings.TrimSpace(path), false); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

type hunkState struct {
	active bool
	old    int
	new    int
}

func parseHunkState(line string) (hunkState, error) {
	fields := strings.Fields(line)
	var oldCount *int
	var newCount *int
	for _, field := range fields {
		if strings.HasPrefix(field, "-") && oldCount == nil {
			count, err := parseHunkRangeCount(field)
			if err != nil {
				return hunkState{}, err
			}
			oldCount = &count
			continue
		}
		if strings.HasPrefix(field, "+") && newCount == nil {
			count, err := parseHunkRangeCount(field)
			if err != nil {
				return hunkState{}, err
			}
			newCount = &count
		}
	}
	if oldCount == nil || newCount == nil {
		return hunkState{}, fmt.Errorf("invalid hunk header %q", line)
	}
	return hunkState{active: *oldCount > 0 || *newCount > 0, old: *oldCount, new: *newCount}, nil
}

func parseHunkRangeCount(field string) (int, error) {
	field = strings.TrimPrefix(field, "-")
	field = strings.TrimPrefix(field, "+")
	if field == "" {
		return 0, fmt.Errorf("invalid hunk range")
	}
	parts := strings.SplitN(field, ",", 2)
	if len(parts) == 1 {
		return 1, nil
	}
	count, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid hunk range count %q: %w", field, err)
	}
	if count < 0 {
		return 0, fmt.Errorf("invalid negative hunk range count %q", field)
	}
	return count, nil
}

func (h *hunkState) consume(line string) {
	if line == "" {
		return
	}
	switch line[0] {
	case ' ':
		if h.old > 0 {
			h.old--
		}
		if h.new > 0 {
			h.new--
		}
	case '-':
		if h.old > 0 {
			h.old--
		}
	case '+':
		if h.new > 0 {
			h.new--
		}
	case '\\':
		// No newline marker, does not consume either side.
	}
	if h.old == 0 && h.new == 0 {
		h.active = false
	}
}

func patchHeaderPathTokens(input string) ([]string, error) {
	rest := strings.TrimSpace(input)
	var paths []string
	for rest != "" {
		path, next, err := nextPatchHeaderPathToken(rest)
		if err != nil {
			return nil, err
		}
		if path == "" {
			break
		}
		paths = append(paths, path)
		rest = strings.TrimSpace(next)
	}
	return paths, nil
}

func nextPatchHeaderPathToken(input string) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", nil
	}
	if input[0] != '"' {
		for i := 0; i < len(input); i++ {
			if isPatchHeaderSpace(input[i]) {
				return input[:i], input[i:], nil
			}
		}
		return input, "", nil
	}

	escaped := false
	for i := 1; i < len(input); i++ {
		switch {
		case escaped:
			escaped = false
		case input[i] == '\\':
			escaped = true
		case input[i] == '"':
			return input[:i+1], input[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("unterminated quoted patch path")
}

func isPatchHeaderSpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func validatePatchPath(ws workspace.Workspace, path string, stripFirstComponent bool) error {
	cleaned, err := normalizePatchPath(path, stripFirstComponent)
	if err != nil {
		return err
	}
	if cleaned == "/dev/null" {
		return nil
	}
	if cleaned == "" {
		return fmt.Errorf("patch path is empty")
	}
	if _, err := ws.Resolve(cleaned); err != nil {
		return err
	}
	return nil
}

func normalizePatchPath(path string, stripFirstComponent bool) (string, error) {
	if unquoted, err := strconv.Unquote(path); err == nil {
		path = unquoted
	}
	if path == "/dev/null" {
		return path, nil
	}
	if !stripFirstComponent {
		return path, nil
	}
	return stripGitApplyPathComponent(path), nil
}

func stripGitApplyPathComponent(path string) string {
	if idx := strings.Index(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func preview(value string, limit int64) string {
	if int64(len(value)) <= limit {
		return value
	}
	return value[:limit]
}
