package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"codeworld/internal/model"
	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
	"codeworld/internal/workspace"
)

const browserSessionStartupTimeout = 10 * time.Second

var browserRefPattern = regexp.MustCompile(`^p[a-z0-9]+-e[1-9][0-9]*$`)
var browserTabPattern = regexp.MustCompile(`^tab-[1-9][0-9]*$`)

const browserSnapshotExpression = `(()=>{
const attr='data-codeworld-ref';
const prefix=window.__codeworldRefPrefix||(window.__codeworldRefPrefix='p'+Math.floor(performance.timeOrigin).toString(36)+crypto.getRandomValues(new Uint32Array(1))[0].toString(36)+'-');
let next=Number(window.__codeworldNextRef)||1;
const visible=e=>{const s=getComputedStyle(e),r=e.getBoundingClientRect();return s.visibility!=='hidden'&&s.display!=='none'&&r.width>0&&r.height>0};
const role=e=>{const explicit=e.getAttribute('role');if(explicit)return explicit;const tag=e.tagName.toLowerCase();if(tag==='a')return 'link';if(tag==='button')return 'button';if(tag==='select')return 'combobox';if(tag==='textarea'||e.isContentEditable)return 'textbox';if(tag==='input'){const t=(e.type||'text').toLowerCase();if(t==='checkbox')return 'checkbox';if(t==='radio')return 'radio';if(t==='submit'||t==='button'||t==='reset')return 'button';return 'textbox'}return tag};
const name=e=>{const labelled=(e.getAttribute('aria-labelledby')||'').split(/\s+/).filter(Boolean).map(id=>document.getElementById(id)?.innerText||'').join(' ');const labels=e.labels?[...e.labels].map(x=>x.innerText||'').join(' '):'';return(e.getAttribute('aria-label')||labelled||labels||e.getAttribute('placeholder')||e.getAttribute('alt')||e.getAttribute('title')||e.innerText||e.value||'').trim().replace(/\s+/g,' ').slice(0,200)};
const nodes=[...document.querySelectorAll('a[href],button,input,textarea,select,summary,[role],[contenteditable="true"]')].filter(visible).slice(0,200);
const elements=nodes.map(e=>{let ref=e.getAttribute(attr);if(!ref){ref=prefix+'e'+next++;e.setAttribute(attr,ref)}return{ref,role:role(e),name:name(e),value:('value'in e?String(e.value):''),disabled:Boolean(e.disabled),checked:Boolean(e.checked)}});
window.__codeworldNextRef=next;
return JSON.stringify({url:location.href,title:document.title,text:(document.body?document.body.innerText:'').trim().slice(0,50000),elements});
})()`

type BrowserSessionManager struct {
	workspace workspace.Workspace
	sandbox   sandbox.Policy
	mu        sync.Mutex
	sessions  map[string]*browserSession
	nextID    atomic.Uint64
	closed    bool
}

type browserSession struct {
	mu        sync.Mutex
	tabs      map[string]*browserTab
	activeTab string
	nextTab   uint64
	debugPort string
	cmd       *exec.Cmd
	profile   string
}

type browserTab struct {
	targetID string
	client   *cdpClient
}

type chromePageTarget struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type cdpClient struct {
	conn        *websocket.Conn
	mu          sync.Mutex
	nextID      int64
	diagnostics []string
	requestURLs map[string]string
}

