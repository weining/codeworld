# Codeworld Phase 2 Design

## Goal

Phase 2 turns the current REPL-only MVP into a daily-use local coding agent with
single-shot execution, session-scoped shell approvals, richer terminal UX,
multiple model providers, larger-project context support, and plugin-ready tool
registration.

This phase intentionally does not add Codex delegation, full MCP protocol
support, IDE integration, web UI, or automatic approval for file writes.

## Current Baseline

The existing implementation has:

- a Go CLI entrypoint in `cmd/codeworld`;
- a provider-neutral model contract in `internal/model`;
- a DeepSeek chat-completions client with compact model-call logging;
- a conservative agent loop in `internal/agent`;
- workspace-safe read, write, patch, shell, and git tools in `internal/tools`;
- a line-oriented REPL in `internal/repl`;
- JSON session persistence in `internal/session`;
- token usage display and terminal title updates.

Phase 2 should preserve the current simple REPL and add new paths around it
rather than replacing the core runtime.

## Feature Scope

### 1. Single-Shot Run Mode

`codeworld run "task"` executes one agent turn or tool loop and exits.

Behavior:

- Reuse the same workspace, session, tools, provider, logger, and agent runner as
  the REPL.
- Print tool progress and final assistant text to stdout.
- Save updated session history and usage after the run.
- Return exit code `0` when the agent produces a final response.
- Return a non-zero exit code when configuration, provider, context, or agent
  execution fails.
- In non-interactive mode, confirmation-required actions are denied unless they
  match an explicit session approval.

The first version should not add many flags. The supported shape is:

```text
codeworld run "inspect this repository"
```

### 2. Session-Scoped Shell Approvals

Shell confirmation prompts gain a session allow option for low-risk repeated
commands.

Behavior:

- Existing `y` confirms only the current tool call.
- Existing default/`n` denies the current tool call.
- New `a` allows matching future shell commands for the current session.
- Session approvals persist in `.codeworld/current-session.json`.
- Approvals apply only to shell requests, not write or patch requests.
- Approval matching is exact after command normalization. Phase 2 does not
  attempt fuzzy matching or command semantic analysis.
- The REPL `/status` output includes the number of active command approvals.
- `/clear` clears messages, usage, and approvals.

The conservative permission policy remains the source of risk classification.
Session approval only bypasses the user prompt after the policy returns
`DecisionAsk` for a shell request with the same normalized command.

### 3. Better Terminal Experience

Phase 2 keeps the existing line REPL and adds an optional full-screen TUI.

Behavior:

- Default `codeworld` keeps using the existing line REPL for compatibility.
- `codeworld tui` starts a Bubble Tea based full-screen interface.
- The TUI has four visible regions:
  - scrollable conversation output;
  - tool/model event stream;
  - bottom status bar with provider, model, token usage, workspace, and approval
    count;
  - input composer.
- The TUI reuses the same runner, confirmer, reporter, session store, and model
  client wiring as the REPL.
- If Bubble Tea cannot run because stdin/stdout is not a terminal, the command
  returns a clear error.

The TUI should be introduced behind a separate command so current scripts and
tests are not forced through a full-screen renderer.

### 4. Multiple Providers

Phase 2 introduces a provider factory while preserving DeepSeek as the default.

Supported providers:

- `deepseek`: current implementation, `DEEPSEEK_API_KEY`;
- `openai`: OpenAI-compatible chat completions, `OPENAI_API_KEY`;
- `anthropic`: Anthropic Messages API, `ANTHROPIC_API_KEY`;
- `local`: OpenAI-compatible local endpoint, configured by
  `CODEWORLD_LOCAL_BASE_URL`.

Configuration:

```toml
provider = "deepseek"
model = "deepseek-v4-pro"
max_steps = 20
workspace = "."
```

Additional optional keys:

```toml
local_base_url = "http://127.0.0.1:11434/v1"
tui = false
```

Provider requirements:

- Every provider implements `model.Client`.
- Every provider maps provider-native token usage into `model.Usage` when
  available.
- Every provider logs request and response `body_json` through the same compact
  model-call logger shape.
- Unsupported providers fail during app construction with a clear error.

OpenAI and local can share an OpenAI-compatible client. Anthropic needs a small
provider-specific request/response mapper because its tool-call format differs.

### 6. Larger-Project Context

Phase 2 adds a lightweight workspace index and conversation summarization.

Workspace index:

- Stored at `.codeworld/index.json`.
- Captures relative path, size, modification time, extension, likely language,
  and whether the file was skipped.
- Skips `.git`, `.codeworld/logs`, large binary-looking files, and files over a
  configured size limit.
- Exposes an `index_workspace` tool so the agent can refresh the index.
- Adds a compact index summary to the system prompt when available.
- `codeworld index` refreshes the index and exits.

Conversation summarization:

- Stores a `summary` field in session JSON.
- Adds a summarizer interface that can call the active model to compress old
  messages.
- Summarization is triggered when message count or token usage crosses a
  configurable threshold.
- The summary is injected after the system prompt and before recent messages.
- The first implementation keeps the most recent messages verbatim and replaces
  older user/assistant/tool messages with the summary.

The index is deterministic and local. Summarization uses the configured model and
therefore may fail; a summarization failure should not discard existing messages.

### 7. Plugin-Ready Tool Registration

Phase 2 adds local plugin manifests and dynamic tool registration, but not full
MCP.

