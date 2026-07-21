package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
	"codeworld/internal/workspace"
)

const (
	defaultBrowserTimeout = 30 * time.Second
	defaultBrowserWidth   = 1440
	defaultBrowserHeight  = 900
)

type browserOpenTool struct {
	workspace   workspace.Workspace
	sandbox     sandbox.Policy
	browserPath string
}

type browserOpenArgs struct {
	URL            string `json:"url"`
	ScreenshotPath string `json:"screenshot_path"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	WaitMS         int    `json:"wait_ms"`
}

// NewBrowserOpenTool creates a stateless headless-browser tool backed by a local Chrome installation.
func NewBrowserOpenTool(ws workspace.Workspace, policy sandbox.Policy) Tool {
	return browserOpenTool{workspace: ws, sandbox: policy}
}

func (t browserOpenTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{
		Name:        "browser_open",
		Description: "Open an HTTP(S) page in a local headless Chrome, return rendered page text, and save a PNG screenshot inside the workspace.",
		InputSchema: objectSchema(map[string]any{
			"url":             map[string]any{"type": "string"},
			"screenshot_path": map[string]any{"type": "string", "description": "Workspace-relative output path ending in .png."},
			"width":           map[string]any{"type": "integer", "minimum": 320, "maximum": 3840},
			"height":          map[string]any{"type": "integer", "minimum": 240, "maximum": 2160},
			"wait_ms":         map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
		}, []string{"url", "screenshot_path"}),
	}
}

func (t browserOpenTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, _, err := t.parseArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{
		Action:  permissions.ActionWrite,
		Target:  parsed.URL + " -> " + parsed.ScreenshotPath,
		Risk:    permissions.RiskNetwork,
		Reason:  "browser_open",
		Preview: parsed.ScreenshotPath,
	}, nil
}

func (t browserOpenTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	parsed, screenshotPath, err := t.parseArgs(args)
	if err != nil {
		return Result{}, err
	}
	browserPath := t.browserPath
	if browserPath == "" {
		browserPath, err = findChrome()
		if err != nil {
			return Result{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(screenshotPath), 0o755); err != nil {
		return Result{}, err
	}
	profileDir, err := os.MkdirTemp(t.workspace.Root, ".codeworld-browser-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(profileDir)

	runCtx, cancel := context.WithTimeout(ctx, defaultBrowserTimeout)
	defer cancel()
	chromeArgs := []string{
		"--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--user-data-dir=" + profileDir,
		"--window-size=" + strconv.Itoa(parsed.Width) + "," + strconv.Itoa(parsed.Height),
		"--virtual-time-budget=" + strconv.Itoa(parsed.WaitMS),
		"--screenshot=" + screenshotPath,
		"--dump-dom", parsed.URL,
	}
	cmd, err := t.sandbox.CommandContext(runCtx, t.workspace.Root, browserPath, chromeArgs...)
	if err != nil {
		return Result{}, err
	}
	cmd.Env = append(os.Environ(), "TMPDIR="+profileDir)
	stdout := &cappedBuffer{limit: defaultReadLimit * 2}
	stderr := &cappedBuffer{limit: defaultReadLimit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return Result{}, fmt.Errorf("browser_open timed out after %s", defaultBrowserTimeout)
		}
		return Result{}, fmt.Errorf("browser_open: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if _, err := os.Stat(screenshotPath); err != nil {
		return Result{}, fmt.Errorf("browser_open did not create screenshot: %w", err)
	}
	text := renderedPageText(stdout.String())
	truncated := false
	if int64(len(text)) > defaultReadLimit {
		text = text[:defaultReadLimit]
		truncated = true
	}
	rel, _ := t.workspace.Rel(screenshotPath)
	return Result{Content: text, Metadata: map[string]any{
		"url": parsed.URL, "screenshot_path": rel, "width": parsed.Width, "height": parsed.Height,
		"wait_ms": parsed.WaitMS, "truncated": truncated,
	}}, nil
}

func (t browserOpenTool) parseArgs(args json.RawMessage) (browserOpenArgs, string, error) {
	var parsed browserOpenArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return parsed, "", err
	}
	parsed.URL = strings.TrimSpace(parsed.URL)
	parsed.ScreenshotPath = strings.TrimSpace(parsed.ScreenshotPath)
	if !strings.HasPrefix(parsed.URL, "http://") && !strings.HasPrefix(parsed.URL, "https://") {
		return parsed, "", fmt.Errorf("url must use http or https")
	}
	if parsed.ScreenshotPath == "" || strings.ToLower(filepath.Ext(parsed.ScreenshotPath)) != ".png" {
		return parsed, "", fmt.Errorf("screenshot_path must be a workspace PNG path")
	}
	resolved, err := t.workspace.Resolve(parsed.ScreenshotPath)
	if err != nil {
		return parsed, "", err
	}
	if parsed.Width == 0 {
		parsed.Width = defaultBrowserWidth
	}
	if parsed.Height == 0 {
		parsed.Height = defaultBrowserHeight
	}
	if parsed.Width < 320 || parsed.Width > 3840 || parsed.Height < 240 || parsed.Height > 2160 {
		return parsed, "", fmt.Errorf("browser viewport is out of range")
	}
	if parsed.WaitMS < 0 || parsed.WaitMS > 10000 {
		return parsed, "", fmt.Errorf("wait_ms must be between 0 and 10000")
	}
	return parsed, resolved, nil
}

func findChrome() (string, error) {
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"google-chrome", "chromium", "chromium-browser",
	}
	for _, candidate := range candidates {
		if filepath.IsAbs(candidate) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("browser_open requires Google Chrome or Chromium")
}

var (
	browserScriptPattern = regexp.MustCompile(`(?is)<(?:script|style|noscript)[^>]*>.*?</(?:script|style|noscript)>`)
	browserTagPattern    = regexp.MustCompile(`(?s)<[^>]+>`)
	browserSpacePattern  = regexp.MustCompile(`[\t\r ]+`)
	browserBlankPattern  = regexp.MustCompile(`\n\s*\n+`)
)

func renderedPageText(body string) string {
	body = browserScriptPattern.ReplaceAllString(body, " ")
	body = browserTagPattern.ReplaceAllString(body, "\n")
	body = html.UnescapeString(body)
	body = browserSpacePattern.ReplaceAllString(body, " ")
	body = browserBlankPattern.ReplaceAllString(body, "\n")
	return strings.TrimSpace(body)
}
