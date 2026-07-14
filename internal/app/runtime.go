package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"codeworld/internal/agent"
	"codeworld/internal/capability"
	"codeworld/internal/config"
	contextgraph "codeworld/internal/context/graph"
	"codeworld/internal/context/indexer"
	"codeworld/internal/context/summarizer"
	"codeworld/internal/execpolicy"
	"codeworld/internal/hooks"
	"codeworld/internal/instructions"
	"codeworld/internal/llmprofile"
	"codeworld/internal/mcp"
	"codeworld/internal/model"
	"codeworld/internal/model/provider"
	"codeworld/internal/permissions"
	"codeworld/internal/repl"
	"codeworld/internal/sandbox"
	"codeworld/internal/session"
	"codeworld/internal/skill"
	"codeworld/internal/subagent"
	"codeworld/internal/tools"
	"codeworld/internal/workspace"
)

type Options struct {
	Root            string
	Profile         string
	Model           string
	SessionID       string
	ResumeLast      bool
	NewSession      bool
	Ephemeral       bool
	CompactPrompt   string
	AdditionalDirs  []string
	ApprovalMode    permissions.Mode
	SandboxMode     sandbox.Mode
	SandboxNetwork  *bool
	ConfigOverrides []string
	SkipUserConfig  bool
	IgnoreRules     bool
	NativeSearch    bool
	BypassHookTrust bool
	In              io.Reader
	Out             io.Writer
	Err             io.Writer
}

type Runtime struct {
	Config           config.Config
	Workspace        workspace.Workspace
	Store            session.Store
	Session          session.Session
	Messages         []model.Message
	Usage            model.Usage
	Skills           []skill.Skill
	MCPClients       []*mcp.Client
	Subagents        *subagent.Manager
	CommandSessions  *tools.CommandSessionManager
	Runner           agent.Runner
	ChildRunner      *agent.Runner
	Hooks            *hooks.Runner
	BaseSystemPrompt string
	CompactPrompt    string
	NativeSearch     bool
	ProviderProfiles *llmprofile.Store
	ActiveProfile    string
	ModelLogPath     string
	Diff             func(context.Context) (string, error)
	In               io.Reader
	Out              io.Writer
	Err              io.Writer
	closer           *runtimeCloser
}

type runtimeCloser struct {
	once sync.Once
	err  error
}

// SaveTurn 持久化当前状态，并处理路径、权限或归档细节。
func (r *Runtime) SaveTurn(result agent.TurnResult) error {
	r.Messages = historyMessages(result.Messages)
	r.Usage = r.Usage.Add(result.Usage)
	r.Session.Messages = modelMessagesToSession(r.Messages)
	r.Session.Usage = sessionUsage(r.Usage)
	return r.Store.SaveCurrent(r.Session)
}

// Close 释放持有的资源，避免后台进程或句柄泄漏。
func (r *Runtime) Close() error {
	if r.closer == nil {
		return r.closeResources()
	}
	r.closer.once.Do(func() { r.closer.err = r.closeResources() })
	return r.closer.err
}

