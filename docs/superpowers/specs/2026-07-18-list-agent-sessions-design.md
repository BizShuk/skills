# List Agent Sessions Design

## Goal

Add a `skills session` Cobra subcommand that lists agent sessions associated
with the current working directory. The command scans agent-owned session
directories from provider configuration, filters by reliable working-directory
metadata, and renders a deterministic human-readable table.

## Scope

- `skills session` accepts no positional arguments and uses the process current
  working directory as its project scope.
- The command lists sessions from all configured providers that expose a
  supported session format.
- Results are sorted by `LastActivity` descending, then agent name, then
  session ID.
- Each result contains the agent name, session ID, start time, last activity
  time, and source path.
- Missing session directories, unsupported provider formats, malformed files,
  and unreadable individual session files do not prevent other sources from
  being listed. A completely unreadable configured root is returned as an
  error only when it is not a missing directory.
- No new external dependency or interactive UI is introduced.

## Provider Configuration

Add `sessionDirs []string` to `svc/agent/providers/*.json`, represented by
`SessionDirs []string` on `agent.Provider` and `agent.Agent`. Paths beginning
with `~/` are expanded at runtime using the existing agent path expansion
convention.

Initial configured roots:

| Provider | Session roots |
| --- | --- |
| `claude-code` | `~/.claude/projects` |
| `codex` | `~/.codex/sessions`, `~/.codex/archived_sessions` |
| `antigravity` | `~/.gemini/antigravity-ide/brain` |
| `antigravity-cli` | `~/.gemini/antigravity-cli/brain` |
| `grok` | `~/.grok/sessions` |
| `hermes-agent` | `~/.hermes/sessions` |
| `opencode` | `~/.local/share/opencode/storage` |
| `pi` | `~/.pi/agent/sessions` |

The provider table is the only source of these paths. The session service does
not contain agent-specific home-directory literals.

## Architecture

```text
cmd/session.go
    -> svc/session.List(cwd)
        -> agent.Agents() / Provider.SessionDirs
        -> provider-specific session discoverers
        -> model.AgentSession
    -> svc/session.Format(w, sessions)
```

`model.AgentSession` is a data-only value. `svc/session` owns filesystem
walking, JSONL/JSON metadata extraction, path normalization, source-specific
parsing, sorting, and formatting. `cmd/session.go` only resolves the current
directory, invokes the service, and writes to Cobra's configured output
writer.

The service uses small source discoverers keyed by `agent.Type`:

- Claude discovers `.jsonl` files under the configured project root. A file is
  included when a record contains a `cwd` equal to the normalized current
  directory; `sessionId` identifies the session.
- Codex discovers `.jsonl` files under both configured roots. A
  `session_meta.payload.cwd` match is authoritative; the ID comes from
  `payload.id`, with the filename as fallback.
- Grok discovers URL-escaped project directories beneath the configured root,
  then reads the child session metadata for ID and timestamps. A matching
  `prompt_context.working_directory` is accepted as an additional check.
- Antigravity and Hermes use their configured transcript/session roots and
  recursively inspect structured working-directory fields such as `cwd`,
  `working_directory`, `workdir`, and `Cwd`. This is best-effort because the
  formats do not expose one stable top-level project field.
- OpenCode and Pi are scanned only when their files expose the same explicit
  working-directory metadata. Session-like files without that evidence are
  ignored rather than attributed to the current project.

All timestamp extraction is best effort. The earliest recognized timestamp is
`StartedAt`; the latest is `LastActivity`. A missing timestamp renders as `-`.
The source path is retained so a user can inspect the underlying session.

## Error Handling

- Normalize both the requested cwd and discovered paths with `filepath.Abs`,
  `filepath.Clean`, and symlink evaluation where available.
- Skip invalid JSON lines while continuing to scan the same session file.
- Skip a session file when it has no ID or no matching working-directory
  evidence.
- Skip missing roots silently.
- Wrap non-missing filesystem errors with provider and root context.
- Return an empty result without error when no provider has a matching session.

## CLI Output

The command uses a stable tabular output with these columns:

```text
AGENT        SESSION ID                              STARTED              LAST ACTIVITY        PATH
claude-code  774ba069-fde1-452e-8aad-1a8868eb822c  2026-07-18 08:12:04  2026-07-18 08:35:19  /Users/.../session.jsonl
```

When no sessions match:

```text
no agent sessions found for /absolute/current/working/directory
```

The formatter writes to an injected `io.Writer`, making command output tests
independent from process stdout.

## Testing

- Provider tests verify `sessionDirs` JSON decoding and every embedded provider
  file remains valid.
- Session service tests use temporary fixture roots for Claude, Codex, Grok,
  and structured metadata fallback cases. They cover cwd filtering, archived
  roots, malformed lines, missing roots, timestamp ordering, and stable sort
  order.
- Command tests verify `skills session` is registered, writes to the Cobra
  output writer, and prints the empty-result message.
- Verification includes `go test ./...`, `go vet ./...`, the explicit binary
  build, and `git diff --check`.

## Non-goals

- No session resume, deletion, mutation, or interactive selection.
- No token or usage aggregation; that remains the responsibility of
  `skills stats`.
- No attempt to infer project ownership from arbitrary prompt text.

