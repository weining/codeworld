package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"codeworld/internal/app"
	"codeworld/internal/model"
)

type Options struct {
	TestMode      bool
	InitialPrompt string
	InitialImages []model.ContentPart
}

type Status struct {
	Workspace string
	Provider  string
	Model     string
	Messages  int
	Approvals int
	Usage     model.Usage
	Git       string
	Running   bool
	Sandbox   string
	Network   bool
}

// RuntimeStatus 执行主要流程，并把运行结果或错误返回给调用方。
func RuntimeStatus(rt *app.Runtime) Status {
	return Status{
		Workspace: rt.Workspace.Root,
		Provider:  rt.Session.Provider,
		Model:     rt.Session.Model,
		Messages:  len(rt.Messages),
		Approvals: len(rt.Session.Approvals),
		Usage:     rt.Usage,
		Git:       gitState(rt.Workspace.Root),
		Sandbox:   rt.Config.SandboxMode,
		Network:   rt.Config.SandboxNetwork,
	}
}

// StatusLine 提供对外可复用的能力，并隐藏内部实现细节。
func StatusLine(status Status) string {
	running := ""
	if status.Running {
		running = " running"
	}
	return fmt.Sprintf("workspace=%s provider=%s model=%s git=%s sandbox=%s network=%t messages=%d approvals=%d tokens input=%d output=%d cache=%d total=%d%s",
		status.Workspace,
		status.Provider,
		status.Model,
		status.Git,
		status.Sandbox,
		status.Network,
		status.Messages,
		status.Approvals,
		status.Usage.InputTokens,
		status.Usage.OutputTokens,
		status.Usage.CacheTokens,
		status.Usage.TotalTokens,
		running,
	)
}

// gitState 封装局部逻辑，保持调用方流程清晰。
func gitState(root string) string {
	if root == "" {
		return "none"
	}
	cmd := exec.Command("git", "-C", root, "status", "--short")
	out, err := cmd.Output()
	if err != nil {
		return "none"
	}
	if strings.TrimSpace(string(out)) == "" {
		return "clean"
	}
	return "dirty"
}

// Run 执行主要流程，并把运行结果或错误返回给调用方。
func Run(ctx context.Context, rt *app.Runtime) error {
	return RunWithOptions(ctx, rt, Options{})
}

// RunWithOptions 执行主要流程，并把运行结果或错误返回给调用方。
func RunWithOptions(ctx context.Context, rt *app.Runtime, opts Options) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := NewModel(rt)
	m.ctx = runCtx
	if prompt := strings.TrimSpace(opts.InitialPrompt); prompt != "" {
		m.initialTurn = &queuedTurn{text: prompt, images: append([]model.ContentPart(nil), opts.InitialImages...)}
	}
	if opts.TestMode {
		_, err := fmt.Fprint(rt.Out, m.View())
		return err
	}
	programOpts := []tea.ProgramOption{tea.WithContext(ctx), tea.WithAltScreen()}
	_, err := tea.NewProgram(m, programOpts...).Run()
	return err
}
