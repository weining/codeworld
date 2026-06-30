# Codeworld

Codeworld is a local coding agent written in Go. It is designed as a small,
inspectable foundation for a Codex- or Claude Code-style terminal assistant.

The current version focuses on:

- a Bubble Tea TUI as the default interactive entry point;
- DeepSeek by default, with OpenAI-compatible and Anthropic providers;
- streaming assistant output for OpenAI-compatible providers;
- workspace-safe tools for reading, writing, patching, shell commands, git, and context search;
- project skills, plugin tools, and stdio MCP tools;
- session persistence, token accounting, and model call logs.

## Install

This project uses Go through `mise`.

```bash
mise exec -- go test ./...
mise exec -- go install ./cmd/codeworld
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
codeworld repl
codeworld run "inspect the project and explain the entry points"
codeworld index
```

If you are running from source:

```bash
mise exec -- go run ./cmd/codeworld
mise exec -- go run ./cmd/codeworld run "list the Go packages"
```

## TUI Commands

Inside the TUI:

```text
/help          show commands
/status        provider, model, git state, messages, approvals, token usage
/model         show current model
/model <name>  change the session model name
/diff          show git diff
/permissions   list session approvals
/mcp           list configured MCP servers
/skills        list loaded project skills
/context       show context system status
/theme <mode>  set system, dark, or light mode state
/repl          show how to restart in line REPL mode
/clear         clear session messages, approvals, and token counters
/exit          quit
```

Scroll the transcript with `PageUp`, `PageDown`, `Ctrl+U`, and `Ctrl+D`.

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

Project plugin manifests live under:

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

## MCP

Codeworld supports stdio MCP servers. Configure them in `.codeworld/config.toml`:

```toml
[[mcp_servers]]
name = "demo"
command = "node"
args = ["server.js"]
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

## Development

Run the full test suite:

```bash
mise exec -- go test -count=1 ./...
```

Build:

```bash
mise exec -- go build -o /private/tmp/codeworld-build ./cmd/codeworld
```

Smoke-test the TUI path without opening an interactive terminal:

```bash
printf '/exit\n' | mise exec -- go run ./cmd/codeworld tui
```