func (r *Runtime) closeResources() error {
	var errs []error
	if r.CommandSessions != nil {
		if err := r.CommandSessions.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if r.Subagents != nil {
		if err := r.Subagents.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if r.Hooks != nil {
		if err := r.Hooks.Run(context.Background(), "Stop", hooks.Context{}); err != nil {
			errs = append(errs, err)
		}
	}
	if err := capability.CloseClients(r.MCPClients); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// RefreshSystemPrompt rebuilds mutable goal, mode, and summary context.
func (r *Runtime) RefreshSystemPrompt() {
	prompt := r.SystemPromptFor(r.Session)
	r.Runner.SystemPrompt = prompt
	if r.ChildRunner != nil {
		r.ChildRunner.SystemPrompt = prompt + "\n\nSubagent mode:\nYou are a local read-only subagent. Investigate independently, avoid editing files, and return concise findings for the parent agent."
	}
}

// SystemPromptFor renders a prompt for a session snapshot.
func (r *Runtime) SystemPromptFor(sess session.Session) string {
	return composeSystemPrompt(r.BaseSystemPrompt, sess)
}

// SetModel keeps parent and subagent runners aligned with the session model.
func (r *Runtime) SetModel(name string) {
	r.Session.Model = name
	r.Runner.ModelName = name
	if r.ChildRunner != nil {
		r.ChildRunner.ModelName = name
	}
}

// MaybeSummarize 在会话过长时压缩旧历史，降低后续模型调用的上下文压力。
func (r *Runtime) MaybeSummarize(ctx context.Context) error {
	if r.Runner.Model == nil || !summarizer.ShouldSummarize(r.Messages, summarizer.Options{MaxMessages: r.Config.SummaryMaxMessages, MaxTokens: r.Config.SummaryMaxTokens}) {
		return nil
	}
	// 摘要只压缩较早的对话，保留最近若干轮原文，避免工具调用上下文被过度概括。
	summary, recent, err := summarizer.Summarize(ctx, r.Runner.Model, r.Session.Summary, r.Messages, summarizer.Options{
		MaxMessages: r.Config.SummaryMaxMessages,
		MaxTokens:   r.Config.SummaryMaxTokens,
		KeepRecent:  20,
		Prompt:      r.CompactPrompt,
	})
	if err != nil {
		return err
	}
	r.Session.Summary = summary
	r.Messages = recent
	r.Session.Messages = modelMessagesToSession(recent)
	r.RefreshSystemPrompt()
	return r.Store.SaveCurrent(r.Session)
}

// NewRuntime 创建并返回对应组件，集中设置默认依赖和初始状态。
func NewRuntime(ctx context.Context, opts Options) (Runtime, error) {
	if err := ctx.Err(); err != nil {
		return Runtime{}, err
	}
	root := opts.Root
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return Runtime{}, err
		}
	}
	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: opts.Profile, Overrides: opts.ConfigOverrides, SkipUser: opts.SkipUserConfig})
	if err != nil {
		return Runtime{}, err
	}
	if opts.Model != "" {
		cfg.Model = opts.Model
	}
	if opts.ApprovalMode != "" {
		switch opts.ApprovalMode {
		case permissions.ModeAuto, permissions.ModeReadOnly, permissions.ModeFullAccess, permissions.ModeUntrusted, permissions.ModeOnRequest, permissions.ModeNever:
		default:
			return Runtime{}, fmt.Errorf("invalid approval mode %q", opts.ApprovalMode)
		}
		cfg.ApprovalMode = string(opts.ApprovalMode)
	}
	configRoot, err := workspace.New(root)
	if err != nil {
		return Runtime{}, err
	}
	workspaceRoot := configRoot.Root
	if cfg.Workspace != "" && cfg.Workspace != "." {
		workspaceRoot, err = configRoot.Resolve(cfg.Workspace)
		if err != nil {
			return Runtime{}, fmt.Errorf("resolve configured workspace: %w", err)
		}
	}
	ws, err := workspace.NewWithAdditional(workspaceRoot, opts.AdditionalDirs)
	if err != nil {
		return Runtime{}, err
	}
	if opts.Err != nil && !ws.IsGitRepo() {
		fmt.Fprintln(opts.Err, "warning: current workspace is not a git repository; patch workflows are safer in git repositories")
	}
	var execRules execpolicy.Set
	if !opts.IgnoreRules {
		home, err := config.Home("")
		if err != nil {
			return Runtime{}, err
		}
		execRules, err = execpolicy.Load(ws.Root, home)
		if err != nil {
			return Runtime{}, fmt.Errorf("load exec rules: %w", err)
		}
	}
	sandboxMode := sandbox.Mode(cfg.SandboxMode)
	if opts.SandboxMode != "" {
		sandboxMode = opts.SandboxMode
		cfg.SandboxMode = string(opts.SandboxMode)
	}
	if sandboxMode == sandbox.ModeDangerFullAccess && opts.SandboxNetwork != nil {
		return Runtime{}, fmt.Errorf("network options cannot be combined with danger-full-access")
	}
	sandboxNetwork := cfg.SandboxNetwork
	if opts.SandboxNetwork != nil {
		sandboxNetwork = *opts.SandboxNetwork
		cfg.SandboxNetwork = sandboxNetwork
	}
	if sandboxMode == sandbox.ModeDangerFullAccess {
		sandboxNetwork = true
		cfg.SandboxNetwork = true
		if opts.Err != nil {
			fmt.Fprintln(opts.Err, "warning: danger-full-access disables OS filesystem and network sandboxing")
		}
	}
	sandboxPolicy := sandbox.Policy{Mode: sandboxMode, Workspace: ws.Root, WritableRoots: append([]string(nil), ws.AdditionalRoots...), Network: sandboxNetwork}
	if err := sandboxPolicy.Validate(); err != nil {
		return Runtime{}, err
	}

	// system prompt 由静态身份、工作区摘要、索引图、项目指令和历史摘要拼接而成。
	// 这里集中装配，保证 REPL 和 TUI 看到的是同一套上下文。
	summary, err := ws.Summary(120)
	if err != nil {
		summary = "workspace summary unavailable: " + err.Error()
	}
	systemPrompt := agent.DefaultSystemPrompt + "\n\nWorkspace files:\n" + summary
	if indexSummary := loadIndexSummary(ws.Root); indexSummary != "" {
		systemPrompt += "\n\nWorkspace index:\n" + indexSummary
	}
	if graphSummary := loadContextGraphSummary(ws.Root, cfg.IndexMaxFileBytes); graphSummary != "" {
		systemPrompt += "\n\nWorkspace context graph:\n" + graphSummary
	}
	projectInstructions, err := instructions.Load(ws.Root, instructions.Options{MaxBytes: instructions.DefaultMaxBytes})
	if err != nil {
		return Runtime{}, err
	}
	if projectInstructions.Text != "" {
		systemPrompt += "\n\nProject instructions:\n" + projectInstructions.Text
	}
	store := session.NewStore(ws.Root)
	providerHome, err := config.Home("")
	if err != nil {
		return Runtime{}, err
	}
	providerProfiles, err := llmprofile.Load(providerHome)
	if err != nil {
		return Runtime{}, err
	}
	configuredProfileRef := profileReference(cfg.Provider)
	if configuredProfileRef != "" {
		configuredProfile, ok := providerProfiles.Get(configuredProfileRef)
		if !ok {
			return Runtime{}, fmt.Errorf("provider profile %q not found", configuredProfileRef)
		}
		if opts.Model == "" {
			cfg.Model = configuredProfile.Model
		}
	}
	if opts.ResumeLast {
		sessions, err := store.List()
		if err != nil {
			return Runtime{}, err
		}
		if len(sessions) == 0 {
			return Runtime{}, fmt.Errorf("no sessions to resume")
		}
		if err := store.SetCurrent(sessions[0].ID); err != nil {
			return Runtime{}, err
		}
	}
	if opts.SessionID != "" {
		if err := store.SetCurrent(opts.SessionID); err != nil {
			return Runtime{}, err
		}
	}
	var sess session.Session
	if opts.Ephemeral || opts.NewSession {
		sess = session.New(ws.Root, cfg.Provider, cfg.Model)
	} else {
		sess, err = loadOrCreateSession(store, ws.Root, cfg.Provider, cfg.Model)
		if err != nil {
			return Runtime{}, err
		}
	}
	if opts.Model != "" {
		sess.Model = opts.Model
	}
	activeProfile := ""
	profileRef := profileReference(sess.Provider)
	if profileRef == "" {
		profileRef = configuredProfileRef
	}
	var selectedProfile llmprofile.Profile
	if profileRef != "" {
		var ok bool
		selectedProfile, ok = providerProfiles.Get(profileRef)
		if !ok {
			return Runtime{}, fmt.Errorf("provider profile %q not found", profileRef)
		}
		activeProfile = profileRef
		sess.Provider = "profile:" + profileRef
		cfg.Provider = sess.Provider
		if opts.Model == "" && (opts.NewSession || sess.Model == "") {
			sess.Model = selectedProfile.Model
		}
		cfg.Model = sess.Model
	}
	// Approvals are process-local trust decisions. Do not trust approvals read
	// from a workspace-controlled session file after a restart.
	sess.Approvals = nil
	sess.ApprovalMode = ""
	messages := sanitizeModelMessages(sessionMessagesToModel(sess.Messages, ws))
	sess.Messages = modelMessagesToSession(messages)
	confirmationIn := opts.In
	if confirmationIn != nil {
		if _, ok := confirmationIn.(*bufio.Reader); !ok {
			confirmationIn = bufio.NewReader(confirmationIn)
		}
	}
	confirmer := repl.Confirmer{In: confirmationIn, Out: opts.Out, Approvals: &sess.Approvals}
	authorizeExternal := func(ctx context.Context, req permissions.Request) error {
		allowed, err := confirmer.Confirm(ctx, req, permissions.Decision{Kind: permissions.DecisionAsk, Reason: req.Reason})
		if err != nil {
			return err
		}
		if !allowed {
			return fmt.Errorf("permission denied: %s", req.Reason)
		}
		return nil
	}
	hookRunner, err := hooks.LoadWithSandbox(ws.Root, sandboxPolicy)
	if err != nil {
		return Runtime{}, err
	}
	for _, command := range hookRunner.Commands() {
		if opts.BypassHookTrust {
			continue
		}
		if err := authorizeExternal(ctx, permissions.Request{Action: permissions.ActionShell, Target: command, Risk: permissions.RiskExecute, Reason: "run workspace hook"}); err != nil {
			return Runtime{}, err
		}
	}
	hookRunner.Enable()
	modelName := firstNonEmpty(sess.Model, cfg.Model)
	logPath := ""
	if cfg.ModelCallLogging {
		logPath = modelCallLogPath(ws.Root)
	}
	// provider client 负责隐藏各家 API 差异；Runtime 只关心统一的 Generate/Stream 接口。
	providerConfig := provider.Config{
		Provider:        cfg.Provider,
		Model:           modelName,
		DeepSeekAPIKey:  cfg.APIKey,
		OpenAIAPIKey:    cfg.OpenAIAPIKey,
		AnthropicAPIKey: cfg.AnthropicAPIKey,
		LocalBaseURL:    cfg.LocalBaseURL,
		Root:            ws.Root,
		LogPath:         logPath,
	}
	if activeProfile != "" {
		providerConfig = formattedProviderConfig(selectedProfile, modelName, ws.Root, logPath)
	}
	client, err := provider.NewClient(providerConfig)
	if err != nil {
		if activeProfile != "" {
			return Runtime{}, profileClientError(selectedProfile, err)
		}
		return Runtime{}, err
	}
	// capability loader 会合并原生 skill、Codex plugin 和 MCP 工具，再统一注册进工具表。
	loadedCapabilities, err := capability.Load(ctx, capability.Options{
		Root:              ws.Root,
		Workspace:         ws,
		PluginsEnabled:    cfg.PluginsEnabled,
		MCPServers:        cfg.MCPServers,
		AuthorizeExternal: authorizeExternal,
		Sandbox:           sandboxPolicy,
	})
	if err != nil {
		return Runtime{}, err
	}
	registry := tools.NewDefaultRegistryWithSandbox(ws, sandboxPolicy)
	for _, tool := range loadedCapabilities.Tools {
		if err := registry.RegisterChecked(tool); err != nil {
			_ = capability.CloseClients(loadedCapabilities.MCPClients)
			return Runtime{}, err
		}
	}
	if len(loadedCapabilities.Skills) > 0 {
		if err := registry.RegisterChecked(tools.NewSkillOpenTool(loadedCapabilities.Skills)); err != nil {
			_ = capability.CloseClients(loadedCapabilities.MCPClients)
			return Runtime{}, err
		}
	}
	if loadedCapabilities.SkillContext != "" {
		systemPrompt += "\n\n" + loadedCapabilities.SkillContext
	}
	diffTool := tools.NewGitDiffTool(ws)

	approvalMode := permissions.Mode(firstNonEmpty(sess.ApprovalMode, cfg.ApprovalMode, string(permissions.ModeAuto)))
	childSandbox := sandboxPolicy
	childSandbox.Mode = sandbox.ModeReadOnly
	childRegistry := tools.NewDefaultRegistryWithSandbox(ws, childSandbox)
	if len(loadedCapabilities.Skills) > 0 {
		if err := childRegistry.RegisterChecked(tools.NewSkillOpenTool(loadedCapabilities.Skills)); err != nil {
			_ = capability.CloseClients(loadedCapabilities.MCPClients)
			return Runtime{}, err
		}
	}
	childRunner := &agent.Runner{}
	roles := make([]subagent.Role, 0, len(cfg.SubagentRoles))
	for _, role := range cfg.SubagentRoles {
		roles = append(roles, subagent.Role{Name: role.Name, Description: role.Description, Instructions: role.Instructions})
	}
	subagentManager, err := subagent.NewManagerWithOptions(ctx, ws.Root, func(ctx context.Context, prompt string) (subagent.RunResult, error) {
		result, err := childRunner.RunTurn(ctx, nil, prompt)
		if err != nil {
			return subagent.RunResult{}, err
		}
		return subagent.RunResult{Content: result.FinalText, Transcript: subagentTranscript(result.Messages)}, nil
	}, subagent.Options{MaxConcurrent: cfg.SubagentMaxConcurrent, Roles: roles})
	if err != nil {
		return Runtime{}, err
	}
	if err := registry.RegisterChecked(subagent.NewStartTool(subagentManager)); err != nil {
		_ = subagentManager.Close()
		_ = capability.CloseClients(loadedCapabilities.MCPClients)
		return Runtime{}, err
	}
	if err := registry.RegisterChecked(subagent.NewStatusTool(subagentManager)); err != nil {
		_ = subagentManager.Close()
		_ = capability.CloseClients(loadedCapabilities.MCPClients)
		return Runtime{}, err
	}
	for _, tool := range subagent.NewControlTools(subagentManager) {
		if err := registry.RegisterChecked(tool); err != nil {
			_ = subagentManager.Close()
			_ = capability.CloseClients(loadedCapabilities.MCPClients)
			return Runtime{}, err
		}
	}
	commandSessions := tools.NewCommandSessionManagerWithSandbox(ws, sandboxPolicy)
	for _, tool := range tools.NewCommandSessionTools(commandSessions) {
		if err := registry.RegisterChecked(tool); err != nil {
			_ = commandSessions.Close()
			_ = subagentManager.Close()
			_ = capability.CloseClients(loadedCapabilities.MCPClients)
			return Runtime{}, err
		}
	}
	childPolicy := permissions.Policy(permissions.ModePolicy{Mode: permissions.ModeReadOnly, AllowSearch: opts.NativeSearch})
	runnerPolicy := permissions.Policy(permissions.ModePolicy{Mode: approvalMode, AllowSearch: opts.NativeSearch})
	if len(execRules.Rules) > 0 {
		childPolicy = execpolicy.Policy{Base: childPolicy, Set: execRules}
		runnerPolicy = execpolicy.Policy{Base: runnerPolicy, Set: execRules}
	}
	*childRunner = agent.Runner{
		Model:        client,
		Tools:        childRegistry,
		Policy:       childPolicy,
		Hooks:        hookRunner,
		MaxSteps:     cfg.MaxSteps,
		ModelName:    modelName,
		SystemPrompt: composeSystemPrompt(systemPrompt, sess) + "\n\nSubagent mode:\nYou are a local read-only subagent. Investigate independently, avoid editing files, and return concise findings for the parent agent.",
	}
	runner := agent.Runner{
		Model:        client,
		Tools:        registry,
		Policy:       runnerPolicy,
		Confirmer:    confirmer,
		Hooks:        hookRunner,
		MaxSteps:     cfg.MaxSteps,
		ModelName:    modelName,
		SystemPrompt: composeSystemPrompt(systemPrompt, sess),
	}
	if !opts.Ephemeral {
		if err := store.SaveCurrent(sess); err != nil {
			_ = commandSessions.Close()
			_ = subagentManager.Close()
			_ = capability.CloseClients(loadedCapabilities.MCPClients)
			return Runtime{}, err
		}
	}
	if err := hookRunner.Run(ctx, "SessionStart", hooks.Context{}); err != nil {
		_ = commandSessions.Close()
		_ = subagentManager.Close()
		_ = capability.CloseClients(loadedCapabilities.MCPClients)
		return Runtime{}, err
	}
	return Runtime{
		Config:    cfg,
		Workspace: ws,
		Store:     store,
		Session:   sess,
		Messages:  messages,
		Usage: model.Usage{
			InputTokens:  sess.Usage.InputTokens,
			OutputTokens: sess.Usage.OutputTokens,
			CacheTokens:  sess.Usage.CacheTokens,
			TotalTokens:  sess.Usage.TotalTokens,
		},
		Skills:           loadedCapabilities.Skills,
		MCPClients:       loadedCapabilities.MCPClients,
		Subagents:        subagentManager,
		CommandSessions:  commandSessions,
		Runner:           runner,
		ChildRunner:      childRunner,
		Hooks:            hookRunner,
		BaseSystemPrompt: systemPrompt,
		CompactPrompt:    opts.CompactPrompt,
		NativeSearch:     opts.NativeSearch,
		ProviderProfiles: providerProfiles,
		ActiveProfile:    activeProfile,
		ModelLogPath:     logPath,
		In:               opts.In,
		Out:              opts.Out,
		Err:              opts.Err,
		closer:           &runtimeCloser{},
		Diff: func(ctx context.Context) (string, error) {
			result, err := diffTool.Execute(ctx, json.RawMessage(`{}`))
			return result.Content, err
		},
	}, nil
}

