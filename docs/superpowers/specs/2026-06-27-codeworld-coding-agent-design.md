# Codeworld Coding Agent Design

Date: 2026-06-27

## Goal

Build `codeworld`, a Go-based local coding agent CLI. The first version is an
interactive REPL that can inspect a local workspace, call a real LLM provider,
request permission before modifying files or running commands, apply reviewed
patches, run tests after confirmation, and save session history.

The long-term direction is to evolve toward the practical coding workflows of
Codex and Claude Code. The first version deliberately stays smaller: one local
process, one model provider, conservative permissions, and a focused coding loop.

## Decisions

- The first user-facing mode is an interactive REPL launched with `codeworld`.
- The first model provider is DeepSeek.
- The default model is `deepseek-v4-pro`.
- Credentials are read from `DEEPSEEK_API_KEY`; secrets are never stored in the
  project config file.
- The permission model is conservative: read-only workspace actions are allowed,
  while file writes, patches, shell commands, dependency installation, network
  access, and workspace-external paths require user confirmation.
- The first architecture is a single-process modular CLI, not a daemon,
  distributed runtime, or multi-agent graph.
- Codex OAuth/access-token authentication is not treated as a generic LLM API
  credential. Codex can be integrated later as an external delegate tool through
  `codex exec` or the Codex SDK.

## Source Notes

- DeepSeek's API is OpenAI-compatible and uses `https://api.deepseek.com` as the
  OpenAI Format base URL. The design keeps model names configurable and defaults
  to `deepseek-v4-pro`. Source: <https://api-docs.deepseek.com/>
- Codex authentication is scoped to Codex local workflows, CLI, app, IDE, SDK,
  and automation. For general model API calls, use provider API keys instead of
  Codex OAuth. Sources: <https://developers.openai.com/codex/auth>,
  <https://developers.openai.com/codex/sdk>, and
  <https://developers.openai.com/codex/enterprise/access-tokens>.

## Architecture

The first version is a single Go binary with internal module boundaries:

```text
cmd/codeworld
  -> internal/repl
  -> internal/agent
  -> internal/model
  -> internal/model/deepseek
  -> internal/tools
  -> internal/permissions
  -> internal/session
  -> internal/workspace
```

Runtime flow:

```text
User input
  -> REPL appends the user message to the active session
  -> AgentLoop builds model input from system prompt, workspace context,
     session messages, and tool definitions
  -> DeepSeek returns either final text or tool calls
  -> ToolRegistry resolves each tool call
  -> PermissionPolicy checks whether execution is allowed or needs confirmation
  -> REPL prompts the user when confirmation is required
  -> Tool result is appended to the model conversation
  -> AgentLoop continues until final response, max step limit, or fatal error
```

The agent core depends on interfaces, not concrete implementations. This keeps
future provider additions, external delegates, and alternative tool backends from
forcing a rewrite of the core loop.

## Components

### `cmd/codeworld`

Responsibilities:

- Load config.
- Detect the current workspace.
- Initialize the model client, session store, tool registry, and permission
  policy.
- Start the REPL.

The binary does not contain agent logic directly. It wires dependencies together.

### `internal/repl`

Responsibilities:

- Read user input in a continuous terminal session.
- Render assistant messages and tool progress.
- Handle fixed slash commands.
- Ask for permission confirmations.
- Save the session after each completed turn.

First-version slash commands:

```text
/help      Show commands
/model     Show or switch model
/status    Show workspace, git, model, and session status
/diff      Show current git diff
/clear     Clear current conversation context
/exit      Exit the REPL
```

The first version does not include a slash-command plugin system.

### `internal/agent`

Responsibilities:

- Own the agent loop.
- Convert session messages into model requests.
- Pass tool definitions to the model.
- Execute returned tool calls through `ToolRegistry`.
- Enforce `maxSteps`.
- Return tool errors to the model as tool results instead of crashing the turn.

Default loop limits:

- `maxSteps = 20`
- single tool output size cap: 20-40 KB
- shell timeout: 60 seconds
- file reads are paginated by offset and limit

### `internal/model`

The core model interface is provider-neutral:

```go
type ModelClient interface {
    Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}
```

Core request/response types should use neutral structures:

```go
type ToolDefinition struct {
    Name        string
    Description string
    InputSchema map[string]any
}

type ToolCall struct {
    ID        string
    Name      string
    Arguments json.RawMessage
}
```

The agent package should not depend on DeepSeek-specific JSON structs.

### `internal/model/deepseek`

Responsibilities:

