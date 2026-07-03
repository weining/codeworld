package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Context struct {
	Tool   string
	Target string
	Risk   string
}

type Runner struct {
	root   string
	events map[string][]Matcher
}

type Matcher struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []CommandHook `json:"hooks"`
}

type CommandHook struct {
	Type          string `json:"type"`
	Command       string `json:"command"`
	Timeout       int    `json:"timeout,omitempty"`
	StatusMessage string `json:"statusMessage,omitempty"`
}

type fileConfig struct {
	Hooks map[string][]Matcher `json:"hooks"`
}

// Load 读取 workspace 的 .codeworld/hooks.json；不存在时返回空 runner。
func Load(root string) (*Runner, error) {
	path := filepath.Join(root, ".codeworld", "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Runner{root: root, events: map[string][]Matcher{}}, nil
		}
		return nil, err
	}
	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Hooks == nil {
		cfg.Hooks = map[string][]Matcher{}
	}
	return &Runner{root: root, events: cfg.Hooks}, nil
}

// Run 执行指定事件匹配到的 command hooks。
func (r *Runner) Run(ctx context.Context, event string, hookCtx Context) error {
	if r == nil {
		return nil
	}
	for _, group := range r.events[event] {
		if group.Matcher != "" && group.Matcher != "*" && group.Matcher != hookCtx.Tool {
			continue
		}
		for _, hook := range group.Hooks {
			if hook.Type != "command" || hook.Command == "" {
				continue
			}
			cmd := exec.CommandContext(ctx, "sh", "-c", hook.Command)
			cmd.Dir = r.root
			cmd.Env = append(os.Environ(),
				"CODEWORLD_HOOK_EVENT="+event,
				"CODEWORLD_HOOK_TOOL="+hookCtx.Tool,
				"CODEWORLD_HOOK_TARGET="+hookCtx.Target,
				"CODEWORLD_HOOK_RISK="+hookCtx.Risk,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("hook %s failed: %w: %s", event, err, out)
			}
		}
	}
	return nil
}
