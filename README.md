# Codeworld

Codeworld is a local coding agent written in Go. It is designed as a small,
inspectable foundation for a Codex- or Claude Code-style terminal assistant.

The current version focuses on:

- a Codex-like Bubble Tea TUI as the default interactive entry point;
- DeepSeek by default, with user-managed OpenAI-compatible, Anthropic Messages, native Gemini, and Codex OAuth provider profiles;
- streaming assistant output for OpenAI-compatible, Gemini, DeepSeek, and Codex providers;
- workspace-safe tools for reading, writing, patching, shell commands, git, context search, and native web search;
- project skills, plugin tools, stdio/HTTP MCP tools, hooks, and local subagents;
- scriptable JSONL execution, structured JSON output, and local Git review;
- PTY-capable background command sessions with incremental output, stdin, resize, and termination;
- session persistence, image input, token accounting, and model call logs.

Chinese documentation is available in [README.zh-CN.md](README.zh-CN.md).

## Status

Codeworld is usable as a local coding agent, but it is still intentionally
smaller than Codex. The current implementation covers a Codex-like local TUI,
provider calls, workspace tools, native web search, permissions, skills,
plugins, stdio/HTTP MCP, hooks, local subagents, image input, and deterministic
context. Larger Codex-style surfaces such as cloud tasks, IDE integration,
browser control, computer use, and hosted GitHub review workflows are future work.

## Install

This project requires Go 1.26.4 or newer.

```bash
go test ./...
go install ./cmd/codeworld
```

Make sure the Go bin directory is on your `PATH`.

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

## Configure

Codeworld layers user configuration from `$CODEWORLD_HOME/config.toml`
(default `~/.codeworld/config.toml`), project configuration from
`.codeworld/config.toml`, and an optional profile from
`$CODEWORLD_HOME/<name>.config.toml`. The legacy
`$CODEWORLD_HOME/profiles/<name>.toml` path remains a fallback. Project values override user values;
an explicitly selected profile overrides both. API-key providers read keys
from the environment; Codex OAuth uses a local login file.

```bash
export DEEPSEEK_API_KEY="sk-..."
```

Minimal config:

```toml
provider = "deepseek"
model = "deepseek-v4-pro"
max_steps = 20
plugins_enabled = true
summary_max_messages = 40
summary_max_tokens = 65536
index_max_file_bytes = 262144
model_call_logging = false
sandbox_mode = "workspace-write"
sandbox_network = false
subagent_max_concurrent = 4

[[subagent_roles]]
name = "reviewer"
description = "Review changes for regressions"
instructions = "Prioritize correctness, security, and missing tests."
```

Select a profile globally or through the environment:

```bash
codeworld --profile fast
codeworld --profile review exec "inspect this repository"
export CODEWORLD_PROFILE=fast
```

Project configuration cannot enable approval `full-access`, sandbox
`danger-full-access`, or sandbox network access. Those trust expansions must
come from a user/profile config or explicit `exec` flags.

MCP servers can also be configured in the same file:

```toml
[[mcp_servers]]
name = "demo"
command = "node"
args = ["server.js"]
```

Servers added with `codeworld mcp add` are stored atomically in
`$CODEWORLD_HOME/mcp.toml`. This managed user layer overrides same-name entries
in user `config.toml`; project config and an explicitly selected profile can
still override it.

Provider environment variables:

```bash
DEEPSEEK_API_KEY=...
OPENAI_API_KEY=...
ANTHROPIC_API_KEY=...
CODEWORLD_LOCAL_BASE_URL=http://127.0.0.1:11434/v1
```

### LLM provider profiles

Use `/providers add` in the TUI to configure an LLM platform one field at a
time. Profiles are stored atomically at `$CODEWORLD_HOME/providers.json` with
`0600` permissions. A profile stores only the API key environment-variable
name, never the key value itself.

Supported wire formats:

- `openai`: OpenAI Chat Completions-compatible APIs, including OpenAI,
  DeepSeek, Moonshot/Kimi, Qwen/DashScope, Zhipu, OpenRouter, Ollama, LM Studio,
  vLLM, and compatible gateways by changing the base URL;
