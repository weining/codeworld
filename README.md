# Codeworld

Codeworld is a local coding agent written in Go. It is designed as a small,
inspectable foundation for a Codex- or Claude Code-style terminal assistant.

The current version focuses on:

- a Codex-like Bubble Tea TUI as the default interactive entry point;
- DeepSeek by default, with OpenAI-compatible, Anthropic, and Codex OAuth providers;
- streaming assistant output for OpenAI-compatible providers;
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
`$CODEWORLD_HOME/profiles/<name>.toml`. Project values override user values;
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

Provider environment variables:

```bash
DEEPSEEK_API_KEY=...
OPENAI_API_KEY=...
ANTHROPIC_API_KEY=...
CODEWORLD_LOCAL_BASE_URL=http://127.0.0.1:11434/v1
```

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
codeworld review
codeworld review --base main
codeworld review --commit HEAD
codeworld sessions
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
- `codeworld exec --json` emits lifecycle, tool, usage, and result events as JSONL.
- `codeworld exec --ephemeral` avoids loading or saving the current session.
- `codeworld exec --approval-mode <auto|read-only|full-access>` overrides prompt/approval behavior independently of the OS sandbox.
- `codeworld exec --sandbox <read-only|workspace-write|danger-full-access>` overrides the filesystem sandbox. Use `--network` to enable sandboxed process and `web_search` network access.
- `codeworld exec --output-schema <path>` requests and validates structured JSON output. The current validator covers `type`, `properties`, `required`, `items`, `enum`, and `additionalProperties`.
- `codeworld exec -o <path>` also writes the final message to a file.
- `codeworld review` reviews staged and unstaged changes in read-only mode.
- `codeworld review --base <branch>` and `--commit <sha>` review a selected Git change set.
- `codeworld sessions [--archived]` lists active or archived sessions.
- `codeworld fork <session-id|--last>` clones a transcript into a new interactive session.
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
/permissions <auto|read-only|full-access>
               switch the current approval mode
/mcp           list configured MCP servers
/skills        list loaded project skills
/context       show context system status
/agents        list local subagent tasks
/agents <id>   show one subagent task
/theme <mode>  set system, dark, or light mode state
/resume        list recent active sessions
/goal <text>   set or show the current task goal
/goal clear    clear the current task goal
/plan          enable plan mode for the session
/plan off      return to the default mode
/compact       summarize older conversation history
/image <path>  attach an image to the next prompt
/repl          show how to restart in line REPL mode
/clear         clear session messages, approvals, and token counters
/exit          quit
```

Type `/` to show slash command suggestions. Scroll the transcript with
`PageUp`, `PageDown`, `Ctrl+U`, and `Ctrl+D`. Use `Up` and `Down` in the
composer to restore submitted drafts.

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

Codeworld also supports a small Codex plugin compatibility shape:

```text
.codeworld/codex-plugins/<plugin-name>/.codex-plugin/plugin.json
.codeworld/codex-plugins/<plugin-name>/skills/<skill-name>/SKILL.md
.codeworld/codex-plugins/<plugin-name>/.mcp.json
```

The compatibility loader currently supports bundled skills and stdio MCP
server declarations. It does not yet support Codex marketplace metadata,
hooks, apps, assets, or plugin lifecycle policy.

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
`subagent_start` and inspect them with `subagent_status`. The TUI and REPL expose
the same state through `/agents` and `/agents <id>`.

Subagent tasks are stored as JSON files in `.codeworld/subagents/`. Each task
records the prompt, status, final result, timestamps, and a compact transcript
with role/content text only.

## Web Search

The native `web_search` tool searches DuckDuckGo Lite without requiring a
separate search API key. It accepts a `query` and optional `limit` and returns
numbered results with title, URL, and snippet.

Because it performs network access, `web_search` is registered as a read action
with network risk. The TUI and REPL permission flow can ask before the request
is made.

## Image Input

OpenAI-compatible and Anthropic providers can receive image content parts. In
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

- `auto`: allow ordinary workspace actions and ask for high-risk actions.
- `read-only`: allow reads and ask before writes, patches, or commands.
- `full-access`: allow in-workspace and network actions without prompting for
  the current process only.

Set the default in `.codeworld/config.toml`:

```toml
approval_mode = "auto"
```

Project config accepts only `auto` and `read-only`; select `full-access`
interactively when it is intentionally needed.

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
- MCP OAuth token refresh and dynamic registration are still partial;
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
