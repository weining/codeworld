package main

import (
	"fmt"
	"io"
)

const completionCommands = "login logout auth mcp plugin features debug repl run exec e review sessions fork archive unarchive delete index resume tui doctor completion sandbox version help"

func runCompletionCommand(out io.Writer, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: codeworld completion <bash|zsh|fish|powershell>")
	}
	var script string
	switch args[0] {
	case "bash":
		script = `# bash completion for codeworld
_codeworld_completion() {
  local current="${COMP_WORDS[COMP_CWORD]}"
  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=($(compgen -W "` + completionCommands + `" -- "${current}"))
  else
    COMPREPLY=($(compgen -W "--help --version --profile --cd --model --approval-mode --sandbox --network --no-network --search --config --enable --disable --strict-config --json --ephemeral --image --output-schema --archived --last --uncommitted --title" -- "${current}"))
  fi
}
complete -F _codeworld_completion codeworld
`
	case "zsh":
		script = `#compdef codeworld
_codeworld() {
  local -a commands
  commands=(` + completionCommands + `)
  _arguments \
    '(-p --profile)'{-p,--profile}'[Load a profile]:profile:' \
    '(-C --cd)'{-C,--cd}'[Use a workspace root]:directory:_directories' \
    '(-m --model)'{-m,--model}'[Override the model]:model:' \
    '(-a --approval-mode)'{-a,--approval-mode}'[Override approval mode]:mode:(auto read-only full-access)' \
    '(-s --sandbox)'{-s,--sandbox}'[Override sandbox mode]:mode:(read-only workspace-write danger-full-access)' \
    '--network[Enable sandbox network access]' \
    '--no-network[Disable sandbox network access]' \
    '--search[Enable native web search]' \
    '*'{-c,--config}'[Override config]:key-value:' \
    '*--enable[Enable a feature]:feature:(plugins model_call_logging)' \
    '*--disable[Disable a feature]:feature:(plugins model_call_logging)' \
    '--strict-config[Require strict config parsing]' \
    '1:command:(${commands})' '*::argument:->args'
}
compdef _codeworld codeworld
`
	case "fish":
		script = "complete -c codeworld -f\n"
		script += "complete -c codeworld -s p -l profile -r -d 'Load a profile'\n"
		script += "complete -c codeworld -s C -l cd -r -a '(__fish_complete_directories)' -d 'Use a workspace root'\n"
		script += "complete -c codeworld -s m -l model -r -d 'Override the model'\n"
		script += "complete -c codeworld -s a -l approval-mode -r -a 'auto read-only full-access' -d 'Override approval mode'\n"
		script += "complete -c codeworld -s s -l sandbox -r -a 'read-only workspace-write danger-full-access' -d 'Override sandbox mode'\n"
		script += "complete -c codeworld -l network -d 'Enable sandbox network access'\n"
		script += "complete -c codeworld -l no-network -d 'Disable sandbox network access'\n"
		script += "complete -c codeworld -l search -d 'Enable native web search'\n"
		script += "complete -c codeworld -s c -l config -r -d 'Override config'\n"
		script += "complete -c codeworld -l enable -r -a 'plugins model_call_logging' -d 'Enable a feature'\n"
		script += "complete -c codeworld -l disable -r -a 'plugins model_call_logging' -d 'Disable a feature'\n"
		script += "complete -c codeworld -l strict-config -d 'Require strict config parsing'\n"
		for _, command := range splitCompletionCommands() {
			script += fmt.Sprintf("complete -c codeworld -n '__fish_use_subcommand' -a %s\n", command)
		}
	case "powershell":
		script = `Register-ArgumentCompleter -Native -CommandName codeworld -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)
  '` + completionCommands + ` --profile -p --cd -C --model -m --approval-mode -a --sandbox -s --network --no-network --search --config -c --enable --disable --strict-config --version -V --uncommitted --title'.Split(' ') |
    Where-Object { $_ -like "$wordToComplete*" } |
    ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }
}
`
	default:
		return fmt.Errorf("unsupported completion shell %q; use bash, zsh, fish, or powershell", args[0])
	}
	_, err := io.WriteString(out, script)
	return err
}

func splitCompletionCommands() []string {
	return []string{"login", "logout", "auth", "mcp", "plugin", "features", "debug", "repl", "run", "exec", "e", "review", "sessions", "fork", "archive", "unarchive", "delete", "index", "resume", "tui", "doctor", "completion", "sandbox", "version", "help"}
}