- `anthropic`: native Anthropic Messages-compatible APIs;
- `gemini`: native Google Gemini `generateContent` and
  `streamGenerateContent`;
- `codex`: ChatGPT/Codex OAuth Responses backend.

For example, this user-level file describes Moonshot and local Ollama:

```json
{
  "version": 1,
  "profiles": [
    {
      "id": "moonshot",
      "name": "Moonshot",
      "api_format": "openai",
      "base_url": "https://api.moonshot.cn/v1",
      "model": "moonshot-v1-8k",
      "api_key_env": "MOONSHOT_API_KEY"
    },
    {
      "id": "ollama",
      "api_format": "openai",
      "base_url": "http://127.0.0.1:11434/v1",
      "model": "qwen3-coder"
    }
  ]
}
```

Select a profile persistently in `config.toml` with
`provider = "profile:moonshot"`, or switch the current TUI session immediately
with `/providers use moonshot`.

Codex OAuth, using the same ChatGPT OAuth shape as OpenClaw:

```bash
codeworld auth codex login
```

Then configure:

```toml
provider = "codex"
model = "gpt-5"
```

The login flow uses `auth.openai.com`, PKCE, `localhost:1455/auth/callback`,
and stores credentials at `.codeworld/auth/codex.json` with `0600`
permissions. Runtime calls go to the ChatGPT backend Codex Responses endpoint,
not the public OpenAI API key endpoint.

The Codex provider honors explicit `HTTPS_PROXY`/`HTTP_PROXY` environment
settings. On macOS, when those variables are absent, it also reads the active
system HTTPS proxy from `scutil --proxy`; this keeps terminal requests aligned
with proxy-enabled desktop traffic.

## Use

Start the TUI:

```bash
codeworld
```

Explicit commands:

```bash
codeworld tui
codeworld auth codex login
codeworld resume --last
codeworld resume <session-id>
codeworld repl
codeworld run "inspect the project and explain the entry points"
codeworld run --image screenshot.png "explain this screenshot"
codeworld exec --json "summarize the repository"
printf 'inspect this repository' | codeworld exec --ephemeral -
codeworld exec --output-schema schema.json -o result.json "extract project metadata"
codeworld exec --approval-mode read-only "inspect without changing files"
codeworld exec resume --last "continue with the next task"
codeworld review
codeworld review --base main
codeworld review --commit HEAD
codeworld mcp add docs --url https://example.com/mcp
codeworld mcp add local -- node server.js --stdio
codeworld mcp list --json
codeworld mcp login docs --scopes tools.read,tools.write
codeworld mcp logout docs
codeworld mcp remove local
codeworld mcp-server
codeworld app-server --stdio
codeworld execpolicy check --pretty --rules ~/.codeworld/rules/default.rules -- git status --short
codeworld sessions
codeworld doctor
codeworld doctor --json
codeworld completion zsh > ~/.zfunc/_codeworld
codeworld auth codex status
codeworld auth codex logout
codeworld login status
codeworld logout
codeworld --version
codeworld sandbox -- go test ./...
codeworld sandbox --sandbox read-only --no-network -- git status
codeworld -C ../another-project -m gpt-5 exec "inspect this workspace"
codeworld -s read-only -a read-only --no-network repl
codeworld --search exec "check the latest upstream documentation"
codeworld --oss --local-provider ollama -m qwen3-coder exec "inspect this workspace"
codeworld exec --add-dir ../shared "update the shared schema"
codeworld exec --dangerously-bypass-hook-trust "run trusted automation hooks"
codeworld -c 'model="gpt-5"' -c max_steps=30 e "inspect this workspace"
codeworld exec review --uncommitted --title "Working tree"
codeworld fork --last
codeworld archive <session-id>
codeworld unarchive <session-id>
codeworld delete <session-id>
codeworld index
```

Command behavior:

