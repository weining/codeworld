# Codeworld Codex-Style TUI Design

Date: 2026-06-28

## Goal

Replace the current `codeworld tui` status-shell wrapper with a real full-screen terminal application modeled after Codex CLI's interactive terminal workflow. The first implementation should feel materially closer to Codex while staying compatible with the current agent, tool, permission, session, and provider interfaces.

The target for this iteration is a Bubble Tea full-screen TUI with non-streaming model output. Streaming model text is explicitly a later phase because the current `model.Client` interface returns complete `GenerateResponse` values.

## Source Reference

The Codex manual describes the CLI as a full-screen terminal UI where users can send prompts through a composer, watch actions in real time, approve or reject steps inline, use slash commands, inspect status and token usage, review diffs, clear conversations, and exit with `/exit` or Ctrl+C. Relevant cached manual sections:

- `Codex CLI features`, lines 5474-5742 in `/var/folders/sr/l1nwyz_x7wb36kq76pzpcyr00000gn/T/openai-docs-cache/codex-manual.md`
- `Slash commands in Codex CLI`, lines 6402-6601 in the same file
- `Agent approvals & security`, lines 1602-1842 in the same file

The design uses those behaviors as product inspiration, not as a claim of feature parity.

## Scope

Implement in this pass:

- `codeworld tui` launches a full-screen Bubble Tea app.
- Main transcript view shows user turns, assistant responses, tool execution events, permission results, errors, and system notices.
- Bottom composer supports text entry and submission.
- Top or bottom status area permanently shows provider, model, workspace, message count, approvals count, and token usage split into input, output, cache, and total.
- Permission prompts appear inline in the TUI with target, risk, reason, optional preview, and choices `y`, `n`, `a` for session approval when the action is shell.
- Existing slash commands continue to work: `/help`, `/model`, `/status`, `/diff`, `/clear`, `/exit`.
- Existing `codeworld run <task>` remains non-interactive.
- A plain REPL path remains available for tests and fallback.

Do not implement in this pass:

- Streaming provider output.
- Syntax-highlighted markdown or diff rendering beyond plain text formatting.
- Mouse support.
- Custom themes.
- Prompt history search.
- Resume picker.
- Image inputs.
- Remote app-server mode.
- New provider behavior.

## User Experience

Launching `codeworld tui` opens a full-screen layout:

```text
codeworld  deepseek/deepseek-v4-pro  input=120 output=30 cache=80 total=150  approvals=1
workspace: /path/to/repo
--------------------------------------------------------------------------------
user
  fix the failing tests

assistant
  I will inspect the test failure first.

tool read_file start  target=internal/foo/foo_test.go  risk=read
tool read_file ok

permission required  risk=execute
target: mise exec -- go test ./...
reason: shell command
[y] allow once   [n] deny   [a] allow similar shell command this session
--------------------------------------------------------------------------------
> _
```

While a turn is running, the composer is disabled for direct submission and the status area shows `running`. The first version may reject new input during a running turn. Queuing follow-up prompts is left for a later pass.

The app should remain keyboard-first:

- `Enter`: submit current input.
- `Esc`: cancel a permission prompt or clear transient UI state.
- `Ctrl+C`: exit when idle; cancel the running turn when a cancellation path exists.
- `PageUp/PageDown` or `Ctrl+U/Ctrl+D`: scroll transcript.
- `/exit`: exit.

Multi-line input can be deferred unless Bubble Tea's text area makes it cheap. If included, prefer `Alt+Enter` for newline and plain `Enter` for submit.

## Architecture

Add an `internal/tui` package with a Bubble Tea model instead of routing through `repl.REPL`.

Core types:

- `Model`: Bubble Tea state, transcript items, current input, viewport dimensions, status snapshot, pending permission, running state, and runtime reference.
- `TranscriptItem`: typed entry for user text, assistant text, tool event, permission event, command output, error, and notice.
- `Status`: provider, model, workspace, message count, approvals, usage, and running state.
- `RunnerAdapter`: owns calls into `agent.Runner.RunTurn`, session persistence, summarization, and usage accumulation.
- `TUIConfirmer`: implements `agent.Confirmer` by sending a permission request into the Bubble Tea model and waiting for a user decision.
- `TUIReporter`: implements `agent.ToolReporter` and appends tool events into the transcript.

The existing `app.Runtime` stays the composition root for config, workspace, session, model client, tools, policy, and diff function. `tui.Run(ctx, *app.Runtime)` should adapt the runtime into the new Bubble Tea program.

## Data Flow

1. CLI builds `app.Runtime`.
2. `tui.Run` creates a Bubble Tea model with current session messages and usage.
3. User submits text in the composer.
4. TUI app appends a user transcript item and starts a background turn command.
5. Background command calls `RunnerAdapter.RunTurn`.
6. Tool events are delivered through `TUIReporter` and appended to the transcript.
7. Permission requests are delivered through `TUIConfirmer`; the running command blocks until the user chooses allow, deny, or allow-session.
8. On final response, the adapter updates messages, usage, session state, saves the session, and optionally summarizes.
9. TUI updates the status line and appends assistant output.

This preserves the existing `agent.Runner` behavior and avoids mixing Bubble Tea rendering logic into the agent.

## Commands

Slash command handling should move behind a reusable command function so REPL and TUI do not drift. The first implementation can duplicate minimal command logic if extracting the shared handler becomes too large, but the behavior must match existing REPL semantics.

Required command behavior:

- `/help`: show supported commands in transcript.
- `/model`: show current model.
- `/model <name>`: update session model and runner model name.
- `/status`: append current status including token split.
- `/diff`: append current git diff or `no diff`.
- `/clear`: clear transcript, messages, approvals, and usage, then save session.
- `/exit`: quit the TUI.

## Error Handling

- If Bubble Tea cannot start, return the error to CLI.
- If the model call fails, append an error transcript item and return to idle state.
- If session save fails, append a warning transcript item but keep the UI running.
- If summarization fails, append a warning transcript item.
- If permission input is cancelled, deny the pending request.
- If terminal size is too small, render a compact fallback with status and composer rather than crashing.

## Testing

Use tests that exercise state transitions without requiring a real terminal:

- Status rendering includes input, output, cache, and total tokens.
- Submitting input creates a user transcript item and starts a turn command.
- Successful turn appends assistant output and updates usage.
- Tool reporter appends start/success/error/denied items.
- Permission confirmer blocks until a decision message and maps `y`, `n`, `a` correctly.
- Slash commands update state and transcript.
- CLI `tui` command dispatches into `tui.Run`.

Keep the current full verification gates:

```bash
mise exec -- go test -count=1 ./...
mise exec -- go build -o /private/tmp/codeworld-build ./cmd/codeworld
```

## Implementation Notes

Add dependencies only for the full-screen TUI:

- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`
- `github.com/charmbracelet/lipgloss`

If dependency download is blocked by sandbox network restrictions, rerun the `go get` command with scoped approval.

The existing status-shell `internal/tui` implementation should be replaced rather than extended. The plain `repl.REPL` remains useful for fallback and tests, but `codeworld tui` should not call `repl.REPL.Run` after this redesign.