type cdpMessage struct {
	ID     int64           `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type browserStartArgs struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type browserActionArgs struct {
	SessionID      string `json:"session_id"`
	Action         string `json:"action"`
	URL            string `json:"url"`
	TabID          string `json:"tab_id"`
	Ref            string `json:"ref"`
	Selector       string `json:"selector"`
	Text           string `json:"text"`
	ScreenshotPath string `json:"screenshot_path"`
	FilePath       string `json:"file_path"`
	DownloadDir    string `json:"download_dir"`
	Submit         bool   `json:"submit"`
	Clear          bool   `json:"clear"`
	WaitMS         int    `json:"wait_ms"`
}

type browserCloseArgs struct {
	SessionID string `json:"session_id"`
}

type browserStartTool struct{ manager *BrowserSessionManager }
type browserActionTool struct{ manager *BrowserSessionManager }
type browserCloseTool struct{ manager *BrowserSessionManager }

func NewBrowserSessionManager(ws workspace.Workspace, policy sandbox.Policy) *BrowserSessionManager {
	return &BrowserSessionManager{workspace: ws, sandbox: policy, sessions: map[string]*browserSession{}}
}

func NewBrowserSessionTools(manager *BrowserSessionManager) []Tool {
	return []Tool{browserStartTool{manager}, browserActionTool{manager}, browserCloseTool{manager}}
}

func (t browserStartTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "browser_session_start", Description: "Start a persistent local headless Chrome session and optionally navigate to an HTTP(S) page.", InputSchema: objectSchema(map[string]any{
		"url": map[string]any{"type": "string"}, "width": map[string]any{"type": "integer", "minimum": 320, "maximum": 3840}, "height": map[string]any{"type": "integer", "minimum": 240, "maximum": 2160},
	}, nil)}
}

func (t browserActionTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "browser_session_action", Description: "Inspect or operate an existing browser session. Snapshot returns stable element refs; interactive actions accept a ref or CSS selector. Diagnostics reports console, JavaScript, and network failures.", InputSchema: objectSchema(map[string]any{
		"session_id": map[string]any{"type": "string"}, "action": map[string]any{"type": "string", "enum": []string{"navigate", "back", "forward", "reload", "snapshot", "diagnostics", "click", "type", "select", "upload", "download", "screenshot", "tabs", "new_tab", "switch_tab", "close_tab"}},
		"url": map[string]any{"type": "string"}, "tab_id": map[string]any{"type": "string"}, "ref": map[string]any{"type": "string"}, "selector": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}, "submit": map[string]any{"type": "boolean"},
		"screenshot_path": map[string]any{"type": "string"}, "file_path": map[string]any{"type": "string"}, "download_dir": map[string]any{"type": "string"}, "clear": map[string]any{"type": "boolean"}, "wait_ms": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
	}, []string{"session_id", "action"})}
}

func (t browserCloseTool) Definition() model.ToolDefinition {
	return model.ToolDefinition{Name: "browser_session_close", Description: "Close a persistent browser session and remove its temporary profile.", InputSchema: objectSchema(map[string]any{
		"session_id": map[string]any{"type": "string"},
	}, []string{"session_id"})}
}

func (t browserStartTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseBrowserStartArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	target := parsed.URL
	if target == "" {
		target = "about:blank"
	}
	return permissions.Request{Action: permissions.ActionWrite, Target: target, Risk: permissions.RiskNetwork, Reason: "browser_session_start"}, nil
}

func (t browserActionTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, _, err := t.parseArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	action := permissions.ActionRead
	risk := permissions.RiskRead
	if parsed.Action != "snapshot" && parsed.Action != "tabs" && parsed.Action != "diagnostics" {
		action, risk = permissions.ActionWrite, permissions.RiskNetwork
	}
	return permissions.Request{Action: action, Target: parsed.SessionID + ":" + parsed.Action, Risk: risk, Reason: "browser_session_action", Preview: firstNonEmpty(parsed.TabID, parsed.Ref, parsed.Selector, parsed.URL, parsed.ScreenshotPath, parsed.FilePath, parsed.DownloadDir)}, nil
}

func (t browserCloseTool) PermissionRequest(args json.RawMessage) (permissions.Request, error) {
	parsed, err := parseBrowserCloseArgs(args)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Action: permissions.ActionRead, Target: parsed.SessionID, Risk: permissions.RiskExecute, Reason: "browser_session_close"}, nil
}

func (t browserStartTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	parsed, err := parseBrowserStartArgs(args)
	if err != nil {
		return Result{}, err
	}
	id, err := t.manager.Start(ctx, parsed)
	if err != nil {
		return Result{}, err
	}
	content := "browser session " + id + " started"
	if parsed.URL != "" {
		if err := t.manager.navigate(ctx, id, parsed.URL); err != nil {
			_ = t.manager.CloseSession(id)
			return Result{}, err
		}
		content, err = t.manager.snapshot(ctx, id)
		if err != nil {
			_ = t.manager.CloseSession(id)
			return Result{}, err
		}
	}
	return Result{Content: content, Metadata: map[string]any{"session_id": id, "url": parsed.URL}}, nil
}

func (t browserActionTool) Execute(ctx context.Context, args json.RawMessage) (Result, error) {
	parsed, resolvedPath, err := t.parseArgs(args)
	if err != nil {
		return Result{}, err
	}
	if parsed.Action == "tabs" {
		content, err := t.manager.listTabs(ctx, parsed.SessionID)
		return Result{Content: content, Metadata: map[string]any{"session_id": parsed.SessionID, "action": parsed.Action}}, err
	}
	if parsed.Action == "diagnostics" {
		content, err := t.manager.diagnostics(ctx, parsed.SessionID, parsed.Clear)
		return Result{Content: content, Metadata: map[string]any{"session_id": parsed.SessionID, "action": parsed.Action}}, err
	}
	metadata := map[string]any{"session_id": parsed.SessionID, "action": parsed.Action}
	switch parsed.Action {
	case "navigate":
		err = t.manager.navigate(ctx, parsed.SessionID, parsed.URL)
	case "back", "forward":
		err = t.manager.history(ctx, parsed.SessionID, parsed.Action)
	case "reload":
		err = t.manager.reload(ctx, parsed.SessionID)
	case "new_tab":
		err = t.manager.newTab(ctx, parsed.SessionID, parsed.URL)
	case "switch_tab":
		err = t.manager.switchTab(ctx, parsed.SessionID, parsed.TabID)
	case "close_tab":
		err = t.manager.closeTab(ctx, parsed.SessionID, parsed.TabID)
	case "click":
		err = t.manager.evaluate(ctx, parsed.SessionID, clickExpression(elementSelector(parsed)))
	case "type":
		err = t.manager.evaluate(ctx, parsed.SessionID, typeExpression(elementSelector(parsed), parsed.Text, parsed.Submit))
	case "select":
		err = t.manager.evaluate(ctx, parsed.SessionID, selectExpression(elementSelector(parsed), parsed.Text))
	case "upload":
		err = t.manager.upload(ctx, parsed.SessionID, elementSelector(parsed), resolvedPath)
	case "download":
		var downloaded []string
		downloaded, err = t.manager.download(ctx, parsed.SessionID, elementSelector(parsed), resolvedPath, parsed.WaitMS)
		metadata["downloaded_files"] = downloaded
		parsed.WaitMS = 0
	case "screenshot":
		err = t.manager.screenshot(ctx, parsed.SessionID, resolvedPath)
	case "snapshot":
	}
	if err != nil {
		return Result{}, err
	}
	if err := waitBrowserAction(ctx, parsed.WaitMS); err != nil {
		return Result{}, err
	}
	content, err := t.manager.snapshot(ctx, parsed.SessionID)
	if err != nil {
		return Result{}, err
	}
	if parsed.Action == "screenshot" && resolvedPath != "" {
		rel, _ := t.manager.workspace.Rel(resolvedPath)
		metadata["screenshot_path"] = rel
	}
	return Result{Content: content, Metadata: metadata}, nil
}

func (t browserCloseTool) Execute(_ context.Context, args json.RawMessage) (Result, error) {
	parsed, err := parseBrowserCloseArgs(args)
	if err != nil {
		return Result{}, err
	}
	if err := t.manager.CloseSession(parsed.SessionID); err != nil {
		return Result{}, err
	}
	return Result{Content: "browser session " + parsed.SessionID + " closed", Metadata: map[string]any{"session_id": parsed.SessionID}}, nil
}

func (m *BrowserSessionManager) Start(ctx context.Context, args browserStartArgs) (string, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", fmt.Errorf("browser session manager is closed")
	}
	m.mu.Unlock()
	browserPath, err := findChrome()
	if err != nil {
		return "", err
	}
	profile, err := os.MkdirTemp(m.workspace.Root, ".codeworld-browser-session-")
	if err != nil {
		return "", err
	}
	width, height := args.Width, args.Height
	if width == 0 {
		width = defaultBrowserWidth
	}
	if height == 0 {
		height = defaultBrowserHeight
	}
	cmd, err := m.sandbox.Command(m.workspace.Root, browserPath, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--remote-debugging-port=0", "--user-data-dir="+profile, "--window-size="+strconv.Itoa(width)+","+strconv.Itoa(height), "about:blank")
	if err != nil {
		_ = os.RemoveAll(profile)
		return "", err
	}
	cmd.Env = append(os.Environ(), "TMPDIR="+profile)
	cmd.Stdout, cmd.Stderr = nil, nil
	configureCommandProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(profile)
		return "", err
	}
	debugPort, target, err := waitForPageTarget(ctx, profile)
	if err != nil {
		killCommandProcessGroup(cmd)
		_ = cmd.Wait()
		_ = os.RemoveAll(profile)
		return "", err
	}
	client, err := connectPageClient(ctx, target.WebSocketDebuggerURL)
	if err != nil {
		killCommandProcessGroup(cmd)
		_ = cmd.Wait()
		_ = os.RemoveAll(profile)
		return "", err
	}
	sess := &browserSession{
		tabs: map[string]*browserTab{"tab-1": {targetID: target.ID, client: client}}, activeTab: "tab-1", nextTab: 1,
		debugPort: debugPort, cmd: cmd, profile: profile,
	}
	id := fmt.Sprintf("browser-%d", m.nextID.Add(1))
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = closeBrowserSession(sess)
		return "", fmt.Errorf("browser session manager is closed")
	}
	m.sessions[id] = sess
	m.mu.Unlock()
	return id, nil
}

func (m *BrowserSessionManager) navigate(ctx context.Context, id, target string) error {
	_, client, err := m.active(id)
	if err != nil {
		return err
	}
	previous, err := client.location(ctx)
	if err != nil {
		return err
	}
	if _, err := client.call(ctx, "Page.navigate", map[string]any{"url": target}); err != nil {
		return err
	}
	if err := waitBrowserAction(ctx, 25); err != nil {
		return err
	}
	return client.waitReady(ctx, target, previous)
}

func (m *BrowserSessionManager) history(ctx context.Context, id, direction string) error {
	_, client, err := m.active(id)
	if err != nil {
		return err
	}
	previous, err := client.location(ctx)
	if err != nil {
		return err
	}
	result, err := client.call(ctx, "Page.getNavigationHistory", map[string]any{})
	if err != nil {
		return err
	}
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID  int64  `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(result, &history); err != nil {
		return err
	}
	index := history.CurrentIndex - 1
	if direction == "forward" {
		index = history.CurrentIndex + 1
	}
	if index < 0 || index >= len(history.Entries) {
		return fmt.Errorf("browser cannot go %s", direction)
	}
	entry := history.Entries[index]
	if _, err := client.call(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": entry.ID}); err != nil {
		return err
	}
	if err := waitBrowserAction(ctx, 25); err != nil {
		return err
	}
	return client.waitReady(ctx, entry.URL, previous)
}