- `codeworld` starts the TUI when attached to a terminal.
- `codeworld tui` starts the TUI explicitly.
- `codeworld auth codex login` signs in with ChatGPT/Codex OAuth for `provider = "codex"`.
- `codeworld resume --last` resumes the newest active saved session.
- `codeworld resume <session-id>` resumes a specific active session.
- `codeworld repl` starts the line-oriented REPL.
- `codeworld run <task>` runs one non-interactive turn and exits.
- `codeworld run --image <path> <task>` attaches an image to a non-interactive turn.
- `codeworld exec <task|->` runs a script-friendly turn; `-` reads the prompt from stdin.
- `codeworld exec --json` emits lifecycle, usage, and typed command/file/MCP/web events as JSONL.
- `codeworld exec --ephemeral` avoids loading or saving the current session.
- `codeworld exec resume <session-id|--last> <task|->` continues a saved session non-interactively.
- `codeworld exec --approval-mode <auto|read-only|full-access>` overrides prompt/approval behavior independently of the OS sandbox.
- `codeworld exec --sandbox <read-only|workspace-write|danger-full-access>` overrides the filesystem sandbox. Use `--network` to enable sandboxed process and `web_search` network access.
- `--oss --local-provider <ollama|lmstudio>` selects the local OpenAI-compatible provider with its conventional loopback endpoint.
- Repeatable `--add-dir <path>` adds canonical extra workspace roots to structured path validation and the macOS/Linux process sandbox. Relative tool paths still resolve under the primary workspace; use absolute paths for extra roots.
- `--dangerously-bypass-approvals-and-sandbox` selects full-access permissions and an unsandboxed process; `--dangerously-bypass-hook-trust` separately skips hook trust prompts for vetted automation.
- `codeworld exec --output-schema <path>` requests and validates structured JSON output. The current validator covers `type`, `properties`, `required`, `items`, `enum`, and `additionalProperties`.
- `codeworld exec -o <path>` also writes the final message to a file.
- `codeworld review` reviews staged and unstaged changes in read-only mode.
- `codeworld review --base <branch>` and `--commit <sha>` review a selected Git change set.
- `codeworld mcp list|get|add|remove|login|logout` manages user-level stdio and HTTP MCP servers, including OAuth 2.0 authorization-code login with PKCE and refresh tokens.
- `codeworld mcp-server` exposes `codex` and `codex-reply` over JSONL stdio MCP so other agents can start and continue Codeworld sessions.
- `codeworld app-server [--stdio|--listen stdio://]` serves the Codex app-server JSONL protocol. The current compatibility slice implements `initialize`, `thread/start`, `thread/resume`, `turn/start`, and `turn/interrupt`, including streamed item and turn lifecycle notifications.
- App-server keeps one Runtime per loaded thread, rejects overlapping turns on the same thread, persists completed or useful partial turns, and cancels the active model/tool context on `turn/interrupt`. WebSocket, daemon, and remote-control transports are not yet advertised.
- MCP thread runtime metadata is saved privately under `$CODEWORLD_HOME/mcp-threads`, allowing `codex-reply` to continue a persisted workspace session after the stdio server restarts.
- The stdio MCP server processes session calls serially, preserves response order, and honors `notifications/cancelled` by cancelling the matching active request context.
- `codeworld plugin list|add|remove` installs Codex-compatible plugins from configured local or Git marketplaces; `codeworld plugin marketplace add|list|upgrade|remove` manages their snapshots.
- `codeworld features list|enable|disable` reports capability stages and persists supported user feature switches; global `--enable/--disable` applies invocation-only overrides.
- `codeworld debug models|prompt-input` renders the configured model selection or exact model-visible prompt input without starting providers, hooks, or MCP servers.
- `codeworld execpolicy check --rules <path>... -- <command>...` evaluates explicit prefix-rule files and emits Codex-compatible JSON without executing the command; `--resolve-host-executables` enables absolute-path fallback through optional `host_executable(name=..., paths=[...])` allowlists.
- `codeworld sessions [--archived]` lists active or archived sessions.
- `codeworld doctor [--json]` checks configuration, credentials, the Git workspace, process sandbox backend, MCP declarations, saved sessions, and terminal metadata without contacting the model.
- `codeworld completion [bash|elvish|fish|powershell|zsh]` emits context-aware shell completion (bash by default).
- `codeworld auth codex status [--json]` reports the user-level OAuth account and expiry without exposing tokens; `logout` removes those credentials idempotently. New logins use `$CODEWORLD_HOME/auth/codex.json`, while existing workspace credentials remain readable as a compatibility fallback.
- Codex-compatible top-level `login`, `login status`, and `logout` commands alias the user-level OAuth lifecycle; `--version`/`-V` reports build information.
- `codeworld sandbox [options] -- <command>` runs a command with the same OS sandbox policy used by agent tools; `-C`, `--sandbox`, `--network`, and `--no-network` are supported.
- Global `-C`/`--cd` selects the workspace without changing the parent shell, while `-m`/`--model` overrides the configured or resumed-session model for one invocation.
- Long options that take values accept both `--flag value` and Codex-style `--flag=value` forms; arguments after a literal `--` are preserved unchanged.
- A trailing top-level prompt starts the interactive TUI and submits it as the initial turn. Repeatable global `-i`/`--image` attaches images to that turn, while `--no-alt-screen` keeps terminal scrollback visible.
- Global `-s`/`--sandbox`, `-a`/`--approval-mode`, and network flags override runtime policy before TUI, REPL, or automation starts. `--search` enables sandbox network access and the existing native web search tool. Command-local exec flags take precedence; review remains read-only.
- Repeatable `-c`/`--config key=value` overrides supported Codeworld scalar settings after files, profiles, and environment variables. Unknown keys fail explicitly. `--strict-config` is accepted for Codex CLI compatibility; Codeworld config parsing is already strict by default.
- `--enable` and `--disable` are accepted both globally and after `exec` or `review` for supported Codeworld feature flags; `codeworld exec --version` and subcommand-local help are also available.
- `e` aliases `exec`; `exec review` aliases the top-level review flow. Review accepts `--uncommitted` and `--title` in addition to base and commit targets.
- Exec accepts Codex-positioned `-m/-p/-C/-s/-a/-i/-c` options after the subcommand. With no prompt it reads piped stdin; when both are present, stdin is appended inside a `<stdin>` block. `--ignore-user-config`, `--ignore-rules`, `--skip-git-repo-check`, and `--color` are accepted with explicit Codeworld semantics.
- `exec --color <auto|always|never>` controls ANSI styling for human-readable tool progress on stderr. Auto mode requires a terminal and respects `NO_COLOR` and `TERM=dumb`; JSONL and final-message output remain uncolored.
- Shell approval rules are loaded from `$CODEWORLD_HOME/rules/*.rules` and `<workspace>/.codeworld/rules/*.rules`. Rules use Codex-compatible `prefix_rule(...)` and `host_executable(...)` syntax; `forbid` takes precedence over `prompt`, then `allow`, and dynamic shell syntax is never auto-allowed. Use `codeworld exec --ignore-rules ...` to skip both rule locations.
- `codeworld fork <session-id|--last>` clones a transcript into a new interactive session.
- Running `codeworld resume` or `codeworld fork` without a session argument opens a numbered session picker. `--last` remains non-interactive, and trailing text is submitted as the first prompt after resume or fork.
- `codeworld sessions rename <id|--last> <name>` or TUI `/rename <name>` assigns a unique session name; resume, fork, archive, unarchive, and delete accept names as well as IDs.
- `codeworld archive`, `unarchive`, and `delete` manage saved session lifecycle.
- `codeworld index` refreshes `.codeworld/index.json`.

