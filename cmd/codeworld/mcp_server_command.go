package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"codeworld/internal/app"
	"codeworld/internal/config"
	"codeworld/internal/mcpserver"
	runmode "codeworld/internal/run"
	"codeworld/internal/sandbox"
)

type codeworldMCPRunner struct {
	root      string
	global    globalOptions
	store     mcpThreadStore
	runThread func(context.Context, mcpThread, string) (mcpserver.Response, error)
	mu        sync.Mutex
	threads   map[string]mcpThread
}

type mcpThread struct {
	options               app.Options
	baseInstructions      string
	developerInstructions string
}

func runMCPServerCommand(ctx context.Context, in io.Reader, out io.Writer, root string, global globalOptions, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: codeworld mcp-server")
	}
	home, err := config.Home("")
	if err != nil {
		return err
	}
	runner := &codeworldMCPRunner{root: root, global: global, store: newMCPThreadStore(home), threads: map[string]mcpThread{}}
	return (mcpserver.Server{Runner: runner, Version: version}).Serve(ctx, in, out)
}

func (r *codeworldMCPRunner) Start(ctx context.Context, req mcpserver.StartRequest) (mcpserver.Response, error) {
	root := r.root
	if req.CWD != "" {
		var err error
		root, err = resolveGlobalWorkingDir(root, req.CWD)
		if err != nil {
			return mcpserver.Response{}, err
		}
	}
	opts := runtimeOptionsFromGlobal(root, r.global, nil, io.Discard, io.Discard)
	opts.NewSession = true
	opts.CompactPrompt = req.CompactPrompt
	if req.Model != "" {
		opts.Model = req.Model
	}
	if req.ApprovalPolicy != "" {
		mode, err := parseApprovalMode(req.ApprovalPolicy)
		if err != nil {
			return mcpserver.Response{}, err
		}
		opts.ApprovalMode = mode
	}
	if req.Sandbox != "" {
		opts.SandboxMode = sandbox.Mode(req.Sandbox)
		switch opts.SandboxMode {
		case sandbox.ModeReadOnly, sandbox.ModeWorkspaceWrite, sandbox.ModeDangerFullAccess:
		default:
			return mcpserver.Response{}, fmt.Errorf("invalid sandbox mode %q", req.Sandbox)
		}
		if opts.SandboxMode == sandbox.ModeDangerFullAccess {
			opts.SandboxNetwork = nil
		}
	}
	overrides, err := mcpConfigOverrides(req.Config)
	if err != nil {
		return mcpserver.Response{}, err
	}
	opts.ConfigOverrides = append(opts.ConfigOverrides, overrides...)
	state := mcpThread{options: opts, baseInstructions: req.BaseInstructions, developerInstructions: req.DeveloperInstructions}
	response, err := r.execute(ctx, state, req.Prompt)
	if err != nil {
		return mcpserver.Response{}, err
	}
	state.options.NewSession = false
	state.options.SessionID = response.ThreadID
	if err := r.store.Save(response.ThreadID, state); err != nil {
		return mcpserver.Response{}, fmt.Errorf("persist MCP thread: %w", err)
	}
	r.mu.Lock()
	r.threads[response.ThreadID] = state
	r.mu.Unlock()
	return response, nil
}

func (r *codeworldMCPRunner) Reply(ctx context.Context, req mcpserver.ReplyRequest) (mcpserver.Response, error) {
	threadID := req.ThreadID
	if threadID == "" {
		threadID = req.ConversationID
	}
	r.mu.Lock()
	state, ok := r.threads[threadID]
	r.mu.Unlock()
	if !ok {
		var err error
		state, err = r.store.Load(threadID)
		if err != nil {
			if os.IsNotExist(err) {
				return mcpserver.Response{}, fmt.Errorf("thread %q is not available", threadID)
			}
			return mcpserver.Response{}, fmt.Errorf("restore MCP thread %q: %w", threadID, err)
		}
		r.mu.Lock()
		r.threads[threadID] = state
		r.mu.Unlock()
	}
	return r.execute(ctx, state, req.Prompt)
}

func (r *codeworldMCPRunner) execute(ctx context.Context, state mcpThread, prompt string) (mcpserver.Response, error) {
	if r.runThread != nil {
		return r.runThread(ctx, state, prompt)
	}
	return runMCPThread(ctx, state, prompt)
}

func runMCPThread(ctx context.Context, state mcpThread, prompt string) (response mcpserver.Response, err error) {
	rt, err := app.NewRuntime(ctx, state.options)
	if err != nil {
		return mcpserver.Response{}, err
	}
	defer func() { err = errors.Join(err, rt.Close()) }()
	applyMCPInstructions(&rt, state.baseInstructions, state.developerInstructions)
	result, err := runmode.Execute(ctx, &rt, prompt, runmode.Options{})
	if err != nil {
		return mcpserver.Response{}, err
	}
	return mcpserver.Response{ThreadID: result.SessionID, Content: result.FinalText}, nil
}

func applyMCPInstructions(rt *app.Runtime, base, developer string) {
	if strings.TrimSpace(base) != "" {
		rt.Runner.SystemPrompt = strings.TrimSpace(base)
	}
	if strings.TrimSpace(developer) != "" {
		rt.Runner.SystemPrompt = strings.TrimSpace(rt.Runner.SystemPrompt) + "\n\nDeveloper instructions:\n" + strings.TrimSpace(developer)
	}
}

func mcpConfigOverrides(values map[string]any) ([]string, error) {
	flat := map[string]any{}
	flattenMCPConfig("", values, flat)
	keys := make([]string, 0, len(flat))
	for key := range flat {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("MCP config override key is empty")
		}
		data, err := json.Marshal(flat[key])
		if err != nil {
			return nil, fmt.Errorf("encode MCP config override %q: %w", key, err)
		}
		out = append(out, key+"="+string(data))
	}
	return out, nil
}

func flattenMCPConfig(prefix string, values map[string]any, out map[string]any) {
	for key, value := range values {
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		if nested, ok := value.(map[string]any); ok {
			flattenMCPConfig(name, nested, out)
			continue
		}
		out[name] = value
	}
}