func (m *BrowserSessionManager) reload(ctx context.Context, id string) error {
	_, client, err := m.active(id)
	if err != nil {
		return err
	}
	target, err := client.location(ctx)
	if err != nil {
		return err
	}
	if _, err := client.call(ctx, "Page.reload", map[string]any{}); err != nil {
		return err
	}
	if err := waitBrowserAction(ctx, 25); err != nil {
		return err
	}
	return client.waitReady(ctx, target, target)
}

func (m *BrowserSessionManager) evaluate(ctx context.Context, id, expression string) error {
	_, client, err := m.active(id)
	if err != nil {
		return err
	}
	result, err := client.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true})
	if err != nil {
		return err
	}
	return evaluationError(result)
}

func (m *BrowserSessionManager) snapshot(ctx context.Context, id string) (string, error) {
	_, client, err := m.active(id)
	if err != nil {
		return "", err
	}
	result, err := client.call(ctx, "Runtime.evaluate", map[string]any{"expression": browserSnapshotExpression, "returnByValue": true})
	if err != nil {
		return "", err
	}
	var payload struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", err
	}
	if len(payload.Exception) > 0 {
		return "", fmt.Errorf("browser evaluation failed")
	}
	var snapshot struct {
		URL      string `json:"url"`
		Title    string `json:"title"`
		Text     string `json:"text"`
		Elements []struct {
			Ref      string `json:"ref"`
			Role     string `json:"role"`
			Name     string `json:"name"`
			Value    string `json:"value"`
			Disabled bool   `json:"disabled"`
			Checked  bool   `json:"checked"`
		} `json:"elements"`
	}
	if err := json.Unmarshal([]byte(payload.Result.Value), &snapshot); err != nil {
		return "", fmt.Errorf("decode browser snapshot: %w", err)
	}
	var output strings.Builder
	fmt.Fprintf(&output, "Title: %s\nURL: %s", snapshot.Title, snapshot.URL)
	if len(snapshot.Elements) > 0 {
		output.WriteString("\nInteractive elements:")
		for _, element := range snapshot.Elements {
			fmt.Fprintf(&output, "\n[ref=%s] %s %q", element.Ref, element.Role, element.Name)
			if element.Value != "" {
				fmt.Fprintf(&output, " value=%q", element.Value)
			}
			if element.Disabled {
				output.WriteString(" disabled")
			}
			if element.Checked {
				output.WriteString(" checked")
			}
		}
	}
	if snapshot.Text != "" {
		output.WriteString("\nPage text:\n")
		output.WriteString(snapshot.Text)
	}
	text := output.String()
	if int64(len(text)) > defaultReadLimit {
		text = text[:defaultReadLimit]
	}
	return strings.TrimSpace(text), nil
}