- Read `DEEPSEEK_API_KEY` from the environment.
- Use the DeepSeek OpenAI-compatible API endpoint.
- Map neutral `GenerateRequest` values to DeepSeek chat completion requests.
- Parse assistant messages and tool calls back into neutral response structs.
- Surface provider errors with enough detail for the REPL to show useful messages
  without printing secrets.

Default config:

```toml
provider = "deepseek"
model = "deepseek-v4-pro"
max_steps = 20
workspace = "."
```

If native tool calling is unreliable for a model or endpoint, a later fallback
can parse structured JSON blocks from model output. The first implementation
should use native tool calling.

### `internal/tools`

Tools are registered behind a common interface:

```go
type Tool interface {
    Definition() ToolDefinition
    PermissionRequest(args json.RawMessage) (permissions.Request, error)
    Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}
```

Read-only tools allowed without confirmation:

- `list_dir(path)` lists files under the workspace.
- `read_file(path, offset, limit)` reads part of a workspace file.
- `search(query, path)` searches workspace text.
- `git_status()` reads git status.
- `git_diff()` reads the current diff.

Mutation and execution tools require confirmation:

- `apply_patch(patch)` applies a unified diff.
- `write_file(path, content)` creates a file or rewrites a small file.
- `shell(command, cwd)` runs a command in the workspace.

Destructive tools are not exposed to the model in the first version:

- `delete_path(path)`
- `move_path(from, to)`

They can exist as internal designs later, but hiding them from the model reduces
first-version risk.

### `internal/permissions`

Permissions are centralized:

```go
type PermissionPolicy interface {
    Check(ctx context.Context, req Request) (Decision, error)
}
```

Each tool creates a structured permission request:

```text
action: read | write | patch | shell
target: file path, directory path, patch summary, or command
risk: read | write | execute | destructive | network | outside_workspace
reason: model-supplied reason
preview: command text, file summary, or diff preview
```

MVP policy:

- allow read-only workspace actions;
- ask before every patch;
- ask before every write;
- ask before every shell command;
- reject workspace path escapes;
- reject writes outside the workspace;
- reject shell working directories outside the workspace.

The REPL confirmation prompt shows:

- what the agent wants to do;
- why it wants to do it;
- affected paths or command text;
- risk classification;
- `y` and `n` choices.

Session-wide `always allow` decisions are intentionally omitted from the first
version.

### `internal/session`

Responsibilities:

- Create a session id.
- Store workspace path, provider, model, message history, tool calls, tool result
  summaries, and timestamps.
- Save after each completed turn.
- Load the current session on startup when appropriate.

Files:

```text
.codeworld/sessions/<timestamp>.json
.codeworld/current-session.json
```

Large tool results are stored as truncated summaries with metadata, not full
unbounded output.

### `internal/workspace`

Responsibilities:

- Resolve and validate workspace paths.
- Prevent path traversal outside the workspace.
- Detect whether the workspace is a git repository.
- Build a lightweight workspace summary for the model.
- Exclude heavy directories from scans, such as `.git`, `node_modules`, `vendor`,
  build outputs, and cache directories.

Startup detection includes:

- git repository status;
- `go.mod`;
- `README.md`;
- `Makefile`;
- `package.json`;
- a bounded file tree summary.

If the workspace is not a git repository, the REPL warns the user and offers to
run `git init`. Running `git init` requires confirmation because it writes to the
workspace.

## File Editing Strategy

The preferred editing path is `apply_patch` with unified diff patches.

Patch flow:

```text
Model requests apply_patch
  -> parse patch and list affected files
  -> verify every affected path is inside the workspace
  -> show diff preview to the user
  -> ask for confirmation
  -> apply patch
  -> return success or failure to the model
  -> include updated git diff summary when available
```

Using patches gives the user a reviewable artifact before writes happen and makes
rollback simpler.

`write_file` is allowed but should be used mainly for:

- creating a new file;
- writing a small config file;
- bootstrapping the project skeleton.

For existing source files, the system prompt should ask the model to prefer
patches over full-file rewrites.

The first implementation can use `git apply --check` and `git apply` when the
workspace is a git repository. Non-git patch application can be added later if
needed.

## Shell Strategy

All shell commands require confirmation in the first version.

Rules:

- run commands only inside the workspace;
- default timeout is 60 seconds;
- capture stdout, stderr, exit code, duration, and truncation status;
- return command failures to the model as tool results;
- do not support long-running background processes in MVP;
- do not automatically whitelist commands.

Risk classification is informational in MVP:

```text
read-like: ls, cat, pwd, git status
test-like: go test, npm test, pytest
network: curl, wget, go get, npm install
destructive: rm, mv, git reset, git clean
unknown: other commands
```

The policy asks for confirmation regardless of classification, but the prompt
shows the classification so the user can make an informed decision.

## Context Management

Each model request is assembled from four layers:

