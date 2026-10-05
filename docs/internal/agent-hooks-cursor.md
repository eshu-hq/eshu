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
| none (the adapter itself) | none | `subagentStart` | none | side effect: links a helper to its parent, always allows |
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

Every Claude payload gets `session_id` from Cursor's `conversation_id`
(else its `session_id`), the per-chat key. The two skill events,
`preToolUse` and `postToolUse`, use the helper-family key instead (see
[Helper agents](#helper-agents)), which is the same id for a chat with no
helpers. `cwd` comes from the payload `cwd`, else the first
`workspace_roots` entry, else `CURSOR_PROJECT_DIR`. All events resolve them
the same way. That matters for goals: `goal-refresh.sh` writes
`<cwd>/.claude/active-goal.<session_id>` and `goal-continue.sh` must find the
same file.

| Cursor event | Claude payload | Cursor answer |
|---|---|---|
| `beforeShellExecution` | `tool_name: Bash`, `tool_input.command` | `permission` allow or deny, with the hook's message |
| `preToolUse` (`Write`) | `tool_name: Write`, `tool_input.file_path` | `permission` allow or deny |
| `postToolUse` (`Read` of a `SKILL.md`) | `tool_name: Skill`, `tool_input.skill` | `{}` |
| `subagentStart` | none (no hook script runs) | exactly `{"permission": "allow"}` |
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
  that exits with anything but 0 or 2, and an adapter that cannot import
  `scripts/cursor_hook_family.py` (missing, or broken). Each deny says why. A
  missing `python3` blocks too, through `failClosed`. A broken guard must not
  quietly allow.
- **Advisory hooks fail open.** For every other event the adapter prints `{}`
  and writes a note to stderr. `subagentStart` answers
  `{"permission": "allow"}` on every path, including a bad payload, and it
  is not `failClosed`: recording a link must never block a helper.
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
  directly in `<root>/.agents/skills/<id>/` (or the `.claude`, `.codex`,
  `.cursor` copies), and `<id>` is a skill under `<root>/.agents/skills`.
  `<root>` is the project dir or the top of any git worktree that
  `git worktree list` shows for the project (the same git common dir). So an
  agent in Cursor opened on the main checkout can load a skill from the
  worktree it works in, which the repo's workflow requires. A skill file in
  another repo, a home folder, or behind a symlink out of the tree does not
  count. If git is missing or fails, only the project dir counts (fail safe:
  the nudge keeps blocking). The nudge's deny message and the compaction note
  tell a Cursor agent to read the file and say the copy in its worktree
  counts too. The override file named in the message still works. That
  Cursor's agent reads `SKILL.md` through the `Read` tool is assumed, not
  observed live.
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

## Helper agents

Claude Code gives a subagent its parent's `session_id`, so a skill a helper
loads unlocks the parent's edits and the reverse. A Cursor helper (the `Task`
tool) runs under its own id, so without help their skill markers would never
meet. The adapter matches Claude for loaded skills only. The logic is in
`scripts/cursor_hook_family.py`.

- `subagentStart` records `subagent_id -> parent_conversation_id`, and
  `conversation_id -> parent_conversation_id` when the start payload's
  `conversation_id` differs from the parent. Each link is one small file,
  `/tmp/eshu-cursor-link-<id>`, written atomically next to the markers. The
  parent is also marked as a family root (`/tmp/eshu-cursor-root-<id>`). Ids
  must match `[A-Za-z0-9][A-Za-z0-9._:-]{0,127}` with no `..`; anything else
  is skipped. A link that would make a loop, or a chain of more than 16
  links, is refused.
- The skill events (`preToolUse`, `postToolUse`) follow the links from the
  payload's `conversation_id`, `session_id` and `subagent_id`, in that order,
  and use the root of the first linked one. Failing that, an id that is a
  family root is used, which covers a helper that carries the parent's
  `session_id` beside a new `conversation_id`. A chain of up to 16 links
  resolves; a loop or a 17th link is ignored (the id is used unlinked). With
  no links the key is `conversation_id`, as before.
- Goals and compaction stay per chat. Claude fires no Stop,
  UserPromptSubmit or SessionStart hook for a subagent, so `stop`,
  `beforeSubmitPrompt` and `preCompact` keep the chat's own key: a helper
  never sees or retires its parent's goal, and its compaction does not clear
  the family's skill markers.
- A link or root file that is a symlink, a FIFO or another non-regular file,
  or that is not UTF-8 or holds a bad id, counts as no link. The nudge then
  keeps blocking rather than wrongly allowing.
- Link and root files are never removed. They stay in `/tmp` (as
  `eshu-cursor-link-<id>` and `eshu-cursor-root-<id>`), like the skill
  markers. Cursor ids are unique, so a stale file does no harm, and a reboot
  or a `/tmp` cleaner clears them.

NOT_CHECKED live: which id a helper's own tool hooks carry (its
`subagent_id`, a new `conversation_id`, or the parent's `session_id`). The
cursor-agent 2026.10.01 bundle builds the `subagentStart` payload with
`subagent_id` and `parent_conversation_id`, but the code alone does not say
which id the helper's later hooks carry. The adapter handles all three, and
the payload log below shows which one Cursor uses.

**Payload log.** Set `ESHU_CURSOR_HOOK_LOG` to a file path and the adapter
appends one JSON line per event: `event`, the sorted top-level key names
(`keys`), the values of `conversation_id`, `session_id`, `subagent_id`,
`parent_conversation_id`, and the resolved `key`. Nothing else is written:
no prompts, commands, file contents or paths. It is off by default. To check
the live behavior, set it, start a chat that uses a helper, and compare the
helper's `postToolUse` lines with its `subagentStart` line. The same log
shows whether Cursor fires `stop`, `beforeSubmitPrompt` or `preCompact` for
a helper (NOT_CHECKED live).

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
own refusal on the same seed. Its sourced companion,
`scripts/test-cursor-hooks-helper-cases.sh`, covers helper links (the helper
id in each of the three fields, unrelated chats, goals and compaction kept
per chat, loops, the 16-link bound, bad state files, malformed starts), a
SKILL.md read in a real second `git worktree` of a temp repo, and the
payload log. It runs in the agent-canon self-test command
and in `verify-agent-hygiene.yml`. Set `CURSOR_HOOK_ADAPTER` to run a
modified adapter through the same suite; keep the copy in `scripts/` so it
finds `cursor_hook_family.py` and the hook scripts.