func (m *BrowserSessionManager) screenshot(ctx context.Context, id, path string) error {
	_, client, err := m.active(id)
	if err != nil {
		return err
	}
	result, err := client.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "fromSurface": true})
	if err != nil {
		return err
	}
	var payload struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (m *BrowserSessionManager) newTab(ctx context.Context, id, targetURL string) error {
	sess, err := m.get(id)
	if err != nil {
		return err
	}
	if targetURL == "" {
		targetURL = "about:blank"
	}
	var target chromePageTarget
	if err := chromeJSON(ctx, http.MethodPut, sess.debugPort, "/json/new?"+url.QueryEscape(targetURL), &target); err != nil {
		return err
	}
	client, err := connectPageClient(ctx, target.WebSocketDebuggerURL)
	if err != nil {
		_ = chromeJSON(context.Background(), http.MethodGet, sess.debugPort, "/json/close/"+url.PathEscape(target.ID), nil)
		return err
	}
	sess.mu.Lock()
	sess.nextTab++
	tabID := fmt.Sprintf("tab-%d", sess.nextTab)
	sess.tabs[tabID] = &browserTab{targetID: target.ID, client: client}
	sess.activeTab = tabID
	sess.mu.Unlock()
	if targetURL != "about:blank" {
		previous := "about:blank"
		if err := client.waitReady(ctx, targetURL, previous); err != nil {
			_ = m.closeTab(context.Background(), id, tabID)
			return err
		}
	}
	return nil
}

