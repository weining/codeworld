package main

import (
	"fmt"
	"io"
	"strings"
)

const (
	completionCommands       = "login logout auth mcp mcp-server app-server plugin features debug execpolicy repl run exec e review sessions fork archive unarchive delete index resume tui doctor completion sandbox version help"
	completionOptions        = "--help --version --profile -p --cd -C --add-dir --model -m --image -i --oss --local-provider --approval-mode -a --sandbox -s --network --no-network --search --no-alt-screen --config -c --enable --disable --dangerously-bypass-approvals-and-sandbox --dangerously-bypass-hook-trust --strict-config --json --ephemeral --output-schema --archived --last --uncommitted --title --color"
	completionApprovalModes  = "untrusted on-request never auto read-only full-access"
	completionSandboxModes   = "read-only workspace-write danger-full-access"
	completionLocalProviders = "ollama lmstudio"
	completionColorModes     = "auto always never"
	completionShells         = "bash elvish fish powershell zsh"
)

type completionPath struct {
	path       string
	candidates string
}

var completionPaths = []completionPath{
	{path: "auth", candidates: "codex"},
	{path: "auth;codex", candidates: "login status logout"},
	{path: "mcp", candidates: "list get add remove login logout"},
	{path: "plugin", candidates: "add list remove marketplace"},
	{path: "plugin;marketplace", candidates: "add list upgrade remove"},
	{path: "features", candidates: "list enable disable"},
	{path: "debug", candidates: "models prompt-input"},
	{path: "execpolicy", candidates: "check"},
	{path: "execpolicy;check", candidates: "--rules -r --pretty --resolve-host-executables"},
	{path: "app-server", candidates: "--listen --stdio"},
	{path: "sessions", candidates: "rename --archived"},
	{path: "exec", candidates: "review"},
	{path: "e", candidates: "review"},
	{path: "completion", candidates: completionShells},
}

func runCompletionCommand(out io.Writer, args []string) error {
	shell := "bash"
	if len(args) > 1 {
		return fmt.Errorf("usage: codeworld completion [bash|elvish|fish|powershell|zsh]")
	}
	if len(args) == 1 {
		shell = args[0]
	}

	var script string
	switch shell {
	case "bash":
		script = bashCompletionScript()
	case "elvish":
		script = elvishCompletionScript()
	case "zsh":
		script = zshCompletionScript()
	case "fish":
		script = fishCompletionScript()
	case "powershell":
		script = powershellCompletionScript()
	default:
		return fmt.Errorf("unsupported completion shell %q; use bash, elvish, fish, powershell, or zsh", shell)
	}
	_, err := io.WriteString(out, script)
	return err
}

func bashCompletionScript() string {
	return `# bash completion for codeworld
_codeworld_completion() {
  local current="${COMP_WORDS[COMP_CWORD]}"
  local previous=""
  if (( COMP_CWORD > 0 )); then
    previous="${COMP_WORDS[COMP_CWORD-1]}"
  fi
  case "${previous}" in
    --approval-mode|-a) COMPREPLY=($(compgen -W "` + completionApprovalModes + `" -- "${current}")); return ;;
    --sandbox|-s) COMPREPLY=($(compgen -W "` + completionSandboxModes + `" -- "${current}")); return ;;
    --local-provider) COMPREPLY=($(compgen -W "` + completionLocalProviders + `" -- "${current}")); return ;;
    --color) COMPREPLY=($(compgen -W "` + completionColorModes + `" -- "${current}")); return ;;
  esac
  if (( COMP_CWORD == 1 )); then
    COMPREPLY=($(compgen -W "` + completionCommands + ` ` + completionOptions + `" -- "${current}"))
    return
  fi

  local command="" command_index=-1 i
  for ((i=1; i<COMP_CWORD; i++)); do
    case " ` + completionCommands + ` " in
      *" ${COMP_WORDS[i]} "*) command="${COMP_WORDS[i]}"; command_index=${i}; break ;;
    esac
  done
  local relative=$((COMP_CWORD-command_index))
  local candidates=""
  if (( relative == 1 )); then
    case "${command}" in
      auth) candidates="codex" ;;
      mcp) candidates="list get add remove login logout" ;;
      plugin) candidates="add list remove marketplace" ;;
      features) candidates="list enable disable" ;;
		debug) candidates="models prompt-input" ;;
		execpolicy) candidates="check" ;;
		app-server) candidates="--listen --stdio" ;;
      sessions) candidates="rename --archived" ;;
      exec|e) candidates="review" ;;
      completion) candidates="` + completionShells + `" ;;
    esac
  elif (( relative == 2 )); then
    case "${command}:${COMP_WORDS[command_index+1]}" in
      auth:codex) candidates="login status logout" ;;
		plugin:marketplace) candidates="add list upgrade remove" ;;
		execpolicy:check) candidates="--rules -r --pretty --resolve-host-executables" ;;
    esac
  fi
  if [[ -n "${candidates}" ]]; then
    COMPREPLY=($(compgen -W "${candidates}" -- "${current}"))
  else
    COMPREPLY=($(compgen -W "` + completionOptions + `" -- "${current}"))
  fi
}
complete -F _codeworld_completion codeworld
`
}