// UseProviderProfile switches the current runtime and child agent to a saved LLM platform.
func (r *Runtime) UseProviderProfile(id string) error {
	if r.ProviderProfiles == nil {
		return fmt.Errorf("provider profiles are unavailable")
	}
	profile, ok := r.ProviderProfiles.Get(strings.TrimSpace(id))
	if !ok {
		return fmt.Errorf("provider profile %q not found", id)
	}
	client, err := provider.NewClient(formattedProviderConfig(profile, profile.Model, r.Workspace.Root, r.ModelLogPath))
	if err != nil {
		return profileClientError(profile, err)
	}
	r.Runner.Model = client
	if r.ChildRunner != nil {
		r.ChildRunner.Model = client
	}
	r.ActiveProfile = profile.ID
	r.Config.Provider = "profile:" + profile.ID
	r.Config.Model = profile.Model
	r.Session.Provider = r.Config.Provider
	r.SetModel(profile.Model)
	return nil
}

func formattedProviderConfig(profile llmprofile.Profile, modelName, root, logPath string) provider.Config {
	apiKey := ""
	if profile.APIKeyEnv != "" {
		apiKey = os.Getenv(profile.APIKeyEnv)
	}
	return provider.Config{
		Provider: "profile:" + profile.ID, Model: modelName,
		APIFormat: string(profile.APIFormat), APIKey: apiKey, BaseURL: profile.BaseURL,
		Root: root, LogPath: logPath,
	}
}