func (m *BrowserSessionManager) listTabs(ctx context.Context, id string) (string, error) {
	sess, err := m.get(id)
	if err != nil {
		return "", err
	}
	sess.mu.Lock()
	active := sess.activeTab
	tabs := make(map[string]*browserTab, len(sess.tabs))
	ids := make([]string, 0, len(sess.tabs))
	for tabID, tab := range sess.tabs {
		tabs[tabID] = tab
		ids = append(ids, tabID)
	}
	sess.mu.Unlock()
	sort.Strings(ids)
	var output strings.Builder
	for _, tabID := range ids {
		result, err := tabs[tabID].client.call(ctx, "Runtime.evaluate", map[string]any{"expression": "[document.title, location.href]", "returnByValue": true})
		if err != nil {
			return "", err
		}
		var payload struct {
			Result struct {
				Value []string `json:"value"`
			} `json:"result"`
		}
		if err := json.Unmarshal(result, &payload); err != nil {
			return "", err
		}
		marker := " "
		if tabID == active {
			marker = "*"
		}
		title, pageURL := "", ""
		if len(payload.Result.Value) > 0 {
			title = payload.Result.Value[0]
		}
		if len(payload.Result.Value) > 1 {
			pageURL = payload.Result.Value[1]
		}
		fmt.Fprintf(&output, "%s %s %q %s\n", marker, tabID, title, pageURL)
	}
	return strings.TrimSpace(output.String()), nil
}

func (m *BrowserSessionManager) switchTab(ctx context.Context, id, tabID string) error {
	sess, err := m.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	tab, ok := sess.tabs[tabID]
	if ok {
		sess.activeTab = tabID
	}
	sess.mu.Unlock()
	if !ok {
		return fmt.Errorf("browser tab %q not found", tabID)
	}
	_, err = tab.client.call(ctx, "Page.bringToFront", map[string]any{})
	return err
}