func zshCompletionScript() string {
	return `#compdef codeworld
_codeworld() {
  local -a commands
  commands=(` + completionCommands + `)
  if (( CURRENT == 3 )); then
    case "${words[2]}" in
      auth) _values 'auth command' codex; return ;;
      mcp) _values 'mcp command' list get add remove login logout; return ;;
      plugin) _values 'plugin command' add list remove marketplace; return ;;
      features) _values 'feature command' list enable disable; return ;;
      debug) _values 'debug command' models prompt-input; return ;;
	  execpolicy) _values 'execpolicy command' check; return ;;
	  app-server) _values 'app-server option' --listen --stdio; return ;;
      sessions) _values 'sessions argument' rename --archived; return ;;
      exec|e) _values 'exec command' review; return ;;
      completion) _values 'shell' ` + completionShells + `; return ;;
    esac
  elif (( CURRENT == 4 )); then
    case "${words[2]}:${words[3]}" in
      auth:codex) _values 'Codex auth command' login status logout; return ;;
      plugin:marketplace) _values 'marketplace command' add list upgrade remove; return ;;
	  execpolicy:check) _values 'execpolicy option' --rules -r --pretty --resolve-host-executables; return ;;
    esac
  fi
  _arguments \
    '(-p --profile)'{-p,--profile}'[Load a profile]:profile:' \
    '(-C --cd)'{-C,--cd}'[Use a workspace root]:directory:_directories' \
    '*--add-dir[Add an extra workspace root]:directory:_directories' \
    '*'{-i,--image}'[Attach an image to the initial prompt]:file:_files' \
    '(-m --model)'{-m,--model}'[Override the model]:model:' \
    '--oss[Use the local OpenAI-compatible provider]' \
    '--local-provider[Select local provider]:provider:(` + completionLocalProviders + `)' \
    '(-a --approval-mode)'{-a,--approval-mode}'[Override approval mode]:mode:(` + completionApprovalModes + `)' \
    '(-s --sandbox)'{-s,--sandbox}'[Override sandbox mode]:mode:(` + completionSandboxModes + `)' \
    '--color[Set output color]:mode:(` + completionColorModes + `)' \
    '--network[Enable sandbox network access]' \
    '--no-network[Disable sandbox network access]' \
    '--search[Enable native web search]' \
    '--no-alt-screen[Preserve terminal scrollback]' \
    '*'{-c,--config}'[Override config]:key-value:' \
    '*--enable[Enable a feature]:feature:(plugins model_call_logging)' \
    '*--disable[Disable a feature]:feature:(plugins model_call_logging)' \
    '--dangerously-bypass-approvals-and-sandbox[Disable approvals and sandboxing]' \
    '--dangerously-bypass-hook-trust[Run hooks without trust prompts]' \
    '--strict-config[Require strict config parsing]' \
    '1:command:(${commands})' '*::argument:->args'
}
compdef _codeworld codeworld
`
}

func fishCompletionScript() string {
	var script strings.Builder
	script.WriteString("complete -c codeworld -f\n")
	script.WriteString("complete -c codeworld -s p -l profile -r -d 'Load a profile'\n")
	script.WriteString("complete -c codeworld -s C -l cd -r -a '(__fish_complete_directories)' -d 'Use a workspace root'\n")
	script.WriteString("complete -c codeworld -l add-dir -r -a '(__fish_complete_directories)' -d 'Add an extra workspace root'\n")
	script.WriteString("complete -c codeworld -s i -l image -r -d 'Attach an image to the initial prompt'\n")
	script.WriteString("complete -c codeworld -s m -l model -r -d 'Override the model'\n")
	script.WriteString("complete -c codeworld -l oss -d 'Use the local provider'\n")
	script.WriteString("complete -c codeworld -l local-provider -r -a '" + completionLocalProviders + "' -d 'Select local provider'\n")
	script.WriteString("complete -c codeworld -s a -l approval-mode -r -a '" + completionApprovalModes + "' -d 'Override approval mode'\n")
	script.WriteString("complete -c codeworld -s s -l sandbox -r -a '" + completionSandboxModes + "' -d 'Override sandbox mode'\n")
	script.WriteString("complete -c codeworld -l color -r -a '" + completionColorModes + "' -d 'Set output color'\n")
	script.WriteString("complete -c codeworld -l network -d 'Enable sandbox network access'\n")
	script.WriteString("complete -c codeworld -l no-network -d 'Disable sandbox network access'\n")
	script.WriteString("complete -c codeworld -l search -d 'Enable native web search'\n")
	script.WriteString("complete -c codeworld -l no-alt-screen -d 'Preserve terminal scrollback'\n")
	script.WriteString("complete -c codeworld -s c -l config -r -d 'Override config'\n")
	script.WriteString("complete -c codeworld -l enable -r -a 'plugins model_call_logging' -d 'Enable a feature'\n")
	script.WriteString("complete -c codeworld -l disable -r -a 'plugins model_call_logging' -d 'Disable a feature'\n")
	script.WriteString("complete -c codeworld -l dangerously-bypass-approvals-and-sandbox -d 'Disable approvals and sandboxing'\n")
	script.WriteString("complete -c codeworld -l dangerously-bypass-hook-trust -d 'Run hooks without trust prompts'\n")
	script.WriteString("complete -c codeworld -l strict-config -d 'Require strict config parsing'\n")
	for _, command := range splitCompletionCommands() {
		fmt.Fprintf(&script, "complete -c codeworld -n '__fish_use_subcommand' -a %s\n", command)
	}
	for _, path := range completionPaths {
		parts := strings.Split(path.path, ";")
		condition := "__fish_seen_subcommand_from " + strings.Join(parts, "; and __fish_seen_subcommand_from ")
		condition += "; and not __fish_seen_subcommand_from " + path.candidates
		fmt.Fprintf(&script, "complete -c codeworld -n '%s' -a '%s'\n", condition, path.candidates)
	}
	return script.String()
}

