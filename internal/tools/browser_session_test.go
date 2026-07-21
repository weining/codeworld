package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"codeworld/internal/permissions"
	"codeworld/internal/sandbox"
)

func TestBrowserSessionChromeSmoke(t *testing.T) {
	if os.Getenv("CODEWORLD_BROWSER_SMOKE") == "" {
		t.Skip("set CODEWORLD_BROWSER_SMOKE=1 to run local Chrome integration")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/two":
			_, _ = w.Write([]byte(`<html><head><title>Second</title></head><body>Second page</body></html>`))
			return
		case "/file.txt":
			w.Header().Set("Content-Disposition", `attachment; filename="sample.txt"`)
			_, _ = w.Write([]byte("downloaded by codeworld"))
			return
		case "/diagnostics":
			_, _ = w.Write([]byte(`<html><head><title>Diagnostics</title></head><body><script>console.error('qa-boom');fetch('/missing')</script>Diagnostics page</body></html>`))
			return
		case "/missing":
			http.Error(w, "missing", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`<html><head><title>First</title></head><body><label for="name">Name</label><input id="name"><button id="copy" onclick="document.querySelector('#output').textContent=document.querySelector('#name').value">Copy</button><select id="color"><option value="red">Red</option><option value="blue">Blue</option></select><label for="upload">Upload</label><input id="upload" type="file" onchange="document.querySelector('#upload-output').textContent=this.files[0].name"><a href="/file.txt" download>Download</a><div id="output"></div><div id="upload-output"></div></body></html>`))
	}))
	defer server.Close()
	ws := newTestWorkspace(t)
	if err := os.MkdirAll(filepath.Join(ws.Root, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Root, "fixtures/upload.txt"), []byte("upload fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewBrowserSessionManager(ws, sandbox.FullAccess(ws.Root))
	defer manager.Close()
	byName := map[string]Tool{}
	for _, tool := range NewBrowserSessionTools(manager) {
		byName[tool.Definition().Name] = tool
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	started, err := byName["browser_session_start"].Execute(ctx, json.RawMessage(`{"url":"`+server.URL+`/one"}`))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := started.Metadata["session_id"].(string)
	textboxRef := snapshotRef(t, started.Content, "textbox")
	buttonRef := snapshotRef(t, started.Content, "button")
	selectRef := snapshotRef(t, started.Content, "combobox")
	uploadRef := snapshotRefNamed(t, started.Content, "textbox", "Upload")
	downloadRef := snapshotRefNamed(t, started.Content, "link", "Download")
	if textboxRef == buttonRef || buttonRef == selectRef {
		t.Fatalf("semantic snapshot missing refs:\n%s", started.Content)
	}
	if _, err := byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"type","ref":"`+textboxRef+`","text":"Codeworld"}`)); err != nil {
		t.Fatal(err)
	}
	result, err := byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"click","ref":"`+buttonRef+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Codeworld") {
		t.Fatalf("page text = %q", result.Content)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"select","ref":"`+selectRef+`","text":"blue"}`))
	if err != nil || !strings.Contains(result.Content, `value="blue"`) {
		t.Fatalf("select result=%q err=%v", result.Content, err)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"upload","ref":"`+uploadRef+`","file_path":"fixtures/upload.txt"}`))
	if err != nil || !strings.Contains(result.Content, "upload.txt") {
		t.Fatalf("upload result=%q err=%v", result.Content, err)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"download","ref":"`+downloadRef+`","download_dir":"artifacts/downloads"}`))
	if err != nil {
		t.Fatal(err)
	}
	downloaded, _ := os.ReadFile(filepath.Join(ws.Root, "artifacts/downloads/sample.txt"))
	if string(downloaded) != "downloaded by codeworld" {
		t.Fatalf("downloaded data=%q metadata=%#v", downloaded, result.Metadata)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"navigate","url":"`+server.URL+`/two"}`))
	if err != nil || !strings.Contains(result.Content, "Second page") {
		t.Fatalf("navigate result=%q err=%v", result.Content, err)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"back"}`))
	if err != nil || !strings.Contains(result.Content, "Title: First") {
		t.Fatalf("back result=%q err=%v", result.Content, err)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"forward"}`))
	if err != nil || !strings.Contains(result.Content, "Title: Second") {
		t.Fatalf("forward result=%q err=%v", result.Content, err)
	}
	if _, err := byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"reload"}`)); err != nil {
		t.Fatal(err)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"new_tab","url":"`+server.URL+`/one"}`))
	if err != nil || !strings.Contains(result.Content, "Title: First") {
		t.Fatalf("new tab result=%q err=%v", result.Content, err)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"tabs"}`))
	if err != nil || !strings.Contains(result.Content, "tab-1") || !strings.Contains(result.Content, "* tab-2") {
		t.Fatalf("tabs result=%q err=%v", result.Content, err)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"switch_tab","tab_id":"tab-1"}`))
	if err != nil || !strings.Contains(result.Content, "Title: Second") {
		t.Fatalf("switch tab result=%q err=%v", result.Content, err)
	}
	if _, err := byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"close_tab","tab_id":"tab-2"}`)); err != nil {
		t.Fatal(err)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"tabs"}`))
	if err != nil || strings.Contains(result.Content, "tab-2") {
		t.Fatalf("tabs after close=%q err=%v", result.Content, err)
	}
	if _, err := byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"screenshot","screenshot_path":"artifacts/session.png"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, "artifacts/session.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"navigate","url":"`+server.URL+`/diagnostics","wait_ms":100}`)); err != nil {
		t.Fatal(err)
	}
	result, err = byName["browser_session_action"].Execute(ctx, json.RawMessage(`{"session_id":"`+id+`","action":"diagnostics","clear":true}`))
	if err != nil || !strings.Contains(result.Content, "qa-boom") || !strings.Contains(result.Content, "HTTP 404") {
		t.Fatalf("diagnostics result=%q err=%v", result.Content, err)
	}
}

