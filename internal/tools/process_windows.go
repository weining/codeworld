//go:build windows

package tools

import "os/exec"

// configureCommandProcessGroup 封装局部逻辑，保持调用方流程清晰。
func configureCommandProcessGroup(cmd *exec.Cmd) {}

// killCommandProcessGroup 封装局部逻辑，保持调用方流程清晰。
func killCommandProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
