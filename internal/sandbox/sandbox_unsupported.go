//go:build !darwin && !linux

package sandbox

import (
	"fmt"
	"runtime"
)

func platformCommand(policy Policy, cwd, executable string, args []string) (string, []string, error) {
	return "", nil, fmt.Errorf("OS sandbox is not supported on %s", runtime.GOOS)
}