func TestBrowserSessionToolsDefinitionsAndValidation(t *testing.T) {
	ws := newTestWorkspace(t)
	manager := NewBrowserSessionManager(ws, sandbox.FullAccess(ws.Root))
	definitions := map[string]bool{}
	for _, tool := range NewBrowserSessionTools(manager) {
		definitions[tool.Definition().Name] = true
	}
	for _, name := range []string{"browser_session_start", "browser_session_action", "browser_session_close"} {
		if !definitions[name] {
			t.Fatalf("missing %s definition", name)
		}
	}
	action := browserActionTool{manager}
	for _, raw := range []string{
		`{"session_id":"browser-1","action":"navigate","url":"file:///tmp/page"}`,
		`{"session_id":"browser-1","action":"click"}`,
		`{"session_id":"browser-1","action":"click","ref":"bad-ref"}`,
		`{"session_id":"browser-1","action":"new_tab","url":"file:///tmp/page"}`,
		`{"session_id":"browser-1","action":"switch_tab","tab_id":"bad-tab"}`,
		`{"session_id":"browser-1","action":"close_tab"}`,
		`{"session_id":"browser-1","action":"screenshot","screenshot_path":"../page.png"}`,
		`{"session_id":"browser-1","action":"upload","selector":"#upload","file_path":"../secret.txt"}`,
		`{"session_id":"browser-1","action":"download","selector":"#download","download_dir":"../downloads"}`,
		`{"session_id":"browser-1","action":"download","download_dir":"downloads"}`,
		`{"session_id":"browser-1","action":"unknown"}`,
	} {
		if _, err := action.PermissionRequest(json.RawMessage(raw)); err == nil {
			t.Fatalf("PermissionRequest accepted %s", raw)
		}
	}
}

