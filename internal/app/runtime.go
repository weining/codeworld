package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"codeworld/internal/agent"
	"codeworld/internal/config"
	"codeworld/internal/context/indexer"
	"codeworld/internal/context/summarizer"
	"codeworld/internal/model"
	"codeworld/internal/model/provider"
	"codeworld/internal/permissions"
	"codeworld/internal/plugin"
	"codeworld/internal/repl"
	"codeworld/internal/session"
	"codeworld/internal/skill"
	"codeworld/internal/tools"
	"codeworld/internal/workspace"
)

type Options struct {
	Root string
	In   io.Reader
	Out  io.Writer
	Err  io.Writer
}

type Runtime struct {
	Config    config.Config
	Workspace workspace.Workspace
	Store     session.Store
	Session   session.Session
	Messages  []model.Message
	Usage     model.Usage
	Skills    []skill.Skill
	Runner    agent.Runner
	Diff      func(context.Context) (string, error)
	In        io.Reader
	Out       io.Writer
	Err       io.Writer
}

func (r *Runtime) SaveTurn(result agent.TurnResult) error {
	r.Messages = historyMessages(result.Messages)
	r.Usage = r.Usage.Add(result.Usage)
	r.Session.Messages = modelMessagesToSession(r.Messages)
	r.Session.Usage = sessionUsage(r.Usage)
	return r.Store.SaveCurrent(r.Session)
}

func (r *Runtime) MaybeSummarize(ctx context.Context) error {
	if r.Runner.Model == nil || !summarizer.ShouldSummarize(r.Messages, r.Usage, summarizer.Options{MaxMessages: r.Config.SummaryMaxMessages}) {
		return nil
	}
	summary, recent, err := summarizer.Summarize(ctx, r.Runner.Model, r.Session.Summary, r.Messages, summarizer.Options{
		MaxMessages: r.Config.SummaryMaxMessages,
		KeepRecent:  20,
	})
	if err != nil {
		return err
	}
	r.Session.Summary = summary
	r.Messages = recent
	r.Session.Messages = modelMessagesToSession(recent)
	return r.Store.SaveCurrent(r.Session)
}

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
	cfg, err := config.Load(root)
	if err != nil {
		return Runtime{}, err
	}
	ws, err := workspace.New(root)
	if err != nil {
		return Runtime{}, err
	}
	if opts.Err != nil && !ws.IsGitRepo() {
		fmt.Fprintln(opts.Err, "warning: current workspace is not a git repository; patch workflows are safer in git repositories")
	}

	summary, err := ws.Summary(120)
	if err != nil {
		summary = "workspace summary unavailable: " + err.Error()
	}
	systemPrompt := agent.DefaultSystemPrompt + "\n\nWorkspace files:\n" + summary
	if indexSummary := loadIndexSummary(ws.Root); indexSummary != "" {
		systemPrompt += "\n\nWorkspace index:\n" + indexSummary
	}
	projectSkills, err := skill.LoadProject(ws.Root)
	if err != nil {
		return Runtime{}, err
	}
	if skillContext := skill.Context(projectSkills); skillContext != "" {
		systemPrompt += "\n\n" + skillContext
	}

	store := session.NewStore(ws.Root)
	sess := loadOrCreateSession(store, ws.Root, cfg.Provider, cfg.Model)
	if sess.Summary != "" {
		systemPrompt += "\n\nConversation summary:\n" + sess.Summary
	}
	messages := sessionMessagesToModel(sess.Messages)
	modelName := firstNonEmpty(sess.Model, cfg.Model)
	client, err := provider.NewClient(provider.Config{
		Provider:        cfg.Provider,
		Model:           modelName,
		DeepSeekAPIKey:  cfg.APIKey,
		OpenAIAPIKey:    cfg.OpenAIAPIKey,
		AnthropicAPIKey: cfg.AnthropicAPIKey,
		LocalBaseURL:    cfg.LocalBaseURL,
		LogPath:         modelCallLogPath(ws.Root),
	})
	if err != nil {
		return Runtime{}, err
	}
	pluginTools, err := plugin.LoadManifests(ws.Root, cfg.PluginsEnabled)
	if err != nil {
		return Runtime{}, err
	}
	registry := tools.NewDefaultRegistry(ws)
	for _, spec := range pluginTools {
		registry.Register(tools.NewPluginTool(ws, spec))
	}
	diffTool := tools.NewGitDiffTool(ws)

	confirmer := repl.Confirmer{In: opts.In, Out: opts.Out}
	runner := agent.Runner{
		Model:        client,
		Tools:        registry,
		Policy:       permissions.AutoPolicy{},
		Confirmer:    confirmer,
		MaxSteps:     cfg.MaxSteps,
		ModelName:    modelName,
		SystemPrompt: systemPrompt,
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
		Skills: projectSkills,
		Runner: runner,
		In:     opts.In,
		Out:    opts.Out,
		Err:    opts.Err,
		Diff: func(ctx context.Context) (string, error) {
			result, err := diffTool.Execute(ctx, json.RawMessage(`{}`))
			return result.Content, err
		},
	}, nil
}

func loadOrCreateSession(store session.Store, workspaceRoot, provider, modelName string) session.Session {
	sess, err := store.LoadCurrent()
	if err == nil && sess.Workspace == workspaceRoot && sess.Provider == provider {
		if sess.Model == "" {
			sess.Model = modelName
		}
		return sess
	}
	return session.New(workspaceRoot, provider, modelName)
}

func sessionMessagesToModel(messages []session.Message) []model.Message {
	out := make([]model.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, model.Message{
			Role:       model.Role(msg.Role),
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  sessionToolCallsToModel(msg.ToolCalls),
		})
	}
	return out
}

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

func modelMessagesToSession(messages []model.Message) []session.Message {
	out := make([]session.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, session.Message{
			Role:       string(msg.Role),
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  modelToolCallsToSession(msg.ToolCalls),
		})
	}
	return out
}

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

func sessionUsage(usage model.Usage) session.Usage {
	return session.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CacheTokens:  usage.CacheTokens,
		TotalTokens:  usage.TotalTokens,
	}
}

func modelCallLogPath(root string) string {
	return filepath.Join(root, ".codeworld", "logs", "model-calls.jsonl")
}

func loadIndexSummary(root string) string {
	idx, err := indexer.Load(indexer.DefaultPath(root))
	if err != nil {
		return ""
	}
	return indexer.Summary(idx, 120)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
