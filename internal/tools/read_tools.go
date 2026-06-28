package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/workspace"
)

const (
	defaultReadLimit   = int64(20000)
	listDirEntryLimit  = 1000
	searchMatchLimit   = 100
	searchFileLimit    = 100
	searchByteLimit    = int64(20000)
	searchDirLimit     = 100
	searchEntryLimit   = 256
	searchDirBatchSize = 128
)

type listDirTool struct {
	workspace workspace.Workspace
}

type pathArgs struct {
	Path string `json:"path"`
}

type readFileArgs struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
	Limit  *int64 `json:"limit"`
}

type searchArgs struct {
	Query string `json:"query"`
	Path  string `json:"path"`
}

func NewListDirTool(ws workspace.Workspace) Tool {
	return listDirTool{workspace: ws}
}

func (t listDirTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "list_dir",
		Description: "List entries in a workspace directory.",
		InputSchema: objectSchema(map[string]any{
			"path": map[string]any{"type": "string"},
		}, []string{"path"}),
	}
}

func (t listDirTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parsePathArgs(args, ".")
	if err != nil {
		return permissions.Request{}, err
	}
	return readRequest("list_dir", parsed.Path), nil
}

func (t listDirTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	parsed, err := parsePathArgs(args, ".")
	if err != nil {
		return Result{}, err
	}
	inputPath := parsed.Path
	resolved, err := t.workspace.Resolve(inputPath)
	if err != nil {
		return Result{}, err
	}
	dir, err := os.Open(resolved)
	if err != nil {
		return Result{}, err
	}
	defer dir.Close()

	names := make([]string, 0)
	truncated := false
	for {
		entries, err := dir.ReadDir(128)
		if err != nil && err != io.EOF {
			return Result{}, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}
			names = append(names, name)
			if len(names) >= listDirEntryLimit {
				truncated = true
				break
			}
		}
		if truncated || err == io.EOF {
			break
		}
	}
	sort.Strings(names)
	content, outputTruncated := joinCapped(names, defaultReadLimit)
	truncated = truncated || outputTruncated

	rel, err := t.workspace.Rel(resolved)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Content: content,
		Metadata: map[string]any{
			"path":      rel,
			"entries":   len(names),
			"truncated": truncated,
		},
	}, nil
}

type readFileTool struct {
	workspace workspace.Workspace
}

func NewReadFileTool(ws workspace.Workspace) Tool {
	return readFileTool{workspace: ws}
}

func (t readFileTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "read_file",
		Description: "Read a byte range from a workspace file.",
		InputSchema: objectSchema(map[string]any{
			"path":   map[string]any{"type": "string"},
			"offset": map[string]any{"type": "integer", "minimum": 0},
			"limit":  map[string]any{"type": "integer", "minimum": 1},
		}, []string{"path"}),
	}
}

func (t readFileTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseReadFileArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	if parsed.Path == "" {
		parsed.Path = "."
	}
	return readRequest("read_file", parsed.Path), nil
}

func (t readFileTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	parsed, err := parseReadFileArgs(args)
	if err != nil {
		return Result{}, err
	}
	inputPath := parsed.Path
	if inputPath == "" {
		return Result{}, fmt.Errorf("path is required")
	}
	offset := parsed.Offset
	if offset < 0 {
		return Result{}, fmt.Errorf("offset must be non-negative")
	}
	limit := defaultReadLimit
	if parsed.Limit != nil {
		limit = *parsed.Limit
	}
	if limit <= 0 {
		return Result{}, fmt.Errorf("limit must be positive")
	}
	if limit > defaultReadLimit {
		limit = defaultReadLimit
	}

	resolved, err := t.workspace.Resolve(inputPath)
	if err != nil {
		return Result{}, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return Result{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return Result{}, err
	}
	if info.IsDir() {
		return Result{}, fmt.Errorf("%q is a directory", inputPath)
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return Result{}, err
	}

	var data bytes.Buffer
	n, err := io.CopyN(&data, file, limit)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return Result{}, err
	}

	rel, err := t.workspace.Rel(resolved)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Content: data.String(),
		Metadata: map[string]any{
			"path":      rel,
			"offset":    offset,
			"limit":     limit,
			"truncated": offset+int64(n) < info.Size(),
		},
	}, nil
}

