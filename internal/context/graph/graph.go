package graph

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"codeworld/internal/context/indexer"
)

type Options struct {
	MaxFileBytes int64
	Summary      string
	Skills       []Snippet
	MCPResources []Snippet
	Mentions     []string
}

type Snippet struct {
	Name string
	Text string
}

type Graph struct {
	Root         string
	Files        []File
	Summary      string
	Skills       []Snippet
	MCPResources []Snippet
	Mentions     []string
}

type File struct {
	Path     string
	Language string
	Size     int64
	Changed  bool
	Symbols  []Symbol
	Content  string
}

type Symbol struct {
	Kind string
	Name string
	Line int
}

type SearchResult struct {
	Path    string
	Kind    string
	Name    string
	Line    int
	Score   int
	Preview string
}

// Build 构建运行所需的数据结构，并在过程中收集必要的上下文。
func Build(root string, opts Options) (Graph, error) {
	idx, err := indexer.Build(root, opts.MaxFileBytes)
	if err != nil {
		return Graph{}, err
	}
	changed := gitChanged(idx.Root)
	files := make([]File, 0, len(idx.Entries))
	for _, entry := range idx.Entries {
		file := File{
			Path:     entry.Path,
			Language: entry.Language,
			Size:     entry.Size,
			Changed:  changed[entry.Path],
		}
		if !entry.Skipped {
			// 只读取索引允许的小文件；大文件留在索引摘要里，避免上下文图意外吃满内存。
			data, err := os.ReadFile(filepath.Join(idx.Root, filepath.FromSlash(entry.Path)))
			if err == nil {
				file.Content = string(data)
				if entry.Language == "go" {
					file.Symbols = parseGoSymbols(entry.Path, data)
				}
			}
		}
		files = append(files, file)
	}
	return Graph{Root: idx.Root, Files: files, Summary: opts.Summary, Skills: opts.Skills, MCPResources: opts.MCPResources, Mentions: opts.Mentions}, nil
}

// File 提供对外可复用的能力，并隐藏内部实现细节。
func (g Graph) File(path string) (File, bool) {
	path = filepath.ToSlash(path)
	for _, file := range g.Files {
		if file.Path == path {
			return file, true
		}
	}
	return File{}, false
}

// Search 在受控范围内搜索数据，并返回截断后的结果集。
func (g Graph) Search(query string, limit int) []SearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	// 搜索分数偏向精确路径和符号命中，其次才是正文片段，适合 agent 快速定位代码入口。
	var results []SearchResult
	for _, file := range g.Files {
		score := 0
		preview := ""
		pathLower := strings.ToLower(file.Path)
		switch {
		case pathLower == query:
			score += 100
			preview = file.Path
		case strings.Contains(pathLower, query):
			score += 40
			preview = file.Path
		}
		for _, symbol := range file.Symbols {
			nameLower := strings.ToLower(symbol.Name)
			if nameLower == query {
				results = append(results, SearchResult{Path: file.Path, Kind: symbol.Kind, Name: symbol.Name, Line: symbol.Line, Score: 120, Preview: symbol.Kind + " " + symbol.Name})
				continue
			}
			if strings.Contains(nameLower, query) {
				results = append(results, SearchResult{Path: file.Path, Kind: symbol.Kind, Name: symbol.Name, Line: symbol.Line, Score: 80, Preview: symbol.Kind + " " + symbol.Name})
			}
		}
		if idx := strings.Index(strings.ToLower(file.Content), query); idx >= 0 {
			score += 20
			preview = previewAround(file.Content, idx)
		}
		if score > 0 {
			results = append(results, SearchResult{Path: file.Path, Kind: "file", Score: score, Preview: preview})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Path < results[j].Path
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		return results[:limit]
	}
	return results
}

// parseGoSymbols 解析输入数据，并执行必要的格式校验。
func parseGoSymbols(path string, data []byte) []Symbol {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, data, 0)
	if err != nil {
		return nil
	}
	var symbols []Symbol
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			symbols = append(symbols, Symbol{Kind: "func", Name: decl.Name.Name, Line: fset.Position(decl.Pos()).Line})
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					symbols = append(symbols, Symbol{Kind: "type", Name: spec.Name.Name, Line: fset.Position(spec.Pos()).Line})
				case *ast.ValueSpec:
					kind := strings.ToLower(decl.Tok.String())
					for _, name := range spec.Names {
						symbols = append(symbols, Symbol{Kind: kind, Name: name.Name, Line: fset.Position(name.Pos()).Line})
					}
				}
			}
		}
	}
	return symbols
}

// gitChanged 封装局部逻辑，保持调用方流程清晰。
func gitChanged(root string) map[string]bool {
	changed := map[string]bool{}
	cmd := exec.Command("git", "-C", root, "status", "--short")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return changed
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if before, after, ok := strings.Cut(path, " -> "); ok {
			_ = before
			path = after
		}
		changed[filepath.ToSlash(path)] = true
	}
	return changed
}

// previewAround 封装局部逻辑，保持调用方流程清晰。
func previewAround(content string, idx int) string {
	start := idx - 40
	if start < 0 {
		start = 0
	}
	end := idx + 80
	if end > len(content) {
		end = len(content)
	}
	return strings.TrimSpace(content[start:end])
}

// SummaryText 提供对外可复用的能力，并隐藏内部实现细节。
func (g Graph) SummaryText(limit int) string {
	if limit <= 0 {
		limit = 80
	}
	lines := make([]string, 0, len(g.Files))
	for _, file := range g.Files {
		if len(lines) >= limit {
			lines = append(lines, "[truncated]")
			break
		}
		changed := ""
		if file.Changed {
			changed = " changed"
		}
		lines = append(lines, fmt.Sprintf("%s %s symbols=%d%s", file.Path, file.Language, len(file.Symbols), changed))
	}
	return strings.Join(lines, "\n")
}
