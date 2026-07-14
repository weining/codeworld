package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"codeworld/internal/agent"
	"codeworld/internal/app"
	"codeworld/internal/model"
)

const maxMessageBytes = 16 << 20

// RuntimeInfo is the app-server-facing snapshot of a Codeworld runtime.
type RuntimeInfo struct {
	ID              string
	Name            string
	CWD             string
	Provider        string
	Model           string
	ApprovalPolicy  string
	Sandbox         string
	SandboxNetwork  bool
	AdditionalRoots []string
	Preview         string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Ephemeral       bool
}

// Runtime executes and persists turns for one app-server thread.
type Runtime interface {
	Info() RuntimeInfo
	Run(context.Context, model.Message, func(agent.TurnEvent) error, agent.ToolReporter) (agent.TurnResult, error)
	Close() error
}

// ThreadRequest contains the supported Codex thread/start and thread/resume options.
type ThreadRequest struct {
	ThreadID              string
	CWD                   string
	Model                 string
	ApprovalPolicy        string
	Sandbox               string
	Ephemeral             bool
	BaseInstructions      string
	DeveloperInstructions string
	Config                map[string]any
	RuntimeWorkspaceRoots []string
}

// RuntimeFactory creates a new or resumed Codeworld runtime.
type RuntimeFactory func(context.Context, ThreadRequest) (Runtime, error)

// Server serves the supported Codex app-server protocol over JSONL stdio.
type Server struct {
	Factory RuntimeFactory
	Home    string
	Version string

	mu      sync.Mutex
	threads map[string]*threadState
	writer  *messageWriter
	turns   sync.WaitGroup
	seq     atomic.Uint64
}

type threadState struct {
	runtime Runtime
	mu      sync.Mutex
	active  *activeTurn
}

type activeTurn struct {
	id     string
	cancel context.CancelFunc
}

type request struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type messageWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func newMessageWriter(out io.Writer) *messageWriter {
	return &messageWriter{encoder: json.NewEncoder(out)}
}

func (w *messageWriter) write(value any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.encoder.Encode(value)
}

func (w *messageWriter) response(id json.RawMessage, result any) error {
	return w.write(map[string]any{"id": id, "result": result})
}

func (w *messageWriter) failure(id json.RawMessage, code int, err error) error {
	return w.write(map[string]any{"id": id, "error": rpcError{Code: code, Message: err.Error()}})
}

func (w *messageWriter) notification(method string, params any) error {
	return w.write(map[string]any{"method": method, "params": params})
}

// Serve reads one protocol message per line and writes responses and notifications as JSONL.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) (err error) {
	if s.Factory == nil {
		return fmt.Errorf("app-server runtime factory is required")
	}
	s.mu.Lock()
	if s.threads == nil {
		s.threads = map[string]*threadState{}
	}
	s.writer = newMessageWriter(out)
	s.mu.Unlock()

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), maxMessageBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			if writeErr := s.writer.failure(nil, -32700, fmt.Errorf("invalid JSON: %w", err)); writeErr != nil {
				return writeErr
			}
			continue
		}
		if err := s.handle(ctx, req); err != nil {
			return err
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return scanErr
	}
	s.turns.Wait()
	return s.closeThreads()
}

func (s *Server) handle(ctx context.Context, req request) error {
	if req.Method == "initialized" {
		return nil
	}
	if len(req.ID) == 0 {
		return nil
	}
	var err error
	switch req.Method {
	case "initialize":
		err = s.initialize(req)
	case "thread/start":
		err = s.startThread(ctx, req, false)
	case "thread/resume":
		err = s.startThread(ctx, req, true)
	case "turn/start":
		err = s.startTurn(ctx, req)
	case "turn/interrupt":
		err = s.interruptTurn(req)
	default:
		return s.writer.failure(req.ID, -32601, fmt.Errorf("method %q is not supported", req.Method))
	}
	if err == nil {
		return nil
	}
	return s.writer.failure(req.ID, -32602, err)
}