func (m *BrowserSessionManager) closeTab(ctx context.Context, id, tabID string) error {
	sess, err := m.get(id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	if len(sess.tabs) <= 1 {
		sess.mu.Unlock()
		return fmt.Errorf("cannot close the only browser tab")
	}
	tab, ok := sess.tabs[tabID]
	if !ok {
		sess.mu.Unlock()
		return fmt.Errorf("browser tab %q not found", tabID)
	}
	sess.mu.Unlock()
	if err := chromeJSON(ctx, http.MethodGet, sess.debugPort, "/json/close/"+url.PathEscape(tab.targetID), nil); err != nil {
		return err
	}
	sess.mu.Lock()
	delete(sess.tabs, tabID)
	if sess.activeTab == tabID {
		ids := make([]string, 0, len(sess.tabs))
		for remaining := range sess.tabs {
			ids = append(ids, remaining)
		}
		sort.Strings(ids)
		sess.activeTab = ids[0]
	}
	sess.mu.Unlock()
	_ = tab.client.conn.Close()
	return nil
}

func chromeJSON(ctx context.Context, method, port, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:"+port+path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Chrome DevTools status %d", resp.StatusCode)
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (m *BrowserSessionManager) get(id string) (*browserSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("browser session %q not found", id)
	}
	return sess, nil
}

func (m *BrowserSessionManager) active(id string) (*browserSession, *cdpClient, error) {
	sess, err := m.get(id)
	if err != nil {
		return nil, nil, err
	}
	sess.mu.Lock()
	tab := sess.tabs[sess.activeTab]
	sess.mu.Unlock()
	if tab == nil || tab.client == nil {
		return nil, nil, fmt.Errorf("browser session %q has no active tab", id)
	}
	return sess, tab.client, nil
}

func (m *BrowserSessionManager) CloseSession(id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("browser session %q not found", id)
	}
	return closeBrowserSession(sess)
}

func (m *BrowserSessionManager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	sessions := make([]*browserSession, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, sess)
	}
	m.sessions = map[string]*browserSession{}
	m.mu.Unlock()
	var first error
	for _, sess := range sessions {
		if err := closeBrowserSession(sess); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func closeBrowserSession(sess *browserSession) error {
	sess.mu.Lock()
	tabs := make([]*browserTab, 0, len(sess.tabs))
	for _, tab := range sess.tabs {
		tabs = append(tabs, tab)
	}
	sess.tabs = map[string]*browserTab{}
	sess.mu.Unlock()
	for _, tab := range tabs {
		if tab.client != nil && tab.client.conn != nil {
			_ = tab.client.conn.Close()
		}
	}
	if sess.cmd != nil && sess.cmd.Process != nil {
		killCommandProcessGroup(sess.cmd)
		_ = sess.cmd.Wait()
	}
	return os.RemoveAll(sess.profile)
}

func (c *cdpClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	id := c.nextID
	if err := c.conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		if deadline, ok := ctx.Deadline(); ok {
			_ = c.conn.SetReadDeadline(deadline)
		} else {
			_ = c.conn.SetReadDeadline(time.Time{})
		}
		var msg cdpMessage
		if err := c.conn.ReadJSON(&msg); err != nil {
			return nil, err
		}
		if msg.Method != "" {
			c.recordDiagnostic(msg)
		}
		if msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("cdp %s: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

func (c *cdpClient) location(ctx context.Context) (string, error) {
	result, err := c.call(ctx, "Runtime.evaluate", map[string]any{"expression": "location.href", "returnByValue": true})
	if err != nil {
		return "", err
	}
	var payload struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", err
	}
	return payload.Result.Value, nil
}

func (c *cdpClient) waitReady(ctx context.Context, target, previous string) error {
	var lastURL, lastState string
	for {
		result, err := c.call(ctx, "Runtime.evaluate", map[string]any{"expression": "[location.href, document.readyState]", "returnByValue": true})
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("wait for navigation to %s: %w (last url=%s state=%s)", target, ctx.Err(), lastURL, lastState)
			}
			return err
		}
		var payload struct {
			Result struct {
				Value []string `json:"value"`
			} `json:"result"`
		}
		if err := json.Unmarshal(result, &payload); err != nil {
			return err
		}
		if len(payload.Result.Value) == 2 && payload.Result.Value[1] == "complete" && (sameBrowserURL(payload.Result.Value[0], target) || payload.Result.Value[0] != previous) {
			return nil
		}
		if len(payload.Result.Value) == 2 {
			lastURL, lastState = payload.Result.Value[0], payload.Result.Value[1]
		}
		if err := waitBrowserAction(ctx, 25); err != nil {
			return fmt.Errorf("wait for navigation to %s: %w (last url=%s state=%s)", target, err, lastURL, lastState)
		}
	}
}