1. System prompt
2. Workspace context
3. Session messages
4. Tool results

The system prompt instructs the agent to:

- inspect before editing;
- keep changes narrow;
- prefer patches;
- request shell commands only when useful;
- never invent command results or file contents;
- explain why a permissioned action is needed;
- stop when it reaches a final answer or cannot make progress.

Workspace context includes the current directory, git status, detected project
type, and bounded file tree summary.

Session messages retain the active conversation. MVP does not include automatic
summarization. When token or size limits are reached, the system truncates older
or large tool-result content and tells the model what was truncated.

Tool results must include:

- tool name;
- arguments summary;
- affected path or command;
- success/failure;
- output preview;
- truncation metadata.

## Error Handling

Expected errors are part of the agent loop:

- file not found;
- path outside workspace;
- permission denied by user;
- patch failed to apply;
- shell command timed out;
- model provider returned an error;
- tool output was truncated.

Tool-level errors are converted into tool result messages when possible, so the
model can recover or choose another step.

Fatal errors are limited to situations where the program cannot continue:

- missing `DEEPSEEK_API_KEY` when a model call is needed;
- invalid config that cannot be defaulted;
- session store cannot be read or written after retry;
- terminal input stream closes unexpectedly.

Provider errors must never print API keys or full authorization headers.

## Configuration

Configuration priority:

```text
CLI flag > environment variable > project config > user config > default value
```

MVP supports environment variables and project config. User config and rich CLI
flags can be added later.

Project config:

```toml
# .codeworld/config.toml
provider = "deepseek"
model = "deepseek-v4-pro"
max_steps = 20
workspace = "."
```

Credential:

```bash
DEEPSEEK_API_KEY=...
```

The project config must not contain API keys or OAuth tokens.

## MVP Scope

Included:

- interactive REPL;
- DeepSeek provider with `deepseek-v4-pro` default;
- read-only workspace tools;
- patch, write, and shell tools with confirmation;
- git status and diff support;
- session JSON persistence;
- workspace detection;
- conservative permission policy;
- unit tests for core boundaries.

Excluded from MVP:

- single-shot `codeworld run`;
- multi-provider support;
- plugin system;
- MCP;
- multi-agent planner/coder/reviewer graph;
- background daemons;
- IDE integration;
- web UI;
- automatic memory summarization;
- session-wide command allowlists;
- direct use of Codex OAuth as a model credential.

## Test Strategy

Unit tests:

- `permissions`: read/write/shell/patch decisions, workspace escape rejection,
  and user-denied actions.
- `workspace`: path normalization, path traversal prevention, git detection, and
  bounded file tree generation.
- `tools`: file read pagination, search truncation, git diff/status behavior,
  patch validation, shell timeout behavior, and output truncation.
- `agent`: mock model returns tool calls; loop executes tools; tool failures are
  passed back to the model; max step limit stops runaway loops.
- `session`: save and load session JSON; truncate large tool results.
- `deepseek`: request construction and response parsing using fixtures.

Manual smoke test:

1. Set `DEEPSEEK_API_KEY`.
2. Run `codeworld` in a small git repository.
3. Ask it to inspect the project.
4. Ask it to make a small source change.
5. Confirm the patch.
6. Ask it to run the relevant tests.
7. Confirm the shell command.
8. Verify final response, git diff, and saved session.

## Acceptance Criteria

- `codeworld` starts an interactive REPL.
- The REPL can send a user request to DeepSeek using `deepseek-v4-pro`.
- The model can call read-only tools without prompting.
- The model cannot write files without user confirmation.
- The model cannot run shell commands without user confirmation.
- Confirmed patches apply inside the workspace.
- Rejected actions are reported back to the model.
- Command output includes exit code and truncation metadata.
- Session history is saved under `.codeworld`.
- Core packages have focused unit tests.

## Evolution Path

1. Add `codeworld run "..."` as a single-shot mode over the same runtime.
2. Add session-scoped command approvals for low-risk commands such as `gofmt` and
   `go test`.
3. Add provider implementations for OpenAI API, Anthropic, and local models.
4. Add Codex delegate support by wrapping `codex exec --json` or the Codex SDK as
   an external tool.
5. Add summarization and repository indexing for larger projects.
6. Add plugin and MCP integration.
7. Add multi-agent workflows once the single-agent loop is stable.

## Open Implementation Constraints

The implementation plan should keep the first build small:

- use the standard library where practical;
- avoid a framework-heavy terminal UI;
- start with explicit interfaces and simple structs;
- test with mocks before adding real provider smoke tests;
- avoid implementing provider abstractions beyond what the DeepSeek provider
  needs immediately;
- keep every tool independently testable.