func profileClientError(profile llmprofile.Profile, err error) error {
	requiresAPIKey := profile.APIFormat == llmprofile.FormatAnthropic || profile.APIFormat == llmprofile.FormatGemini
	if requiresAPIKey && profile.APIKeyEnv != "" && strings.TrimSpace(os.Getenv(profile.APIKeyEnv)) == "" {
		return fmt.Errorf("%s is not set for provider profile %q", profile.APIKeyEnv, profile.ID)
	}
	return err
}

func profileReference(providerName string) string {
	providerName = strings.TrimSpace(providerName)
	if !strings.HasPrefix(providerName, "profile:") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(providerName, "profile:"))
}

// SetApprovalMode updates the parent policy while preserving loaded exec rules.
func (r *Runtime) SetApprovalMode(mode permissions.Mode) {
	base := permissions.ModePolicy{Mode: mode, AllowSearch: r.NativeSearch}
	switch current := r.Runner.Policy.(type) {
	case execpolicy.Policy:
		current.Base = base
		r.Runner.Policy = current
	case *execpolicy.Policy:
		current.Base = base
	default:
		r.Runner.Policy = base
	}
}

// Compact 手动压缩当前会话历史，并返回面向用户的结果文案。
func (r *Runtime) Compact(ctx context.Context) (string, error) {
	if len(r.Messages) == 0 {
		return "nothing to compact", nil
	}
	if r.Runner.Model == nil {
		return "", fmt.Errorf("compact requires a model client")
	}
	if r.Hooks != nil {
		if err := r.Hooks.Run(ctx, "PreCompact", hooks.Context{}); err != nil {
			return "", err
		}
	}
	summary, recent, err := summarizer.Summarize(ctx, r.Runner.Model, r.Session.Summary, r.Messages, summarizer.Options{
		MaxMessages: r.Config.SummaryMaxMessages,
		MaxTokens:   r.Config.SummaryMaxTokens,
		KeepRecent:  20,
		Prompt:      r.CompactPrompt,
	})
	if err != nil {
		return "", err
	}
	before := len(r.Messages)
	r.Session.Summary = summary
	r.Messages = recent
	r.Session.Messages = modelMessagesToSession(recent)
	r.RefreshSystemPrompt()
	if err := r.Store.SaveCurrent(r.Session); err != nil {
		return "", err
	}
	if r.Hooks != nil {
		if err := r.Hooks.Run(ctx, "PostCompact", hooks.Context{}); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("compacted messages=%d kept=%d", before, len(recent)), nil
}

// loadOrCreateSession 复用同 workspace/provider 的当前会话，否则创建新的会话状态。
func loadOrCreateSession(store session.Store, workspaceRoot, provider, modelName string) (session.Session, error) {
	sess, err := store.LoadCurrent()
	if err == nil && sess.Workspace == workspaceRoot && sess.Provider == provider {
		if sess.Model == "" {
			sess.Model = modelName
		}
		return sess, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return session.Session{}, fmt.Errorf("load current session: %w", err)
	}
	return session.New(workspaceRoot, provider, modelName), nil
}

func composeSystemPrompt(base string, sess session.Session) string {
	prompt := base
	if sess.Summary != "" {
		prompt += "\n\nConversation summary:\n" + sess.Summary
	}
	if sess.Goal != "" {
		prompt += "\n\nCurrent goal:\n" + sess.Goal
	}
	if sess.Mode == "plan" {
		prompt += "\n\nPlan mode:\nBefore making code changes, propose a concise implementation plan and wait for explicit approval unless the user has already approved the plan."
	}
	return prompt
}

// sessionMessagesToModel 把持久化会话消息转换为模型层消息结构。
func sessionMessagesToModel(messages []session.Message, ws workspace.Workspace) []model.Message {
	out := make([]model.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, model.Message{
			Role:       model.Role(msg.Role),
			Content:    msg.Content,
			Parts:      sessionContentPartsToModel(msg.Parts, ws),
			ToolCallID: msg.ToolCallID,
			ToolCalls:  sessionToolCallsToModel(msg.ToolCalls),
		})
	}
	return out
}