If you are running from source:

```bash
go run ./cmd/codeworld
go run ./cmd/codeworld exec --ephemeral "list the Go packages"
```

## TUI Commands

The TUI uses a responsive terminal layout with a compact two-line status
header, an empty-workspace welcome state, visually distinct conversation/tool/
permission blocks, and a state-aware bordered composer with shortcut hints.

Inside the TUI:

```text
/help          show commands
/status        provider, model, git state, messages, approvals, token usage
/model         show current model
/model <name>  change the session model name
/diff          show git diff
/permissions   list session approvals
/permissions <untrusted|on-request|never|auto|read-only|full-access>
               switch the current approval mode
/mcp           list configured MCP servers
/providers     list LLM provider profiles
/providers add configure a profile with a step-by-step wizard
/providers edit <id>
               edit a saved profile
/providers delete <id>
               delete a profile after explicit confirmation
/providers use <id>
               switch this session and its subagents to a profile
/providers cancel
               cancel the active provider wizard
/skills        list loaded project skills
/context       show context system status
/agents        list local subagent tasks
/agents <id>   show one subagent task
/agents send <id> <message>
               append an instruction and resume/restart the task
/agents interrupt <id>
               interrupt a running task so it can be resumed
/agents terminate <id>
               permanently terminate a task
/theme <mode>  set system, dark, or light mode state
/resume        list recent active sessions
/goal <text>   set or show the current task goal
/goal clear    clear the current task goal
/rename <name> assign a reusable name to the current session
/plan          enable plan mode for the session
/plan off      return to the default mode
/interrupt     interrupt the active turn without exiting
/compact       summarize older conversation history
/image <path>  attach an image to the next prompt
/repl          show how to restart in line REPL mode
/clear         clear session messages, approvals, and token counters
/exit          quit
```

