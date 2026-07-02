package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
)

const (
	defaultWebSearchLimit   = 5
	maxWebSearchLimit       = 10
	defaultWebSearchTimeout = 10 * time.Second
	defaultWebSearchURL     = "https://lite.duckduckgo.com/lite/"
)

type webSearchTool struct {
	endpoint string
	client   *http.Client
}

type webSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type webSearchResult struct {
	Title   string
	URL     string
	Snippet string
}

func NewWebSearchTool() Tool {
	return webSearchTool{
		endpoint: defaultWebSearchURL,
		client:   &http.Client{Timeout: defaultWebSearchTimeout},
	}
}

func (t webSearchTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "web_search",
		Description: "Search the web for current external information. Requires network permission.",
		InputSchema: objectSchema(map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxWebSearchLimit},
		}, []string{"query"}),
	}
}

func (t webSearchTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseWebSearchArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{
		Action: permissions.ActionRead,
		Target: parsed.Query,
		Risk:   permissions.RiskNetwork,
		Reason: "web_search",
	}, nil
}

func (t webSearchTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	parsed, err := parseWebSearchArgs(args)
	if err != nil {
		return Result{}, err
	}
	limit := parsed.Limit
	if limit <= 0 {
		limit = defaultWebSearchLimit
	}
	if limit > maxWebSearchLimit {
		limit = maxWebSearchLimit
	}

	endpoint := t.endpoint
	if endpoint == "" {
		endpoint = defaultWebSearchURL
	}
	client := t.client
	if client == nil {
		client = &http.Client{Timeout: defaultWebSearchTimeout}
	}
	requestURL, err := webSearchURL(endpoint, parsed.Query)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "codeworld-web-search/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("web_search status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, defaultReadLimit+1))
	if err != nil {
		return Result{}, err
	}
	bodyTruncated := int64(len(data)) > defaultReadLimit
	if bodyTruncated {
		data = data[:defaultReadLimit]
	}
	results := parseWebSearchResults(string(data), limit)
	content := formatWebSearchResults(results)
	outputTruncated := false
	if int64(len(content)) > defaultReadLimit {
		content = content[:defaultReadLimit]
		outputTruncated = true
	}
	return Result{
		Content: content,
		Metadata: map[string]any{
			"query":             parsed.Query,
			"results":           len(results),
			"limit":             limit,
			"truncated":         bodyTruncated || outputTruncated,
			"body_truncated":    bodyTruncated,
			"output_truncated":  outputTruncated,
			"provider":          "duckduckgo_lite",
			"permission_reason": "network",
		},
	}, nil
}

func parseWebSearchArgs(args json.RawMessage) (webSearchArgs, error) {
	var parsed webSearchArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return webSearchArgs{}, err
	}
	parsed.Query = strings.TrimSpace(parsed.Query)
	if parsed.Query == "" {
		return webSearchArgs{}, fmt.Errorf("query is required")
	}
	if parsed.Limit < 0 {
		return webSearchArgs{}, fmt.Errorf("limit must be positive")
	}
	return parsed, nil
}

func webSearchURL(endpoint string, query string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	values := parsed.Query()
	values.Set("q", query)
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

var (
	webResultAnchorPattern  = regexp.MustCompile(`(?is)<a[^>]*class=["'][^"']*result-link[^"']*["'][^>]*>.*?</a>`)
	webResultHrefPattern    = regexp.MustCompile(`(?is)href=["']([^"']+)["']`)
	webResultContentPattern = regexp.MustCompile(`(?is)>(.*?)</a>`)
	webResultSnippetPattern = regexp.MustCompile(`(?is)<td[^>]+class=["'][^"']*result-snippet[^"']*["'][^>]*>(.*?)</td>`)
	webTagPattern           = regexp.MustCompile(`(?is)<[^>]+>`)
)

func parseWebSearchResults(body string, limit int) []webSearchResult {
	if limit <= 0 {
		limit = defaultWebSearchLimit
	}
	anchors := webResultAnchorPattern.FindAllString(body, limit)
	snippets := webResultSnippetPattern.FindAllStringSubmatch(body, limit)
	results := make([]webSearchResult, 0, len(anchors))
	for i, anchor := range anchors {
		href := webResultHrefPattern.FindStringSubmatch(anchor)
		content := webResultContentPattern.FindStringSubmatch(anchor)
		if len(href) < 2 || len(content) < 2 {
			continue
		}
		result := webSearchResult{
			Title: cleanWebSearchText(content[1]),
			URL:   decodeDuckDuckGoURL(cleanWebSearchText(href[1])),
		}
		if i < len(snippets) {
			result.Snippet = cleanWebSearchText(snippets[i][1])
		}
		if result.Title == "" || result.URL == "" {
			continue
		}
		results = append(results, result)
	}
	return results
}

func formatWebSearchResults(results []webSearchResult) string {
	if len(results) == 0 {
		return "no web search results"
	}
	var b strings.Builder
	for i, result := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(result.Title)
		b.WriteByte('\n')
		b.WriteString(result.URL)
		if result.Snippet != "" {
			b.WriteByte('\n')
			b.WriteString(result.Snippet)
		}
	}
	return b.String()
}

func cleanWebSearchText(value string) string {
	value = webTagPattern.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

func decodeDuckDuckGoURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Host == "duckduckgo.com" && strings.HasPrefix(parsed.Path, "/l/") {
		if redirected := parsed.Query().Get("uddg"); redirected != "" {
			return redirected
		}
	}
	return raw
}