func TestCDPClientRecordsDiagnostics(t *testing.T) {
	client := &cdpClient{requestURLs: map[string]string{}}
	for _, raw := range []string{
		`{"method":"Runtime.consoleAPICalled","params":{"type":"error","args":[{"value":"boom"}]}}`,
		`{"method":"Runtime.exceptionThrown","params":{"exceptionDetails":{"text":"Uncaught","exception":{"description":"Error: broken"}}}}`,
		`{"method":"Network.responseReceived","params":{"response":{"url":"https://example.test/missing","status":404,"statusText":"Not Found"}}}`,
		`{"method":"Network.requestWillBeSent","params":{"requestId":"7","request":{"url":"https://example.test/api"}}}`,
		`{"method":"Network.loadingFailed","params":{"requestId":"7","errorText":"net::ERR_FAILED"}}`,
	} {
		var message cdpMessage
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			t.Fatal(err)
		}
		client.recordDiagnostic(message)
	}
	joined := strings.Join(client.diagnostics, "\n")
	for _, want := range []string{"console.error: boom", "Error: broken", "HTTP 404 Not Found", "net::ERR_FAILED", "https://example.test/api"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("diagnostics missing %q:\n%s", want, joined)
		}
	}
}

func TestBrowserSessionActionUsesExistingCDPSession(t *testing.T) {
	var expressions []string
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var request struct {
				ID     int64          `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			result := map[string]any{}
			switch request.Method {
			case "Runtime.evaluate":
				expression, _ := request.Params["expression"].(string)
				expressions = append(expressions, expression)
				value := any(true)
				if strings.Contains(expression, "data-codeworld-ref") && strings.Contains(expression, "JSON.stringify") {
					value = `{"url":"https://example.test","title":"Example","text":"Page text","elements":[{"ref":"ptest-e1","role":"button","name":"Save"}]}`
				}
				result = map[string]any{"result": map[string]any{"value": value}}
			case "Page.captureScreenshot":
				result = map[string]any{"data": "cG5n"}
			}
			_ = conn.WriteJSON(map[string]any{"id": request.ID, "result": result})
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ws := newTestWorkspace(t)
	profile := filepath.Join(ws.Root, "profile")
	if err := os.Mkdir(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := NewBrowserSessionManager(ws, sandbox.FullAccess(ws.Root))
	manager.sessions["browser-1"] = &browserSession{
		tabs: map[string]*browserTab{"tab-1": {targetID: "target-1", client: &cdpClient{conn: conn}}}, activeTab: "tab-1", nextTab: 1, profile: profile,
	}
	tool := browserActionTool{manager}

	req, err := tool.PermissionRequest(json.RawMessage(`{"session_id":"browser-1","action":"click","ref":"ptest-e1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Action != permissions.ActionWrite || req.Risk != permissions.RiskNetwork {
		t.Fatalf("permission = (%q, %q)", req.Action, req.Risk)
	}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"session_id":"browser-1","action":"click","ref":"ptest-e1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "[ref=ptest-e1] button \"Save\"") || !strings.Contains(result.Content, "Page text") || len(expressions) != 2 || !strings.Contains(expressions[0], "data-codeworld-ref") {
		t.Fatalf("result=%q expressions=%#v", result.Content, expressions)
	}

	result, err = tool.Execute(context.Background(), json.RawMessage(`{"session_id":"browser-1","action":"screenshot","screenshot_path":"artifacts/page.png"}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(ws.Root, "artifacts/page.png"))
	if err != nil || string(data) != "png" {
		t.Fatalf("screenshot data=%q err=%v", data, err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatalf("profile still exists: %v", err)
	}
}

func snapshotRef(t *testing.T, snapshot, role string) string {
	t.Helper()
	pattern := regexp.MustCompile(`\[ref=([^\]]+)\] ` + regexp.QuoteMeta(role) + ` `)
	match := pattern.FindStringSubmatch(snapshot)
	if len(match) != 2 {
		t.Fatalf("snapshot has no %s ref:\n%s", role, snapshot)
	}
	return match[1]
}

func snapshotRefNamed(t *testing.T, snapshot, role, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`\[ref=([^\]]+)\] ` + regexp.QuoteMeta(role) + ` "` + regexp.QuoteMeta(name) + `"`)
	match := pattern.FindStringSubmatch(snapshot)
	if len(match) != 2 {
		t.Fatalf("snapshot has no %s %q ref:\n%s", role, name, snapshot)
	}
	return match[1]
}