Type `/` to show slash command suggestions. Scroll the transcript with
`PageUp`, `PageDown`, `Ctrl+U`, and `Ctrl+D`. Use `Up` and `Down` in the
composer to restore submitted drafts. While a turn is running, Enter queues a
follow-up, `Ctrl+J`/`Alt+Enter` still inserts a newline, and `Ctrl+X` interrupts
the active turn without exiting; queued follow-ups start automatically.

`/goal` persists a durable objective in the session and injects it into future
model calls. `/plan` asks the agent to propose a concise plan before edit-heavy
work. `/compact` manually summarizes older history using the configured model.

## Architecture

Codeworld is organized as a small set of explicit layers:

```text
cmd/codeworld
  CLI dispatch for TUI, REPL, run, and index modes

internal/app
  Runtime assembly: config, workspace, session, provider, tools, capabilities

internal/agent
  Turn loop: model calls, streaming events, tool calls, permissions

internal/model
  Provider-neutral model types and provider clients

internal/tools
  Workspace tools, shell, git, patching, web search, plugin tools, MCP bridge, context tools

internal/capability
  Project skills, Codeworld plugins, Codex plugin bundles, MCP setup

internal/context
  File index, context graph, and conversation summarization

internal/tui
  Bubble Tea terminal interface
```

The runtime builds a system prompt from the base agent instructions, workspace
summary, file index, context graph, `AGENTS.md` instructions, skill index, and
conversation summary. Skills use progressive disclosure: the prompt contains
only a compact skill list, and the model can call `skill_open` when it needs
the full instructions.

## Project Instructions

Codeworld loads project instructions from `AGENTS.md` and
`AGENTS.override.md`. Discovery starts at the git root when available and walks
down to the workspace directory. In a single directory, `AGENTS.override.md`
wins over `AGENTS.md`; child directories are appended after parent directories.

Use these files for durable project guidance such as test commands, coding
style, review expectations, and package-specific rules.

## Skills

Project skills live under:

```text
.codeworld/skills/<skill-name>/SKILL.md
```

Example:

```markdown
---
name: reviewer
description: Review Go changes carefully.
---

Inspect relevant files before editing. Run focused tests before reporting completion.
```

Skills are loaded at startup, listed with `/skills`, and injected into the
agent system context.

## Plugins

Codeworld supports two plugin shapes.

Native Codeworld plugin manifests live under:

```text
.codeworld/plugins/<plugin-name>/plugin.json
```

Example:

```json
{
  "name": "demo",
  "tools": [
    {
      "name": "demo.echo",
      "description": "Echo text",
      "command": "printf",
      "args": ["{{text}}"],
      "input_schema": {
        "type": "object",
        "properties": {
          "text": { "type": "string" }
        },
        "required": ["text"]
      },
      "risk": "read"
    }
  ]
}
```