// sessionToolCallsToModel 把会话中的工具调用恢复为模型可发送的工具调用。
func sessionToolCallsToModel(calls []session.ToolCall) []model.ToolCall {
	out := make([]model.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, model.ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: call.Arguments,
		})
	}
	return out
}

// historyMessages 去掉 system 消息，只把可持久化的对话历史写回 session。
func historyMessages(messages []model.Message) []model.Message {
	history := make([]model.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == model.RoleSystem {
			continue
		}
		history = append(history, msg)
	}
	return history
}

// subagentTranscript 保留子代理线程的角色和文本摘要，避免持久化大块图片或工具参数。
func subagentTranscript(messages []model.Message) []subagent.TranscriptMessage {
	out := make([]subagent.TranscriptMessage, 0, len(messages))
	for _, msg := range historyMessages(messages) {
		if msg.Content == "" {
			continue
		}
		out = append(out, subagent.TranscriptMessage{Role: string(msg.Role), Content: msg.Content})
	}
	return out
}

// modelMessagesToSession 清理并转换模型消息，避免非法工具历史进入持久化文件。
func modelMessagesToSession(messages []model.Message) []session.Message {
	sanitized := sanitizeModelMessages(messages)
	out := make([]session.Message, 0, len(sanitized))
	for _, msg := range sanitized {
		out = append(out, session.Message{
			Role:       string(msg.Role),
			Content:    msg.Content,
			Parts:      modelContentPartsToSession(msg.Parts),
			ToolCallID: msg.ToolCallID,
			ToolCalls:  modelToolCallsToSession(msg.ToolCalls),
		})
	}
	return out
}