func sameBrowserURL(actual, target string) bool {
	return strings.TrimSuffix(actual, "/") == strings.TrimSuffix(target, "/")
}

func waitForPageTarget(ctx context.Context, profile string) (string, chromePageTarget, error) {
	deadline := time.Now().Add(browserSessionStartupTimeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return "", chromePageTarget{}, err
		}
		data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if err == nil {
			lines := strings.Split(string(data), "\n")
			if len(lines) > 0 {
				request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strings.TrimSpace(lines[0])+"/json/list", nil)
				resp, err := http.DefaultClient.Do(request)
				if err == nil {
					var pages []chromePageTarget
					err = json.NewDecoder(resp.Body).Decode(&pages)
					_ = resp.Body.Close()
					if err == nil {
						for _, page := range pages {
							if page.Type == "page" && page.WebSocketDebuggerURL != "" {
								return strings.TrimSpace(lines[0]), page, nil
							}
						}
					}
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return "", chromePageTarget{}, fmt.Errorf("timed out waiting for Chrome DevTools")
}

func connectPageClient(ctx context.Context, wsURL string) (*cdpClient, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, err
	}
	client := &cdpClient{conn: conn, requestURLs: map[string]string{}}
	if _, err := client.call(ctx, "Page.enable", map[string]any{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := client.call(ctx, "Runtime.enable", map[string]any{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := client.call(ctx, "Network.enable", map[string]any{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := client.call(ctx, "Log.enable", map[string]any{}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

func parseBrowserStartArgs(args json.RawMessage) (browserStartArgs, error) {
	var parsed browserStartArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return parsed, err
	}
	parsed.URL = strings.TrimSpace(parsed.URL)
	if parsed.URL != "" && !isHTTPURL(parsed.URL) {
		return parsed, fmt.Errorf("url must use http or https")
	}
	if parsed.Width != 0 && (parsed.Width < 320 || parsed.Width > 3840) {
		return parsed, fmt.Errorf("browser width is out of range")
	}
	if parsed.Height != 0 && (parsed.Height < 240 || parsed.Height > 2160) {
		return parsed, fmt.Errorf("browser height is out of range")
	}
	return parsed, nil
}

func (t browserActionTool) parseArgs(args json.RawMessage) (browserActionArgs, string, error) {
	var parsed browserActionArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return parsed, "", err
	}
	parsed.SessionID, parsed.Action = strings.TrimSpace(parsed.SessionID), strings.TrimSpace(parsed.Action)
	parsed.URL, parsed.TabID, parsed.Ref = strings.TrimSpace(parsed.URL), strings.TrimSpace(parsed.TabID), strings.TrimSpace(parsed.Ref)
	parsed.Selector, parsed.ScreenshotPath = strings.TrimSpace(parsed.Selector), strings.TrimSpace(parsed.ScreenshotPath)
	parsed.FilePath, parsed.DownloadDir = strings.TrimSpace(parsed.FilePath), strings.TrimSpace(parsed.DownloadDir)
	if parsed.SessionID == "" {
		return parsed, "", fmt.Errorf("session_id is required")
	}
	if parsed.WaitMS < 0 || parsed.WaitMS > 10000 {
		return parsed, "", fmt.Errorf("wait_ms must be between 0 and 10000")
	}
	switch parsed.Action {
	case "navigate":
		if !isHTTPURL(parsed.URL) {
			return parsed, "", fmt.Errorf("navigate requires an http or https url")
		}
	case "click", "type", "select", "upload", "download":
		if parsed.Ref == "" && parsed.Selector == "" {
			return parsed, "", fmt.Errorf("%s requires ref or selector", parsed.Action)
		}
		if parsed.Ref != "" && !browserRefPattern.MatchString(parsed.Ref) {
			return parsed, "", fmt.Errorf("invalid browser element ref %q", parsed.Ref)
		}
		if parsed.Action == "upload" {
			if parsed.FilePath == "" {
				return parsed, "", fmt.Errorf("upload requires file_path")
			}
			resolved, err := t.manager.workspace.Resolve(parsed.FilePath)
			if err != nil {
				return parsed, "", err
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return parsed, "", err
			}
			if !info.Mode().IsRegular() {
				return parsed, "", fmt.Errorf("upload file_path must be a regular file")
			}
			return parsed, resolved, nil
		}
		if parsed.Action == "download" {
			if parsed.DownloadDir == "" {
				return parsed, "", fmt.Errorf("download requires download_dir")
			}
			resolved, err := t.manager.workspace.Resolve(parsed.DownloadDir)
			if err != nil {
				return parsed, "", err
			}
			return parsed, resolved, nil
		}
	case "new_tab":
		if parsed.URL != "" && !isHTTPURL(parsed.URL) {
			return parsed, "", fmt.Errorf("new_tab url must use http or https")
		}
	case "switch_tab", "close_tab":
		if !browserTabPattern.MatchString(parsed.TabID) {
			return parsed, "", fmt.Errorf("%s requires a valid tab_id", parsed.Action)
		}
	case "snapshot", "diagnostics", "back", "forward", "reload", "tabs":
	case "screenshot":
		if parsed.ScreenshotPath == "" || strings.ToLower(filepath.Ext(parsed.ScreenshotPath)) != ".png" {
			return parsed, "", fmt.Errorf("screenshot requires a workspace PNG path")
		}
		resolved, err := t.manager.workspace.Resolve(parsed.ScreenshotPath)
		if err != nil {
			return parsed, "", err
		}
		return parsed, resolved, nil
	default:
		return parsed, "", fmt.Errorf("unsupported browser action %q", parsed.Action)
	}
	return parsed, "", nil
}

func parseBrowserCloseArgs(args json.RawMessage) (browserCloseArgs, error) {
	var parsed browserCloseArgs
	if err := decodeArgs(args, &parsed); err != nil {
		return parsed, err
	}
	parsed.SessionID = strings.TrimSpace(parsed.SessionID)
	if parsed.SessionID == "" {
		return parsed, fmt.Errorf("session_id is required")
	}
	return parsed, nil
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func clickExpression(selector string) string {
	encoded, _ := json.Marshal(selector)
	return "(()=>{const e=document.querySelector(" + string(encoded) + ");if(!e)throw new Error('selector not found');e.scrollIntoView({block:'center',inline:'center'});e.click();return true})()"
}

func selectExpression(selector, value string) string {
	s, _ := json.Marshal(selector)
	v, _ := json.Marshal(value)
	return "(()=>{const e=document.querySelector(" + string(s) + ");if(!e)throw new Error('selector not found');if(!(e instanceof HTMLSelectElement))throw new Error('element is not a select');e.value=" + string(v) + ";e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));return true})()"
}

func elementSelector(args browserActionArgs) string {
	if args.Ref == "" {
		return args.Selector
	}
	return `[data-codeworld-ref="` + args.Ref + `"]`
}

func typeExpression(selector, text string, submit bool) string {
	s, _ := json.Marshal(selector)
	v, _ := json.Marshal(text)
	expression := "(()=>{const e=document.querySelector(" + string(s) + ");if(!e)throw new Error('selector not found');e.scrollIntoView({block:'center',inline:'center'});e.focus();const v=" + string(v) + ";const p=e instanceof HTMLInputElement?HTMLInputElement.prototype:e instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:null;const set=p&&Object.getOwnPropertyDescriptor(p,'value')?.set;if(set)set.call(e,v);else if(e.isContentEditable)e.textContent=v;else e.value=v;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));"
	if submit {
		expression += "const f=e.closest('form');if(f)f.requestSubmit();else e.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',code:'Enter',bubbles:true}));"
	}
	return expression + "return true})()"
}

func evaluationError(result json.RawMessage) error {
	var payload struct {
		Exception *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return err
	}
	if payload.Exception != nil {
		detail := firstNonEmpty(payload.Exception.Exception.Description, payload.Exception.Text, "JavaScript evaluation error")
		return fmt.Errorf("browser action failed: %s", detail)
	}
	return nil
}

func waitBrowserAction(ctx context.Context, waitMS int) error {
	if waitMS <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(waitMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
