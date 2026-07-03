# Codeworld

Codeworld is a local coding agent written in Go. It is designed as a small,
inspectable foundation for a Codex- or Claude Code-style terminal assistant.

The current version focuses on:

- a Codex-like Bubble Tea TUI as the default interactive entry point;
- DeepSeek by default, with OpenAI-compatible and Anthropic providers;
- streaming assistant output for OpenAI-compatible providers;
- workspace-safe tools for reading, writing, patching, shell commands, git, context search, and native web search;
- project skills, plugin tools, stdio/HTTP MCP tools, hooks, and local subagents;
- session persistence, image input, token accounting, and model call logs.

Chinese documentation is available in [README.zh-CN.md](README.zh-CN.md).

## Status

Codeworld is usable as a local coding agent, but it is still intentionally
smaller than Codex. The current implementation covers a Codex-like local TUI,
provider calls, workspace tools, native web search, permissions, skills,
plugins, stdio/HTTP MCP, hooks, local subagents, image input, and deterministic
context. Larger Codex-style surfaces such as cloud tasks, IDE integration,
browser control, computer use, and hosted review workflows are future work.

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

Codeworld reads `.codeworld/config.toml` from the workspace root and provider
keys from the environment.

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
index_max_file_bytes = 262144
```

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

## Use

Start the TUI:

```bash
codeworld
```

Explicit commands:

```bash
codeworld tui
codeworld resume --last
codeworld resume <session-id>
codeworld repl
codeworld run "inspect the project and explain the entry points"
codeworld run --image screenshot.png "explain this screenshot"
codeworld index
```

Command behavior:

- `codeworld` starts the TUI when attached to a terminal.
- `codeworld tui` starts the TUI explicitly.
- `codeworld resume --last` resumes the newest archived local session.
- `codeworld resume <session-id>` resumes a specific archived session.
- `codeworld repl` starts the line-oriented REPL.
- `codeworld run <task>` runs one non-interactive turn and exits.
- `codeworld run --image <path> <task>` attaches an image to a non-interactive turn.
- `codeworld index` refreshes `.codeworld/index.json`.

If you are running from source:

```bash
go run ./cmd/codeworld
go run ./cmd/codeworld run "list the Go packages"
```

## TUI Commands

The TUI uses a Codex-like terminal layout: a single-line status header, a
role-aligned transcript, inline tool and permission events, and a bordered
bottom composer with shortcut hints.

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
/resume        list recent archived sessions
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

DeepSeek text models currently return a clear unsupported-image error when an
image is attached.

## Permissions

Every tool exposes a permission request with an action, target, risk, and
reason. The default runtime uses an auto policy that allows ordinary workspace
actions but still asks for high-risk actions such as destructive or networked
shell commands and web search. The TUI shows permission prompts inline and
supports allowing once, denying, or approving similar shell commands for the
current session.

Approval modes:

- `auto`: allow ordinary workspace actions and ask for high-risk actions.
- `read-only`: allow reads and ask before writes, patches, or commands.
- `full-access`: allow in-workspace and network actions without prompting.

Set the default in `.codeworld/config.toml`:

```toml
approval_mode = "auto"
```

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

This is not full sandbox parity with Codex yet. Fine-grained filesystem
profiles, network policy, and hook trust remain future work.

## Logs And State

Workspace state is stored under `.codeworld/`.

Important files:

```text
.codeworld/current-session.json
.codeworld/sessions/
.codeworld/index.json
.codeworld/logs/model-calls.jsonl
```

Model call logs keep compact `body_json` records and redact API keys.

## Limitations

Known gaps compared with Codex include:

- no browser/computer-use surface;
- subagents are local read-only investigation tasks, not cloud tasks;
- MCP OAuth token refresh and dynamic registration are still partial;
- no image generation;
- no cloud task or PR review integration;
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