Enable plugin loading with:

```toml
plugins_enabled = true
```

Codeworld also supports Codex plugin bundles with skills and MCP declarations:

```text
.codeworld/codex-plugins/<plugin-name>/.codex-plugin/plugin.json
.codeworld/codex-plugins/<plugin-name>/skills/<skill-name>/SKILL.md
.codeworld/codex-plugins/<plugin-name>/.mcp.json
```

The compatibility loader supports bundled skills and MCP server declarations.
Plugins can be installed into the user cache through the Codex-compatible
marketplace lifecycle:

```sh
codeworld plugin marketplace add ./my-marketplace
codeworld plugin marketplace add owner/repo --ref main
codeworld plugin list --available --json
codeworld plugin add reviewer@my-marketplace
codeworld features list
codeworld features enable plugins
codeworld debug models
codeworld debug prompt-input "inspect this workspace"
codeworld plugin remove reviewer@my-marketplace
codeworld plugin marketplace upgrade
```

Marketplace plugin directories are discovered under `plugins/<name>` and must
contain `.codex-plugin/plugin.json`. Hooks, apps, and UI assets are preserved in
the cache but are not executed or rendered by Codeworld yet.

## MCP

Codeworld supports stdio and HTTP MCP servers. Configure them in `.codeworld/config.toml`:

```toml
[[mcp_servers]]
name = "demo"
command = "node"
args = ["server.js"]
```

HTTP MCP example:

```toml
mcp_oauth_callback_port = 5555
mcp_oauth_callback_url = "http://localhost:5555/callback"

[[mcp_servers]]
name = "docs"
url = "https://mcp.example.test/mcp"
bearer_token_env_var = "DOCS_TOKEN"
http_headers = ["X-Test: yes"]
enabled_tools = ["search"]
disabled_tools = ["write"]
```

For OAuth-capable HTTP servers, `codeworld mcp login <name>` discovers the
protected-resource and authorization-server metadata, dynamically registers a
public client, opens a PKCE authorization flow, and stores user-level tokens
under `$CODEWORLD_HOME/mcp-oauth/` with restricted permissions. Runtime requests
refresh expiring tokens automatically and can still read legacy workspace
tokens; `codeworld mcp logout <name>` deletes both locations.

Codeworld can also run as an MCP server:

```sh
codeworld mcp-server
```

Its `codex` tool accepts `prompt`, `cwd`, `model`, `approval-policy`, `sandbox`,
config overrides, and instruction overrides. It returns `threadId` and
`content`; pass that thread id to `codex-reply` to continue the session. The
stdio transport follows current MCP JSONL framing while the client continues to
accept legacy `Content-Length` response frames.

MCP tools are registered as:

```text
mcp.<server-name>.<tool-name>
```

## Context Tools

The agent can use these context tools:

```text
context_refresh   rebuild the workspace context graph
context_search    search paths, file content, and Go symbols
context_open      open a graph file with symbol metadata
```

The graph is deterministic and local. It extracts file metadata, Go symbols,
git changed state, project skills, summaries, and other configured context.

## Local Subagents

The model can start independent read-only investigation tasks with
`subagent_start`, inspect them with `subagent_status`, and control them with
`subagent_send`, `subagent_interrupt`, and `subagent_terminate`. Starts may select
a configured role. The manager enforces `subagent_max_concurrent`; interrupted
tasks can be resumed with a follow-up message, while terminated tasks are final.

Subagent tasks are stored as JSON files in `.codeworld/subagents/`. Each task
records the prompt, role, follow-up messages, status, final result, timestamps,
and a compact transcript with role/content text only.

## Web Search

The native `web_search` tool searches DuckDuckGo Lite without requiring a
separate search API key. It accepts a `query` and optional `limit` and returns
numbered results with title, URL, and snippet.

Because it performs network access, `web_search` is registered as a read action
with network risk. Explicit global `--search` enables both sandbox network and
native search without per-call approval; enabling network through config or
`--network` alone retains the normal permission flow.

## Image Input