func (s *Server) initialize(req request) error {
	var params struct {
		ClientInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	if err := decodeParams(req.Params, &params); err != nil {
		return err
	}
	if strings.TrimSpace(params.ClientInfo.Name) == "" || strings.TrimSpace(params.ClientInfo.Version) == "" {
		return fmt.Errorf("clientInfo.name and clientInfo.version are required")
	}
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	family := "unix"
	if runtime.GOOS == "windows" {
		family = "windows"
	}
	return s.writer.response(req.ID, map[string]any{
		"codexHome":      s.Home,
		"platformFamily": family,
		"platformOs":     osName,
		"userAgent":      "codeworld/" + firstNonEmpty(s.Version, "dev"),
	})
}

type threadParams struct {
	ThreadID              string         `json:"threadId"`
	CWD                   string         `json:"cwd"`
	Model                 string         `json:"model"`
	ApprovalPolicy        string         `json:"approvalPolicy"`
	Sandbox               string         `json:"sandbox"`
	Ephemeral             *bool          `json:"ephemeral"`
	BaseInstructions      string         `json:"baseInstructions"`
	DeveloperInstructions string         `json:"developerInstructions"`
	Config                map[string]any `json:"config"`
	RuntimeWorkspaceRoots []string       `json:"runtimeWorkspaceRoots"`
}

func (s *Server) startThread(ctx context.Context, req request, resume bool) error {
	var params threadParams
	if err := decodeParams(req.Params, &params); err != nil {
		return err
	}
	if resume && strings.TrimSpace(params.ThreadID) == "" {
		return fmt.Errorf("threadId is required")
	}
	request := ThreadRequest{
		ThreadID: params.ThreadID, CWD: params.CWD, Model: params.Model,
		ApprovalPolicy: params.ApprovalPolicy, Sandbox: params.Sandbox,
		BaseInstructions: params.BaseInstructions, DeveloperInstructions: params.DeveloperInstructions,
		Config: params.Config, RuntimeWorkspaceRoots: append([]string(nil), params.RuntimeWorkspaceRoots...),
	}
	if params.Ephemeral != nil {
		request.Ephemeral = *params.Ephemeral
	}

	if resume {
		s.mu.Lock()
		state := s.threads[params.ThreadID]
		s.mu.Unlock()
		if state != nil {
			return s.writer.response(req.ID, threadResponse(state.runtime.Info(), true))
		}
	}
	rt, err := s.Factory(ctx, request)
	if err != nil {
		return err
	}
	info := rt.Info()
	if strings.TrimSpace(info.ID) == "" {
		_ = rt.Close()
		return fmt.Errorf("runtime returned an empty thread id")
	}
	state := &threadState{runtime: rt}
	s.mu.Lock()
	if existing := s.threads[info.ID]; existing != nil {
		s.mu.Unlock()
		_ = rt.Close()
		return fmt.Errorf("thread %q is already loaded", info.ID)
	}
	s.threads[info.ID] = state
	s.mu.Unlock()
	if err := s.writer.response(req.ID, threadResponse(info, resume)); err != nil {
		return err
	}
	return s.writer.notification("thread/started", map[string]any{"thread": threadObject(info, nil)})
}

func threadResponse(info RuntimeInfo, includeTurns bool) map[string]any {
	turns := []any{}
	if !includeTurns {
		turns = []any{}
	}
	return map[string]any{
		"thread":                threadObject(info, turns),
		"model":                 info.Model,
		"modelProvider":         info.Provider,
		"cwd":                   info.CWD,
		"approvalPolicy":        normalizedApproval(info.ApprovalPolicy),
		"approvalsReviewer":     "user",
		"sandbox":               sandboxObject(info),
		"runtimeWorkspaceRoots": append([]string(nil), info.AdditionalRoots...),
		"instructionSources":    []string{},
	}
}

func threadObject(info RuntimeInfo, turns []any) map[string]any {
	if turns == nil {
		turns = []any{}
	}
	created := info.CreatedAt.Unix()
	updated := info.UpdatedAt.Unix()
	if info.CreatedAt.IsZero() {
		created = time.Now().Unix()
	}
	if info.UpdatedAt.IsZero() {
		updated = created
	}
	return map[string]any{
		"id": info.ID, "sessionId": info.ID, "name": nullableString(info.Name),
		"preview": info.Preview, "cwd": info.CWD, "modelProvider": info.Provider,
		"cliVersion": "codeworld", "source": "appServer", "status": map[string]any{"type": "idle"},
		"ephemeral": info.Ephemeral, "createdAt": created, "updatedAt": updated, "turns": turns,
	}
}

func sandboxObject(info RuntimeInfo) map[string]any {
	switch info.Sandbox {
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	case "read-only":
		return map[string]any{"type": "readOnly", "networkAccess": info.SandboxNetwork}
	default:
		return map[string]any{
			"type": "workspaceWrite", "networkAccess": info.SandboxNetwork,
			"writableRoots":   append([]string(nil), info.AdditionalRoots...),
			"excludeSlashTmp": false, "excludeTmpdirEnvVar": false,
		}
	}
}

type userInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
}