// sessionContentPartsToModel 把 session 中的多模态内容恢复为模型消息块。
func sessionContentPartsToModel(parts []session.ContentPart, ws workspace.Workspace) []model.ContentPart {
	out := make([]model.ContentPart, 0, len(parts))
	for _, part := range parts {
		restored := model.ContentPart{
			Type:      model.ContentPartType(part.Type),
			Text:      part.Text,
			ImageURL:  part.ImageURL,
			MediaType: part.MediaType,
			Data:      part.Data,
			Path:      part.Path,
		}
		if restored.Type == model.ContentPartImage && restored.ImageURL == "" && restored.Data == "" && restored.Path != "" {
			path, err := ws.Resolve(restored.Path)
			if err != nil {
				restored = model.TextPart("[image unavailable: " + restored.Path + "]")
			} else {
				image, err := model.ImagePartFromFile(path)
				if err != nil {
					restored = model.TextPart("[image unavailable: " + restored.Path + "]")
				} else {
					restored = image
				}
			}
		}
		out = append(out, restored)
	}
	return out
}

// modelContentPartsToSession 把模型多模态内容转换为 session JSON 结构。
func modelContentPartsToSession(parts []model.ContentPart) []session.ContentPart {
	out := make([]session.ContentPart, 0, len(parts))
	for _, part := range parts {
		out = append(out, session.ContentPart{
			Type:      string(part.Type),
			Text:      part.Text,
			MediaType: part.MediaType,
			Path:      part.Path,
		})
	}
	return out
}