OpenAI-compatible, Anthropic, and Gemini providers can receive image content parts. In
the TUI, run `/image <path>` before your next prompt. In non-interactive mode,
use:

```bash
codeworld run --image screenshot.png "explain this screenshot"
```

DeepSeek text models and the Codex OAuth provider currently return a clear
unsupported-image error when an image is attached. Persisted local images are
reloaded from their original path when a session resumes.

## Permissions

Every tool exposes a permission request with an action, target, risk, and
reason. The default runtime uses an auto policy that allows structured
workspace reads and writes, but asks before shell, MCP, and
plugin execution as well as destructive or networked actions. The TUI shows permission prompts inline and
supports allowing once, denying, or approving similar shell commands for the
current session.

Approval modes:

- `untrusted`: automatically run trusted read-only shell commands and ask for
  other commands.
- `on-request`: allow ordinary sandboxed actions and ask for destructive or
  network actions.
- `never`: never request approval; actions outside the workspace remain denied
  and the configured OS sandbox remains active.
- `auto`: allow ordinary workspace actions and ask for high-risk actions.
- `read-only`: allow reads and ask before writes, patches, or commands.
- `full-access`: allow in-workspace and network actions without prompting for
  the current process only.

Set the default in `.codeworld/config.toml`:

```toml
approval_mode = "auto"
```

Project config accepts the sandbox-preserving modes but rejects `full-access`;
select that legacy mode interactively when it is intentionally needed.

## Process sandbox

Model-triggered shell, background/PTY, plugin, and hook commands run in an OS
sandbox by default. `workspace-write` exposes the host filesystem read-only and
allows writes only below the workspace; `read-only` also removes structured
write/patch/index tools; `danger-full-access` deliberately bypasses the OS
sandbox. Network access is disabled by default and also controls whether the
native `web_search` tool is registered.

```toml
sandbox_mode = "workspace-write"
sandbox_network = false
```

macOS uses the built-in `sandbox-exec`. Linux requires `bwrap` from
Bubblewrap. Restricted modes fail closed when the backend is missing or the
host forbids nested sandboxing. Windows reports restricted modes as unsupported.
Configured MCP servers remain trusted extensions with their own explicit
startup approval; provider API traffic is not routed through the command sandbox.

## Hooks

Codeworld can run command hooks from `.codeworld/hooks.json` around session
startup, user prompts, tool use, compaction, and shutdown. Supported events are
`SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PermissionRequest`,
`PostToolUse`, `PreCompact`, `PostCompact`, and `Stop`.

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "shell",
        "hooks": [
          { "type": "command", "command": "printf pre-tool >> .codeworld/hooks.log" }
        ]
      }
    ]
  }
}
```

Every unique workspace hook command requires explicit approval before
`SessionStart`; choosing the session approval option remembers the exact
command for the current process. Persisted workspace session files are never
trusted as an approval source after restart. Hook timeouts default to 60 seconds,
and hook processes use the same OS sandbox as shell tools.

## Logs And State

Workspace state is stored under `.codeworld/`.

Important files:

```text
.codeworld/current-session.json
.codeworld/sessions/
.codeworld/index.json
.codeworld/logs/model-calls.jsonl
```

Model call logs are disabled by default. Set `model_call_logging = true` to
write compact `body_json` records; API keys are redacted, but prompts and tool
results may still contain sensitive workspace data.

## Limitations

Known gaps compared with Codex include:

- no browser/computer-use surface;
- subagents are local read-only investigation tasks, not cloud tasks;
- PTY sessions are available on Unix platforms; Windows reports PTY as unsupported instead of silently using pipes;
- Linux sandboxing requires Bubblewrap to be installed; Windows restricted process sandboxing is not implemented;
- no image generation;
- no cloud task or hosted GitHub PR review integration;
- no syntax-highlighted TUI diff/code blocks yet.

## Development

Run the full test suite:

```bash
go test -count=1 ./...
```

Build:

```bash
go build -o /private/tmp/codeworld-build ./cmd/codeworld
```

Smoke-test the TUI path without opening an interactive terminal:

```bash
printf '/exit\n' | go run ./cmd/codeworld tui
```