type turnStartParams struct {
	ThreadID string      `json:"threadId"`
	Input    []userInput `json:"input"`
}

func (s *Server) startTurn(parent context.Context, req request) error {
	var params turnStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return err
	}
	if strings.TrimSpace(params.ThreadID) == "" {
		return fmt.Errorf("threadId is required")
	}
	s.mu.Lock()
	state := s.threads[params.ThreadID]
	s.mu.Unlock()
	if state == nil {
		return fmt.Errorf("thread %q is not loaded", params.ThreadID)
	}
	message, err := buildUserMessage(state.runtime.Info().CWD, params.Input)
	if err != nil {
		return err
	}
	turnID := s.nextID("turn")
	userItemID := s.nextID("item")
	agentItemID := s.nextID("item")
	ctx, cancel := context.WithCancel(parent)
	state.mu.Lock()
	if state.active != nil {
		state.mu.Unlock()
		cancel()
		return fmt.Errorf("thread %q already has an active turn", params.ThreadID)
	}
	state.active = &activeTurn{id: turnID, cancel: cancel}
	state.mu.Unlock()

	started := time.Now()
	turn := turnObject(turnID, "inProgress", started, time.Time{}, "", nil)
	if err := s.writer.response(req.ID, map[string]any{"turn": turn}); err != nil {
		cancel()
		return err
	}
	if err := s.writer.notification("turn/started", map[string]any{"threadId": params.ThreadID, "turn": turn}); err != nil {
		cancel()
		return err
	}
	userItem := map[string]any{"id": userItemID, "type": "userMessage", "content": params.Input}
	if err := s.itemLifecycle("started", params.ThreadID, turnID, userItem, started); err != nil {
		cancel()
		return err
	}
	if err := s.itemLifecycle("completed", params.ThreadID, turnID, userItem, started); err != nil {
		cancel()
		return err
	}
	agentItem := map[string]any{"id": agentItemID, "type": "agentMessage", "text": ""}
	if err := s.itemLifecycle("started", params.ThreadID, turnID, agentItem, started); err != nil {
		cancel()
		return err
	}

	s.turns.Add(1)
	go s.runTurn(ctx, state, params.ThreadID, turnID, agentItemID, message, started)
	return nil
}

