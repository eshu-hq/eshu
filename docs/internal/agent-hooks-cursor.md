# Cursor Hooks

Split out of [Agent Hooks](agent-hooks.md). That file owns the hook
*behavior*. This one owns the Cursor *envelope*: what Cursor sends on stdin,
what it reads back on stdout, and where the port differs from Claude. Read
both before you touch `.cursor/hooks.json` or `scripts/cursor-hook.py`.

## Layout

`.cursor/hooks.json` (committed) wires Cursor's native hook events. Every
entry runs one adapter, `scripts/cursor-hook.py`, with the Cursor event name
and the Claude hook to run:

```text
python3 scripts/cursor-hook.py <cursor-event> .claude/hooks/<hook>.sh
```

The adapter turns the Cursor payload into the Claude shape, runs the
unchanged file under `.claude/hooks/`, and turns the Claude answer back into
Cursor JSON. So one logic copy serves Claude, Muse, and Cursor. The Muse port
uses one wrapper per hook; Cursor uses one adapter for all of them, because
its envelope differs from Claude's in the same few ways for every hook.

Cursor runs project hooks from the project root, so the commands are plain
relative paths with no `$VAR` or `$(...)`. They work whether or not Cursor
starts them through a shell.

## Mapping

| Claude hook | Claude event | Cursor event | Cursor matcher | Tier in Cursor |
|---|---|---|---|---|
| `guard-live-gate.sh` | PreToolUse `Bash` | `beforeShellExecution` | none | guard, `failClosed: true` |
| `skill-nudge.sh` | PreToolUse `Edit\|MultiEdit\|Write` | `preToolUse` | `Write` | guard, `failClosed: true` |
| `skill-loaded.sh` | PostToolUse `Skill` | `postToolUse` | `Read` | side effect (see below) |
| `eshu-doc-staleness.sh` | PostToolUse `Edit\|MultiEdit\|Write` | `afterFileEdit` | none | side effect |
| `on-compact.sh` | SessionStart `compact\|resume` | `preCompact` | none | side effect, note to the user |
| `goal-refresh.sh` | UserPromptSubmit | `beforeSubmitPrompt` | none | side effect only (goal file) |
| `goal-continue.sh` | Stop | `stop` | none | follow-up message |
| `goal-role-router.py claude` | UserPromptSubmit | not portable | | |
| `goal-role-router.py claude-dispatch` | PreToolUse `Agent` | not portable | | |
| `on-compact.sh` on resume | SessionStart `resume` | not portable | | |

The "not portable" rows, and why:

- **`goal-role-router.py claude`, and context injection in general.** This
  hook only adds text to the prompt's context. Cursor's docs list no context
  field for `beforeSubmitPrompt`: only `continue` and `user_message`, and
  `user_message` is shown only when `continue` is false. cursor-agent
  2026.10.01 accepts an `additional_context` field there without documenting
  it. This port does not rely on undocumented behavior, so it sends none
  (whether the model would see it is NOT_CHECKED live). The same holds for the
  goal that `goal-refresh.sh` restates on each prompt.
- **`goal-role-router.py claude-dispatch`** drops a model override from a
  subagent call so the role's own model applies. Cursor could do the rewrite:
  `preToolUse` with matcher `Task` has a documented `updated_input`. It is not
  wired because the Cursor role files in `.cursor/agents` use
  `model: inherit`, so there is no per-role model to route to.
- **`on-compact.sh` on resume.** Claude runs it when a session resumes.
  Cursor's `sessionStart` payload says nothing about resume or compaction, so
  wiring it there would print "context was compacted" on every new chat. Only
  the compaction half is wired, through `preCompact`.

## How the adapter translates

Every Claude payload gets `session_id` from Cursor's `conversation_id` and
`cwd` from the payload `cwd`, else the first `workspace_roots` entry, else
`CURSOR_PROJECT_DIR`. All events resolve them the same way. That matters for
goals: `goal-refresh.sh` writes `<cwd>/.claude/active-goal.<session_id>` and
`goal-continue.sh` must find the same file.

| Cursor event | Claude payload | Cursor answer |
|---|---|---|
| `beforeShellExecution` | `tool_name: Bash`, `tool_input.command` | `permission` allow or deny, with the hook's message |
| `preToolUse` (`Write`) | `tool_name: Write`, `tool_input.file_path` | `permission` allow or deny |
| `postToolUse` (`Read` of a `SKILL.md`) | `tool_name: Skill`, `tool_input.skill` | `{}` |
| `afterFileEdit` | `tool_name: Edit`, `tool_input.file_path` | `{}` |
| `preCompact` | `SessionStart`, `source: compact` | `user_message` |
| `beforeSubmitPrompt` | `UserPromptSubmit`, `prompt`, `prompt_id` | exactly `{"continue": true}` |
| `stop` | `Stop`, `prompt_id`, `stop_hook_active` | `followup_message` when the goal is open |

