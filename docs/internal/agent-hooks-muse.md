# Muse Hooks

Split out of [Agent Hooks](agent-hooks.md). That file owns the hook
*behavior*; this one owns the Muse Code *envelope*: what Muse delivers on
stdin, what it honors on stdout, and where the port differs from Claude.
Read both before touching `.muse/hooks/`.

## Layout

`.muse/hooks.json` (committed) routes Muse lifecycle events to wrappers in
`.muse/hooks/`. Each wrapper translates the Muse envelope to the Claude
shape and delegates to the same file under `.claude/hooks/`, so one logic
copy serves both harnesses. Wrappers are real files, not symlinks: hook
payloads are per-harness (the canon rule in `agent-hooks.md` applies).

| Wrapper | Event | Muse matcher | Delegates to |
|---|---|---|---|
| `goal-continue.sh` | Stop | — | `goal-continue.sh` |
| `goal-refresh.sh` | UserPromptSubmit | — | `goal-refresh.sh` |
| `skill-nudge.sh` | PreToolUse | `write_file`, `edit_file` (separate entries) | `skill-nudge.sh` |
| `skill-loaded.sh` | PostToolUse | `read_skill` | `skill-loaded.sh` |
| `guard-live-gate.sh` | PreToolUse | `bash` | `guard-live-gate.sh` |
| `on-compact.sh` | SessionStart, PreCompact | — | `on-compact.sh` |
| `eshu-doc-staleness.sh` | PostToolUse | `write_file`, `edit_file` | `eshu-doc-staleness.sh` |

`scripts/test-muse-hooks.sh` feeds recorded Muse payloads through the
wrappers and asserts translation plus verdict, including a parity section
that runs the same goal layout against both harnesses and requires the same
verdict. It runs in CI as part of `verify-agent-hygiene.yml`.

## Envelope, recorded live

Payloads below were captured from real `muse exec` sessions with a
stdin-dumping hook, not guessed. Full captures live in the session log of
the port; the shapes that matter:

- Session lifecycle keys match Claude's: `session_id`, `cwd`,
  `hook_event_name`, `transcript_path`. No `prompt_id` anywhere; the
  per-turn key is `turn_id`, stable across the stops of one turn
  (`stop_hook_active` flips false→true on the second).
- `UserPromptSubmit` carries `prompt`. `Stop` carries
  `last_assistant_message` and `stop_hook_active`.
- `PreToolUse`/`PostToolUse` carry `tool_name`, `tool_input`,
  `tool_use_id` (and `tool_response` on PostToolUse). Tool names differ:
  `write_file` (path at `tool_input.path`), `edit_file` (same),
  `bash` (command at `tool_input.command`, same as Claude), `read_skill`
  (id at `tool_input.name`). The edit path may be **relative to the
  payload `cwd`** (observed live: `tooltest.txt` while Claude sends
  absolute paths), so the skill-nudge wrapper joins a relative path onto
  `cwd` before delegating -- without that the scope walk starts at `.`
  and the nudge silently never fires.

Output protocol is Claude-compatible where it matters:

- `{"decision":"block","reason":...}` on Stop continues the turn; the
  reason enters context (proven: the continued turn obeyed an injected
  instruction). Second Stop arrives with `stop_hook_active: true`.
- `{"hookSpecificOutput":{"hookEventName":...,"additionalContext":...}}`
  is injected into context (proven with a marker word).
- PreToolUse exit 2 fails the tool call with the hook's stderr surfaced
  ("blocked by hook", proven against `bash`).
- `matcher` filters on the exact tool name (proven: a nonsense matcher
  never ran; `bash` ran only for bash). Regex alternation (`a|b`) is
  *unproven*, so `hooks.json` uses one entry per tool rather than
  depending on it.

## Deliberate differences and limits

- **Budget key.** `goal-continue.sh` keys its nudge counter on
  `prompt_id`; the wrapper fills it from `turn_id`. Suite asserts a new
  `turn_id` starts a fresh budget.
- **Fresh SessionStart stays silent.** Muse delivers `source: "startup"`
  on a fresh session. The `on-compact` wrapper exits 0 there: emitting
  "CONTEXT WAS COMPACTED" on a new session would be a lie, and clearing
  markers is pointless. Any other (or missing) source delegates, and
  `PreCompact` always delegates. The resume-source value is inferred from
  the `startup` contrast, not yet observed live.
- **No compaction round-trip observed.** Marker clearing and context
  re-injection on `PreCompact` are unit-tested, not live-proven; a real
  compaction was not forced during the port.
- **Environment is cleared.** Hooks see only HOME, LANG, LOGNAME, PATH,
  PWD, SHELL, SHLVL, TERM, TMPDIR, USER (recorded live). Exported knobs
  (`CLAUDE_GOAL_OFF`, `CLAUDE_GOAL_MAX_NUDGES`, `CLAUDE_GOAL_FILE`,
  `CLAUDE_GOAL_CONSENT`) do *not* survive, so under Muse the goal file
  itself is the only control plane: DONE/BLOCKED/CONSENT lines work,
  env overrides do not. Inline `VAR=` prefixes in the hook command do
  work (the shell applies them). The per-call `CLAUDE_HOOK_ALLOW=1`
  override keeps working because it is command text, not environment.
- **`transcript_path` is null in `exec` mode.** The no-progress budget
  then degrades to its fixed bound (missing transcript reports no
  progress by design, same as Claude).
- **Observer sessions have their own ids.** Background observer tool
  calls arrive with distinct `session_id`s, so their skill markers never
  satisfy the lead session's nudge. Same as Claude's subagent behavior.
- **Goal files are shared.** Both harnesses read and write the same
  `.claude/active-goal*` files: a `/goal` set under Claude is enforced
  under Muse and vice versa.

## Trust

Project hooks load only in a trusted workspace. An untrusted session
skips them silently (observed: single-turn exit, no enforcement, no
warning naming hooks). `muse exec --trust-workspace` loads them for one
run; persistent trust is the workspace trust store. User-level hooks in
`~/.config/muse/settings.json` need no trust step. Settings (both files)
are validated at session startup; a malformed project file contributes
no handlers and warns, a malformed user file fails validation.

Admission is observable in the session log: `hook.admission ... handlers=N
runnable=N` (`cli-*.log`). A trusted worktree session with this port
admits all project handlers (`ready`, all runnable); an untrusted one
shows no enforcement and no hook warning.
