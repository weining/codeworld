package approvals

import "strings"

type Set struct {
	Commands []string
}

func NormalizeCommand(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

func (s Set) Allows(command string) bool {
	normalized := NormalizeCommand(command)
	for _, item := range s.Commands {
		if NormalizeCommand(item) == normalized {
			return true
		}
	}
	return false
}

func (s *Set) Add(command string) {
	normalized := NormalizeCommand(command)
	if normalized == "" || s.Allows(normalized) {
		return
	}
	s.Commands = append(s.Commands, normalized)
}
