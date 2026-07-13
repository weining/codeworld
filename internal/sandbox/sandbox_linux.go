//go:build linux

package sandbox

import (
	"fmt"
	"os/exec"
)

func platformCommand(policy Policy, cwd, executable string, args []string) (string, []string, error) {
	backend, err := exec.LookPath("bwrap")
	if err != nil {
		return "", nil, fmt.Errorf("Linux sandbox backend unavailable: install bubblewrap (bwrap)")
	}
	wrapped := []string{
		"--die-with-parent",
		"--new-session",
		"--ro-bind", "/", "/",
		"--dev-bind", "/dev", "/dev",
		"--proc", "/proc",
	}
	if policy.Mode == ModeWorkspaceWrite {
		wrapped = append(wrapped, "--bind", policy.Workspace, policy.Workspace)
		for _, root := range policy.WritableRoots {
			wrapped = append(wrapped, "--bind", root, root)
		}
	}
	if !policy.Network {
		wrapped = append(wrapped, "--unshare-net")
	}
	wrapped = append(wrapped, "--chdir", cwd, "--", executable)
	wrapped = append(wrapped, args...)
	return backend, wrapped, nil
}
