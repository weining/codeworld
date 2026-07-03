//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// configureCommandProcessGroup 封装局部逻辑，保持调用方流程清晰。
func configureCommandProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killCommandProcessGroup 封装局部逻辑，保持调用方流程清晰。
func killCommandProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