func powershellCompletionScript() string {
	return `Register-ArgumentCompleter -Native -CommandName codeworld -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)
  $elements = @($commandAst.CommandElements | ForEach-Object { $_.Extent.Text })
  $currentIsElement = $wordToComplete -ne '' -and $elements.Count -gt 1 -and $elements[-1] -eq $wordToComplete
  $previousIndex = if ($currentIsElement) { $elements.Count - 2 } else { $elements.Count - 1 }
  $previous = if ($previousIndex -gt 0) { $elements[$previousIndex] } else { '' }
  $candidates = switch ($previous) {
    { $_ -in '--approval-mode', '-a' } { '` + completionApprovalModes + `'.Split(' '); break }
    { $_ -in '--sandbox', '-s' } { '` + completionSandboxModes + `'.Split(' '); break }
    '--local-provider' { '` + completionLocalProviders + `'.Split(' '); break }
    '--color' { '` + completionColorModes + `'.Split(' '); break }
    default {
      if ($elements.Count -le 1) { '` + completionCommands + ` ` + completionOptions + `'.Split(' '); break }
      switch ($elements[1]) {
        'auth' { if ($elements.Count -le 2 -or ($elements.Count -eq 3 -and $currentIsElement)) { 'codex'.Split(' ') } elseif ($elements[2] -eq 'codex') { 'login status logout'.Split(' ') } }
        'mcp' { 'list get add remove login logout'.Split(' ') }
        'plugin' { if ($elements[2] -eq 'marketplace') { 'add list upgrade remove'.Split(' ') } else { 'add list remove marketplace'.Split(' ') } }
        'features' { 'list enable disable'.Split(' ') }
        'debug' { 'models prompt-input'.Split(' ') }
		'execpolicy' { if ($elements.Count -le 2 -or ($elements.Count -eq 3 -and $currentIsElement)) { 'check'.Split(' ') } else { '--rules -r --pretty --resolve-host-executables'.Split(' ') } }
		'app-server' { '--listen --stdio'.Split(' ') }
        'sessions' { 'rename --archived'.Split(' ') }
        { $_ -in 'exec', 'e' } { 'review'.Split(' ') }
        'completion' { '` + completionShells + `'.Split(' ') }
        default { '` + completionOptions + `'.Split(' ') }
      }
    }
  }
  $candidates |
    Where-Object { $_ -like "$wordToComplete*" } |
    ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }
}
`
}

func elvishCompletionScript() string {
	var script strings.Builder
	script.WriteString("use builtin;\nuse str;\n\n")
	script.WriteString("set edit:completion:arg-completer[codeworld] = {|@words|\n")
	script.WriteString("    fn cand {|text| edit:complex-candidate $text &display=$text }\n")
	script.WriteString("    var command = 'codeworld'\n")
	script.WriteString("    for word $words[1..-1] {\n")
	script.WriteString("        if (str:has-prefix $word '-') { break }\n")
	script.WriteString("        set command = $command';'$word\n")
	script.WriteString("    }\n")
	script.WriteString("    var completions = [\n")
	script.WriteString("        &'codeworld'= {\n")
	for _, candidate := range strings.Fields(completionCommands + " " + completionOptions) {
		fmt.Fprintf(&script, "            cand %s\n", candidate)
	}
	script.WriteString("        }\n")
	for _, path := range completionPaths {
		fmt.Fprintf(&script, "        &'codeworld;%s'= {\n", path.path)
		for _, candidate := range strings.Fields(path.candidates) {
			fmt.Fprintf(&script, "            cand %s\n", candidate)
		}
		script.WriteString("        }\n")
	}
	script.WriteString("    ]\n")
	script.WriteString("    if (has-key $completions $command) { $completions[$command] }\n")
	script.WriteString("}\n")
	return script.String()
}

func splitCompletionCommands() []string {
	return strings.Fields(completionCommands)
}
