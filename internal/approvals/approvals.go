package approvals

import "strings"

type Set struct {
	Commands []string
}

// NormalizeCommand 规范化输入，减少等价写法对后续判断的影响。
func NormalizeCommand(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

// Allows 判断输入是否满足特定条件，并用于后续分支决策。
func (s Set) Allows(command string) bool {
	normalized := NormalizeCommand(command)
	for _, item := range s.Commands {
		if NormalizeCommand(item) == normalized {
			return true
		}
	}
	return false
}

// Add 提供对外可复用的能力，并隐藏内部实现细节。
func (s *Set) Add(command string) {
	normalized := NormalizeCommand(command)
	if normalized == "" || s.Allows(normalized) {
		return
	}
	s.Commands = append(s.Commands, normalized)
}