func (s *Server) runTurn(ctx context.Context, state *threadState, threadID, turnID, agentItemID string, message model.Message, started time.Time) {
	defer s.turns.Done()
	defer func() {
		state.mu.Lock()
		if state.active != nil && state.active.id == turnID {
			state.active = nil
		}
		state.mu.Unlock()
	}()

	var text strings.Builder
	reporter := &toolReporter{server: s, threadID: threadID, turnID: turnID, cwd: state.runtime.Info().CWD}
	result, runErr := state.runtime.Run(ctx, message, func(event agent.TurnEvent) error {
		switch event.Kind {
		case agent.TurnEventAssistantDelta:
			text.WriteString(event.Text)
			return s.writer.notification("item/agentMessage/delta", map[string]any{
				"threadId": threadID, "turnId": turnID, "itemId": agentItemID, "delta": event.Text,
			})
		case agent.TurnEventAssistantDone:
			if text.Len() == 0 {
				text.WriteString(event.Text)
			}
		case agent.TurnEventError:
			if event.Text != "" {
				return s.writer.notification("error", map[string]any{
					"threadId": threadID, "turnId": turnID, "willRetry": false,
					"error": map[string]any{"message": event.Text},
				})
			}
		}
		return nil
	}, reporter)
	if text.Len() == 0 {
		text.WriteString(result.FinalText)
	}
	finished := time.Now()
	agentItem := map[string]any{"id": agentItemID, "type": "agentMessage", "text": text.String()}
	_ = s.itemLifecycle("completed", threadID, turnID, agentItem, finished)
	status := "completed"
	var turnErr any
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			status = "interrupted"
		} else {
			status = "failed"
			turnErr = map[string]any{"message": runErr.Error()}
			_ = s.writer.notification("error", map[string]any{
				"threadId": threadID, "turnId": turnID, "willRetry": false,
				"error": map[string]any{"message": runErr.Error()},
			})
		}
	}
	turn := turnObject(turnID, status, started, finished, text.String(), turnErr)
	_ = s.writer.notification("turn/completed", map[string]any{"threadId": threadID, "turn": turn})
}

func (s *Server) itemLifecycle(phase, threadID, turnID string, item map[string]any, at time.Time) error {
	params := map[string]any{"threadId": threadID, "turnId": turnID, "item": item}
	if phase == "started" {
		params["startedAtMs"] = at.UnixMilli()
	} else {
		params["completedAtMs"] = at.UnixMilli()
	}
	return s.writer.notification("item/"+phase, params)
}

func turnObject(id, status string, started, finished time.Time, finalText string, turnErr any) map[string]any {
	items := []any{}
	if finalText != "" {
		items = append(items, map[string]any{"id": id + "-message", "type": "agentMessage", "text": finalText})
	}
	turn := map[string]any{"id": id, "status": status, "items": items, "startedAt": started.Unix()}
	if !finished.IsZero() {
		turn["completedAt"] = finished.Unix()
		turn["durationMs"] = finished.Sub(started).Milliseconds()
	}
	if turnErr != nil {
		turn["error"] = turnErr
	}
	return turn
}

func (s *Server) interruptTurn(req request) error {
	var params struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
	}
	if err := decodeParams(req.Params, &params); err != nil {
		return err
	}
	s.mu.Lock()
	state := s.threads[params.ThreadID]
	s.mu.Unlock()
	if state == nil {
		return fmt.Errorf("thread %q is not loaded", params.ThreadID)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.active == nil || state.active.id != params.TurnID {
		return fmt.Errorf("turn %q is not active", params.TurnID)
	}
	state.active.cancel()
	return s.writer.response(req.ID, map[string]any{})
}

func (s *Server) nextID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), s.seq.Add(1))
}