A hook that exits 2 becomes a deny (with its stderr as both messages) on a
permission event, a `followup_message` on `stop`, and `continue: false` on
`beforeSubmitPrompt` (with the message as `user_message`). A Claude
`{"decision":"block"}` maps the same way. Messages built from a hook's output
are cut at 20000 characters.

## Failure rules

- **Guards fail closed.** `beforeShellExecution` and `preToolUse` are
  `failClosed: true` in `hooks.json`, with Claude's default 60-second
  timeout so a slow `lsof` probe is not turned into a deny Claude would not
  give. A bad, empty or unreadable payload denies, and so does a path key
  that is present but not a string. So do a missing hook script and a script
  that exits with anything but 0 or 2. Each deny says why. A missing `python3` blocks too,
  through `failClosed`. A broken guard must not quietly allow.
- **Advisory hooks fail open.** For every other event the adapter prints `{}`
  and writes a note to stderr.
- **Exactly one JSON object.** Cursor treats invalid JSON from a permission
  hook as a block, so the adapter always prints one object and keeps the
  child's own stdout to itself.

## Deliberate differences and limits

- **The edit path.** cursor-agent 2026.10.01 sends a `Write` as
  `{file_path, content}` (its bundle builds the tool input from the edit's
  path and text). The adapter reads `tool_input.file_path`, then the same
  fallbacks as the sibling adapters (`path`, `target_file`, `filePath`,
  `file`), then the payload's own `file_path`. A relative path is joined onto
  `cwd`. A present key that is not a string denies. With no path key at all
  the edit is allowed with a note on stderr, the same as `skill-nudge.sh`
  does for a Claude payload with no path.
- **Skills load by reading `SKILL.md`.** Cursor has no Skill tool, so
  nothing would ever record a loaded skill and `skill-nudge.sh` would block
  governed edits for good. The adapter treats a `Read` of a `SKILL.md` as
  loading `<id>` only when its real path (symlinks and `../` resolved) sits
  directly in this project's `.agents/skills/<id>/` (or the `.claude`,
  `.codex`, `.cursor` copies), and `<id>` is a skill under `.agents/skills`.
  A skill file in another repo, a home folder, or behind a symlink out of the
  project does not count. The nudge's deny message and the compaction note
  tell a Cursor agent to read the file. The override file named in the
  message still works. That Cursor's agent reads `SKILL.md` through the
  `Read` tool is assumed, not observed live.
- **The goal is not restated on each prompt.** `goal-refresh.sh` still
  writes and retires the goal file, but its restated goal is dropped (see the
  not-portable rows). The goal reaches the agent through the `stop`
  follow-up. The compaction note from `on-compact.sh` arrives as the
  `preCompact` `user_message`. `/goal <text>` may be taken by Cursor as a
  slash command; `GOAL: <text>` always reaches the hook.
- **Budget key.** `goal-continue.sh` keys its nudge counter on `prompt_id`;
  the adapter fills it from `generation_id`. The counter looks for
  `"type":"tool_use"` in the transcript to see progress, and Cursor's
  transcript format is not verified. NOT_CHECKED: that `generation_id` stays
  the same across the follow-ups Cursor submits (if it changes, the counter
  resets on every follow-up), and whether Cursor's `loop_count` and its
  `loop_limit` (default 5) count per conversation or per prompt.
- **Aborted stops are not continued.** Claude fires no Stop hook when the
  user interrupts. The adapter only runs `goal-continue.sh` when the Cursor
  `status` is `completed`. A missing or empty status is not taken as
  completed.
- **Project dir.** The adapter sets `CLAUDE_PROJECT_DIR` for the hook from
  `CURSOR_PROJECT_DIR` when it is not already set. Cursor sets both.

## Third-party loading

Cursor's setting "Include third-party Plugins, Skills, and other configs"
loads Claude Code's `.claude/settings.json` hooks. While it is on, **both**
the Claude-compatible hooks and these native hooks fire, so every guard runs
twice and goal follow-ups can double. Turn that setting off once this port
is in your checkout.

## Tests

`scripts/test-cursor-hooks.sh` pins the wiring (event, matcher,
`failClosed`, command) and feeds Cursor-shaped payloads through the adapter:
a seeded violation per guard is denied, a clean call is allowed, every
answer is one JSON object, and the guard's deny matches the Claude hook's
own refusal on the same seed. It runs in the agent-canon self-test command
and in `verify-agent-hygiene.yml`. Set `CURSOR_HOOK_ADAPTER` to run a
modified adapter through the same suite.