type searchTool struct {
	workspace workspace.Workspace
}

func NewSearchTool(ws workspace.Workspace) Tool {
	return searchTool{workspace: ws}
}

func (t searchTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "search",
		Description: "Search workspace text files for a literal query.",
		InputSchema: objectSchema(map[string]any{
			"query": map[string]any{"type": "string"},
			"path":  map[string]any{"type": "string"},
		}, []string{"query"}),
	}
}

func (t searchTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseSearchArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	if parsed.Path == "" {
		parsed.Path = "."
	}
	return readRequest("search", parsed.Path), nil
}

func (t searchTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	parsed, err := parseSearchArgs(args)
	if err != nil {
		return Result{}, err
	}
	query := parsed.Query
	if query == "" {
		return Result{}, fmt.Errorf("query is required")
	}
	inputPath := parsed.Path
	if inputPath == "" {
		inputPath = "."
	}
	start, err := t.workspace.Resolve(inputPath)
	if err != nil {
		return Result{}, err
	}

	matches := make([]string, 0)
	truncated := false
	readTruncated := false
	searchTruncated := false
	scannedFiles := 0
	scannedBytes := int64(0)
	scannedDirs := 0
	scannedEntries := 0

	processFile := func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(matches) >= searchMatchLimit {
			truncated = true
			return nil
		}
		if scannedFiles >= searchFileLimit || scannedBytes >= searchByteLimit {
			searchTruncated = true
			return nil
		}

		resolved, err := t.workspace.Resolve(path)
		if err != nil {
			return err
		}
		remainingBytes := searchByteLimit - scannedBytes
		fileResult, err := searchFile(t.workspace, resolved, query, searchMatchLimit-len(matches), remainingBytes)
		if err != nil {
			return err
		}
		scannedFiles++
		scannedBytes += fileResult.scannedBytes
		readTruncated = readTruncated || fileResult.readTruncated
		matches = append(matches, fileResult.matches...)
		if len(matches) >= searchMatchLimit {
			truncated = true
		}
		return nil
	}

	info, err := os.Stat(start)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		scannedEntries = 1
		if err := processFile(start); err != nil {
			return Result{}, err
		}
	} else {
		dirs := []string{start}
		stopTraversal := false
		for len(dirs) > 0 && !stopTraversal {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			if scannedDirs >= searchDirLimit {
				searchTruncated = true
				break
			}

			dirPath := dirs[0]
			dirs = dirs[1:]
			scannedDirs++

			dir, err := os.Open(dirPath)
			if err != nil {
				return Result{}, err
			}
			for !stopTraversal {
				entries, err := dir.ReadDir(searchDirBatchSize)
				if err != nil && err != io.EOF {
					_ = dir.Close()
					return Result{}, err
				}
				sort.Slice(entries, func(i, j int) bool {
					return entries[i].Name() < entries[j].Name()
				})

				for _, entry := range entries {
					if scannedEntries >= searchEntryLimit {
						searchTruncated = true
						stopTraversal = true
						break
					}
					scannedEntries++

					child := filepath.Join(dirPath, entry.Name())
					resolved, err := t.workspace.Resolve(child)
					if err != nil {
						_ = dir.Close()
						return Result{}, err
					}
					if entry.IsDir() {
						if !isHeavyDir(entry.Name()) {
							dirs = append(dirs, resolved)
						}
						continue
					}

					if err := processFile(resolved); err != nil {
						_ = dir.Close()
						return Result{}, err
					}
					if truncated || searchTruncated {
						stopTraversal = true
						break
					}
				}

				if err == io.EOF {
					break
				}
			}
			if err := dir.Close(); err != nil {
				return Result{}, err
			}
		}
	}

	rel, err := t.workspace.Rel(start)
	if err != nil {
		return Result{}, err
	}
	content := strings.Join(matches, "\n")
	outputTruncated := false
	if int64(len(content)) > defaultReadLimit {
		content = content[:int(defaultReadLimit)]
		outputTruncated = true
	}

	return Result{
		Content: content,
		Metadata: map[string]any{
			"path":             rel,
			"query":            query,
			"matches":          len(matches),
			"truncated":        truncated || outputTruncated,
			"read_truncated":   readTruncated,
			"output_truncated": outputTruncated,
			"search_truncated": searchTruncated,
			"scanned_files":    scannedFiles,
			"scanned_bytes":    scannedBytes,
			"scanned_dirs":     scannedDirs,
			"scanned_entries":  scannedEntries,
		},
	}, nil
}

