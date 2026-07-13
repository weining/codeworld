//go:build darwin

package sandbox

import (
	"fmt"
	"os/exec"
	"strings"
)

func platformCommand(policy Policy, cwd, executable string, args []string) (string, []string, error) {
	backend, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return "", nil, fmt.Errorf("macOS sandbox backend unavailable: sandbox-exec not found")
	}
	profile := `(version 1)(allow default)(deny file-write*)`
	if !policy.Network {
		profile += `(deny network*)`
	}
	if policy.Mode == ModeWorkspaceWrite {
		profile += `(allow file-write* (subpath "` + escapeProfileString(policy.Workspace) + `"))`
		for _, root := range policy.WritableRoots {
			profile += `(allow file-write* (subpath "` + escapeProfileString(root) + `"))`
		}
	}
	wrapped := []string{"-p", profile, "--", executable}
	wrapped = append(wrapped, args...)
	return backend, wrapped, nil
}

func escapeProfileString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