Manifest path:

```text
.codeworld/plugins/<plugin-name>/plugin.json
```

Manifest shape:

```json
{
  "name": "example",
  "tools": [
    {
      "name": "example.echo",
      "description": "Echo input text",
      "command": "echo",
      "args": ["{{text}}"],
      "input_schema": {
        "type": "object",
        "properties": {
          "text": {"type": "string"}
        },
        "required": ["text"]
      },
      "risk": "read"
    }
  ]
}
```

Behavior:

- Plugin tools are loaded at app construction.
- Plugin tool names must include a namespace prefix such as `example.echo`.
- Plugin commands execute through the existing shell execution path and
  permission system.
- Plugin tool arguments are rendered only from JSON fields using exact
  `{{field}}` placeholders.
- Missing placeholder values fail before command execution.
- Plugin tools are disabled by default unless `plugins_enabled = true` is set in
  project config.

This gives the project a stable extension boundary before adding MCP later.

## Runtime Architecture

Introduce a small app wiring layer so REPL, TUI, run mode, and index mode share
construction:

```text
cmd/codeworld
  parses command shape
  calls internal/app builder

internal/app
  loads config
  creates workspace/session/store
  creates model provider via provider factory
  creates tool registry, plugin tools, index tools
  creates agent runner
  returns runtime object used by repl/run/tui/index commands
```

Provider packages:

```text
internal/model/deepseek
internal/model/openai
internal/model/anthropic
internal/model/local
internal/model/provider
```

Context packages:

```text
internal/context/indexer
internal/context/summarizer
```

Plugin packages:

```text
internal/plugin
internal/tools/plugin_tool.go
```

The existing package boundaries should remain recognizable. The app builder is
introduced to remove duplicated setup from `cmd/codeworld/main.go`, not to hide
business logic.

## Data Flow

Interactive REPL:

```text
codeworld
  -> app.NewRuntime
  -> repl.Run
  -> agent.RunTurn
  -> provider.Generate
  -> tools.Execute as needed
  -> session.SaveCurrent
```

Single-shot run:

```text
codeworld run "task"
  -> app.NewRuntime
  -> runtime.RunOnce
  -> agent.RunTurn
  -> session.SaveCurrent
  -> print final text
```

TUI:

```text
codeworld tui
  -> app.NewRuntime
  -> tui.Run
  -> agent.RunTurn on submitted input
  -> stream tool events into TUI event log
  -> session.SaveCurrent after each completed turn
```

Index:

```text
codeworld index
  -> app.NewRuntime
  -> indexer.Build
  -> write .codeworld/index.json
```

## Error Handling

- Provider configuration errors fail before starting the REPL/TUI/run command.
- Missing API keys produce provider-specific messages without printing secrets.
- Non-interactive confirmation denials are returned to the model as permission
  denials, matching current REPL behavior.
- TUI startup errors should not corrupt sessions.
- Index write failures are returned to the command and do not modify existing
  index files.
- Plugin manifest parse errors fail startup only when plugins are enabled.
- Plugin command rendering errors return a tool error to the model.
- Summarization failures are logged to stderr or the event stream and preserve
  original messages.

## Testing Strategy

Unit tests:

- command parsing and dispatch for `run`, `tui`, `index`, default REPL;
- session approval save/load and exact command matching;
- provider factory selection and missing credential errors;
- OpenAI-compatible request/response mapping;
- Anthropic request/response mapping;
- indexer skip rules and deterministic output;
- summarizer message replacement behavior;
- plugin manifest parsing, validation, and placeholder rendering;
- TUI model state transitions without requiring a real terminal.

Integration-style tests:

- `codeworld run` with a fake model client saves session and prints final text;
- approved shell command bypasses prompt in the same restored session;
- plugin tool appears in model tool definitions only when enabled.

Manual smoke tests:

1. `codeworld run "inspect this repository"` with DeepSeek.
2. REPL approve `go test ./...` with `a`, then ask for tests again.
3. `codeworld index` creates `.codeworld/index.json`.
4. `codeworld tui` starts in a terminal and updates status after a turn.
5. Switch provider to `openai` and run `/status`.
6. Enable a simple plugin and verify the tool appears in the model-call log.

## Rollout Order

1. Extract shared app runtime wiring.
2. Add `codeworld run`.
3. Add session shell approvals.
4. Add provider factory and OpenAI-compatible provider.
5. Add Anthropic provider.
6. Add workspace index and `codeworld index`.
7. Add conversation summarization.
8. Add plugin manifest loading and plugin shell tools.
9. Add optional Bubble Tea TUI.

The TUI is intentionally last because it depends on stable runtime events,
session state, and provider/tool behavior.

## Acceptance Criteria

- Existing `codeworld` REPL behavior still works.
- `codeworld run "..."` executes a single task and saves session state.
- Session shell approvals persist and bypass repeated prompts for exact matching
  shell commands.
- `provider = "openai"`, `provider = "anthropic"`, and `provider = "local"` are
  recognized and produce model clients when credentials/config are present.
- `.codeworld/index.json` can be generated and read into the system prompt.
- Old conversation messages can be summarized without losing recent context.
- Plugin tools load only when enabled and execute through the same permission
  system as built-in shell tools.
- `codeworld tui` is optional and does not replace the line REPL.
- `mise exec -- go test -count=1 ./...` passes.
- `mise exec -- go build ./cmd/codeworld` succeeds.