// sanitizeModelMessages 移除孤立或不完整的 tool 消息组，避免 provider 拒绝历史请求。
func sanitizeModelMessages(messages []model.Message) []model.Message {
	out := make([]model.Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == model.RoleTool {
			continue
		}
		if msg.Role != model.RoleAssistant || len(msg.ToolCalls) == 0 {
			out = append(out, msg)
			continue
		}

		required := map[string]bool{}
		for _, call := range msg.ToolCalls {
			if call.ID != "" {
				required[call.ID] = true
			}
		}
		if len(required) == 0 {
			out = append(out, msg)
			continue
		}

		group := []model.Message{msg}
		j := i + 1
		for ; j < len(messages); j++ {
			next := messages[j]
			if next.Role != model.RoleTool {
				break
			}
			if next.ToolCallID == "" || !required[next.ToolCallID] {
				break
			}
			delete(required, next.ToolCallID)
			group = append(group, next)
			if len(required) == 0 {
				j++
				break
			}
		}
		if len(required) == 0 {
			out = append(out, group...)
			i = j - 1
			continue
		}
		// 历史里不完整的工具调用组会被 provider 拒绝；丢弃整组，保留后续普通消息。
		for i+1 < len(messages) && messages[i+1].Role == model.RoleTool {
			i++
			if _, ok := required[messages[i].ToolCallID]; ok {
				delete(required, messages[i].ToolCallID)
				if len(required) == 0 {
					break
				}
			}
		}
	}
	return out
}