type searchFileResult struct {
	matches       []string
	readTruncated bool
	scannedBytes  int64
}

func searchFile(ws workspace.Workspace, path string, query string, remaining int, byteLimit int64) (searchFileResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return searchFileResult{}, err
	}
	defer file.Close()

	rel, err := ws.Rel(path)
	if err != nil {
		return searchFileResult{}, err
	}

	var matches []string
	if byteLimit > defaultReadLimit {
		byteLimit = defaultReadLimit
	}
	reader := bufio.NewReader(io.LimitReader(file, byteLimit+1))
	lineNumber := 0
	readBytes := int64(0)
	for remaining > 0 {
		lineBytes, err := reader.ReadBytes('\n')
		if len(lineBytes) == 0 && err == io.EOF {
			break
		}
		if err != nil && err != io.EOF {
			return searchFileResult{}, err
		}
		if bytes.Contains(lineBytes, []byte{0}) {
			return searchFileResult{readTruncated: readBytes > byteLimit, scannedBytes: minInt64(readBytes, byteLimit)}, nil
		}

		readBytes += int64(len(lineBytes))
		if readBytes > byteLimit {
			overage := readBytes - byteLimit
			lineBytes = lineBytes[:len(lineBytes)-int(overage)]
		}
		lineNumber++
		line := strings.TrimRight(string(lineBytes), "\r\n")
		if strings.Contains(line, query) {
			matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, lineNumber, line))
			if len(matches) >= remaining {
				break
			}
		}
		if err == io.EOF {
			break
		}
	}
	return searchFileResult{
		matches:       matches,
		readTruncated: readBytes > byteLimit,
		scannedBytes:  minInt64(readBytes, byteLimit),
	}, nil
}

func joinCapped(items []string, limit int64) (string, bool) {
	var builder strings.Builder
	truncated := false
	for i, item := range items {
		if i > 0 {
			if int64(builder.Len()+1) > limit {
				truncated = true
				break
			}
			builder.WriteByte('\n')
		}
		if int64(builder.Len()+len(item)) > limit {
			remaining := int(limit) - builder.Len()
			if remaining > 0 {
				builder.WriteString(item[:remaining])
			}
			truncated = true
			break
		}
		builder.WriteString(item)
	}
	return builder.String(), truncated
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func readRequest(reason string, target string) permissions.Request {
	return permissions.Request{
		Action: permissions.ActionRead,
		Target: target,
		Risk:   permissions.RiskRead,
		Reason: reason,
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func parsePathArgs(args json.RawMessage, defaultPath string) (pathArgs, error) {
	var parsed pathArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return pathArgs{}, err
	}
	if parsed.Path == "" {
		parsed.Path = defaultPath
	}
	return parsed, nil
}

func parseReadFileArgs(args json.RawMessage) (readFileArgs, error) {
	var parsed readFileArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return readFileArgs{}, err
	}
	return parsed, nil
}

func parseSearchArgs(args json.RawMessage) (searchArgs, error) {
	var parsed searchArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return searchArgs{}, err
	}
	return parsed, nil
}

func decodeArgs(args json.RawMessage, target any) error {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid tool arguments: multiple JSON values")
		}
		return fmt.Errorf("invalid tool arguments: multiple JSON values")
	}
	return nil
}

func isHeavyDir(name string) bool {
	switch name {
	case ".git", ".codeworld", "node_modules", "vendor", "dist", "build", "target", ".cache":
		return true
	default:
		return false
	}
}
