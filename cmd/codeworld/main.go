package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"codeworld/internal/agent"
	"codeworld/internal/config"
	"codeworld/internal/model"
	"codeworld/internal/model/deepseek"
	"codeworld/internal/permissions"
	"codeworld/internal/repl"
	"codeworld/internal/session"
	"codeworld/internal/tools"
	"codeworld/internal/workspace"
)

func main() {
	if err := runWithIO(os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "codeworld:", err)
		os.Exit(1)
	}
}

func runWithIO(in io.Reader, out io.Writer, stderr io.Writer) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	app, err := newAppWithIO(in, out, stderr, root)
	if err != nil {
		return err
	}
	return app.Run(context.Background())
}

func newAppWithIO(in io.Reader, out io.Writer, stderr io.Writer, root string) (repl.REPL, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return repl.REPL{}, err
	}
	ws, err := workspace.New(root)
	if err != nil {
		return repl.REPL{}, err
	}
	if !ws.IsGitRepo() {
		fmt.Fprintln(stderr, "warning: current workspace is not a git repository; patch workflows are safer in git repositories")
	}

	summary, err := ws.Summary(120)
	if err != nil {
		summary = "workspace summary unavailable: " + err.Error()
	}
	systemPrompt := agent.DefaultSystemPrompt + "\n\nWorkspace files:\n" + summary

	store := session.NewStore(ws.Root)
	sess := loadOrCreateSession(store, ws.Root, cfg.Provider, cfg.Model)
	messages := sessionMessagesToModel(sess.Messages)
	modelName := firstNonEmpty(sess.Model, cfg.Model)
	client := deepseek.NewClient(cfg.APIKey, modelName)
	registry := tools.NewDefaultRegistry(ws)
	diffTool := tools.NewGitDiffTool(ws)

	confirmer := repl.Confirmer{In: in, Out: out}
	runner := agent.Runner{
		Model:        client,
		Tools:        registry,
		Policy:       permissions.ConservativePolicy{},
		Confirmer:    confirmer,
		MaxSteps:     cfg.MaxSteps,
		ModelName:    modelName,
		SystemPrompt: systemPrompt,
	}
	return repl.REPL{
		In:       in,
		Out:      out,
		Runner:   runner,
		Store:    store,
		Session:  sess,
		Messages: messages,
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