func (s *Server) closeThreads() error {
	s.mu.Lock()
	threads := make([]*threadState, 0, len(s.threads))
	for _, state := range s.threads {
		threads = append(threads, state)
	}
	s.threads = map[string]*threadState{}
	s.mu.Unlock()
	var errs []error
	for _, state := range threads {
		if err := state.runtime.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func buildUserMessage(cwd string, inputs []userInput) (model.Message, error) {
	if len(inputs) == 0 {
		return model.Message{}, fmt.Errorf("input is required")
	}
	var texts []string
	var parts []model.ContentPart
	for _, input := range inputs {
		switch input.Type {
		case "text":
			if input.Text != "" {
				texts = append(texts, input.Text)
				parts = append(parts, model.TextPart(input.Text))
			}
		case "localImage":
			path := input.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(cwd, path)
			}
			part, err := model.ImagePartFromFile(path)
			if err != nil {
				return model.Message{}, err
			}
			parts = append(parts, part)
		case "image":
			if strings.TrimSpace(input.URL) == "" {
				return model.Message{}, fmt.Errorf("image url is required")
			}
			parts = append(parts, model.ContentPart{Type: model.ContentPartImage, ImageURL: input.URL})
		case "mention", "skill":
			text := strings.TrimSpace(input.Name)
			if text == "" {
				text = strings.TrimSpace(input.Path)
			}
			if text != "" {
				texts = append(texts, text)
				parts = append(parts, model.TextPart(text))
			}
		default:
			return model.Message{}, fmt.Errorf("unsupported input type %q", input.Type)
		}
	}
	if len(parts) == 0 {
		return model.Message{}, fmt.Errorf("input contains no content")
	}
	return model.Message{Role: model.RoleUser, Content: strings.Join(texts, "\n"), Parts: parts}, nil
}

func decodeParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func normalizedApproval(value string) string {
	switch value {
	case "untrusted", "on-request", "never":
		return value
	case "read-only":
		return "never"
	case "full-access", "auto", "":
		return "on-request"
	default:
		return value
	}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type toolReporter struct {
	server   *Server
	threadID string
	turnID   string
	cwd      string
}

func (r *toolReporter) ReportTool(_ context.Context, event agent.ToolEvent) {
	status := "inProgress"
	phase := "started"
	if event.Status != agent.ToolEventStart {
		phase = "completed"
		status = "completed"
		if event.Status != agent.ToolEventSuccess {
			status = "failed"
		}
	}
	item := map[string]any{
		"id": event.CallID, "type": "dynamicToolCall", "tool": event.Name,
		"arguments": map[string]any{"target": event.Request.Target}, "status": status,
	}
	if event.Error != "" {
		item["success"] = false
		item["contentItems"] = []any{map[string]any{"type": "inputText", "text": event.Error}}
	} else if phase == "completed" {
		item["success"] = true
	}
	_ = r.server.itemLifecycle(phase, r.threadID, r.turnID, item, time.Now())
}

// AppRuntime adapts app.Runtime to the app-server Runtime interface.
type AppRuntime struct {
	runtime   *app.Runtime
	ephemeral bool
}

// NewAppRuntime wraps a configured Codeworld runtime for use by Server.
func NewAppRuntime(rt *app.Runtime, ephemeral bool) *AppRuntime {
	return &AppRuntime{runtime: rt, ephemeral: ephemeral}
}

// Info returns the current thread metadata.
func (r *AppRuntime) Info() RuntimeInfo {
	if r == nil || r.runtime == nil {
		return RuntimeInfo{}
	}
	sess := r.runtime.Session
	return RuntimeInfo{
		ID: sess.ID, Name: sess.Name, CWD: r.runtime.Workspace.Root,
		Provider: sess.Provider, Model: sess.Model, ApprovalPolicy: r.runtime.Config.ApprovalMode,
		Sandbox: r.runtime.Config.SandboxMode, SandboxNetwork: r.runtime.Config.SandboxNetwork,
		AdditionalRoots: append([]string(nil), r.runtime.Workspace.AdditionalRoots...),
		Preview:         preview(r.runtime.Messages), CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
		Ephemeral: r.ephemeral,
	}
}

// Run executes one streaming turn and persists any useful partial result.
func (r *AppRuntime) Run(ctx context.Context, input model.Message, emit func(agent.TurnEvent) error, reporter agent.ToolReporter) (agent.TurnResult, error) {
	if r == nil || r.runtime == nil {
		return agent.TurnResult{}, fmt.Errorf("runtime is unavailable")
	}
	r.runtime.Runner.Reporter = reporter
	result, runErr := r.runtime.Runner.RunTurnStreamMessage(ctx, r.runtime.Messages, input, emit)
	if !r.ephemeral && (len(result.Messages) > 0 || !result.Usage.IsZero()) {
		if saveErr := r.runtime.SaveTurn(result); saveErr != nil {
			runErr = errors.Join(runErr, saveErr)
		}
	}
	if runErr == nil && !r.ephemeral {
		if summaryErr := r.runtime.MaybeSummarize(ctx); summaryErr != nil {
			runErr = summaryErr
		}
	}
	return result, runErr
}

// Close releases the wrapped runtime resources.
func (r *AppRuntime) Close() error {
	if r == nil || r.runtime == nil {
		return nil
	}
	return r.runtime.Close()
}

func preview(messages []model.Message) string {
	for _, message := range messages {
		if message.Role == model.RoleUser && strings.TrimSpace(message.Content) != "" {
			return strings.TrimSpace(message.Content)
		}
	}
	return ""
}
