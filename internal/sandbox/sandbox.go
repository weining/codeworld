package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Mode 描述本地进程可以写入的文件系统范围。
type Mode string

const (
	ModeReadOnly         Mode = "read-only"
	ModeWorkspaceWrite   Mode = "workspace-write"
	ModeDangerFullAccess Mode = "danger-full-access"
)

// Policy 是所有模型触发本地进程共用的 OS 沙箱策略。
type Policy struct {
	Mode      Mode
	Workspace string
	Network   bool
}

// Default 返回适合交互式编码的最小权限策略。
func Default(workspace string) Policy {
	return Policy{Mode: ModeWorkspaceWrite, Workspace: workspace}
}

// FullAccess 返回兼容旧构造函数的宿主机直连策略。
func FullAccess(workspace string) Policy {
	return Policy{Mode: ModeDangerFullAccess, Workspace: workspace, Network: true}
}

// Validate 校验策略，不探测或启动平台后端。
func (p Policy) Validate() error {
	switch p.Mode {
	case ModeReadOnly, ModeWorkspaceWrite, ModeDangerFullAccess:
	default:
		return fmt.Errorf("invalid sandbox mode %q", p.Mode)
	}
	if strings.TrimSpace(p.Workspace) == "" {
		return fmt.Errorf("sandbox workspace is required")
	}
	if !filepath.IsAbs(p.Workspace) {
		return fmt.Errorf("sandbox workspace must be absolute")
	}
	return nil
}

// CommandContext 创建受策略约束的命令；后端缺失时直接返回错误。
func (p Policy) CommandContext(ctx context.Context, cwd, executable string, args ...string) (*exec.Cmd, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.Mode == ModeDangerFullAccess {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Dir = cwd
		return cmd, nil
	}
	wrappedExecutable, wrappedArgs, err := platformCommand(p, cwd, executable, args)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, wrappedExecutable, wrappedArgs...)
	cmd.Dir = cwd
	return cmd, nil
}

// Command 创建可交互的受限命令，供 PTY 和后台会话使用。
func (p Policy) Command(cwd, executable string, args ...string) (*exec.Cmd, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.Mode == ModeDangerFullAccess {
		cmd := exec.Command(executable, args...)
		cmd.Dir = cwd
		return cmd, nil
	}
	wrappedExecutable, wrappedArgs, err := platformCommand(p, cwd, executable, args)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(wrappedExecutable, wrappedArgs...)
	cmd.Dir = cwd
	return cmd, nil
}