// modelToolCallsToSession 把模型工具调用转换为可写入 session JSON 的结构。
func modelToolCallsToSession(calls []model.ToolCall) []session.ToolCall {
	out := make([]session.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, session.ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: call.Arguments,
		})
	}
	return out
}

// sessionUsage 把运行时 token 用量转换为 session 持久化格式。
func sessionUsage(usage model.Usage) session.Usage {
	return session.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CacheTokens:  usage.CacheTokens,
		TotalTokens:  usage.TotalTokens,
	}
}

// modelCallLogPath 返回当前 workspace 的模型调用日志路径。
func modelCallLogPath(root string) string {
	return filepath.Join(root, ".codeworld", "logs", "model-calls.jsonl")
}

// loadIndexSummary 读取已有文件索引摘要；索引不存在时静默跳过。
func loadIndexSummary(root string) string {
	idx, err := indexer.Load(indexer.DefaultPath(root))
	if err != nil {
		return ""
	}
	return indexer.Summary(idx, 120)
}

// loadContextGraphSummary 构建轻量上下文图摘要，用于补充 system prompt。
func loadContextGraphSummary(root string, maxFileBytes int) string {
	g, err := contextgraph.Build(root, contextgraph.Options{MaxFileBytes: int64(maxFileBytes)})
	if err != nil {
		return ""
	}
	return g.SummaryText(120)
}

// firstNonEmpty 从候选值中选择满足条件的结果。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
