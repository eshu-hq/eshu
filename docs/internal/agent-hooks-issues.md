# Goal Issue Ledger

Split out of [Agent Hooks](agent-hooks.md). That file owns the other hooks; this
one owns the three that keep an issue filed during a goal from being forgotten.

## Why

Agents working a goal kept filing GitHub issues for what they found, treating the
filing as the deferral. The owner met "these were filed" at the end of the goal,
and the pile reached 100+ issues. AGENTS.md already says every issue created
during a goal joins the goal's backlog. Nothing enforced it, so prose lost again.

The fix has three parts that must agree on which goal an issue belongs to:

| Hook | Event | What it does |
|---|---|---|
| `issue-capture.sh` | PostToolUse on Bash | Notices an issue the call created and appends it to the goal's ledger. Tells the agent to work it or `DEFER` it. |
| `goal-continue.sh` | Stop | When the goal is DONE, refuses the stop while a ledger issue is still open. |
| `goal-refresh.sh` | UserPromptSubmit | Restates open ledger issues every turn, with no network call. |

All three call `.claude/hooks/lib/goal-issues.sh`, which holds the resolver (which
ledger belongs to this session) and `goal_file_retired`, so they decide alike by
construction. `lib/goal-issues.py` reads and writes the ledger bytes and runs
`gh`; `lib/goal-issues-extract.py` decides which tool calls created an issue.
`scripts/test-goal-refresh-hook-parity-cases.sh` runs goal-file layouts through
all three and compares.

## The ledger

A sidecar next to the goal file: `<goal file>.issues`, for example
`.claude/active-goal.<session_id>.issues`. It is deliberately **not** a new
metadata line in the goal file. The goal file's leading-block parser is the
proven bug surface (see "The two hooks must decide alike" in
[Agent Hooks](agent-hooks.md)), and a third kind of line would need an identical
new branch in both hooks.

```text
owner/repo#N <created-iso> <open|closed|deferred> [<ruling path>]
DEFER: [owner/repo]#N <ruling path>
```

The first form is a record. The second is written by the agent to ask for a
deferral and is consumed by the next check, which rewrites the matching record to
`deferred <path>`. Lines that match neither form are ignored and kept. Writers
take a lock file beside the ledger (`<ledger>.lock`, gitignored with the rest of
`.claude/`), because parallel subagents capture while the lead's Stop hook
rewrites.

## What counts as "created an issue"

Capture fires on a Bash call when the output has a line that is **exactly**
`https://github.com/<owner>/<repo>/issues/<N>`. That is what `gh issue create`
prints, and what a script that files issues echoes. A `gh api` POST to
`repos/<owner>/<repo>/issues` also counts, through the `html_url` in its JSON. That
includes `-X POST`, `-f title=...` and `--input body.json` (gh sends a POST whenever
it has a request body, which is the usual REST fallback when its GraphQL path
fails).

It must not capture URLs that merely appear in other output. A gh read verb
(`gh issue list|view|status`, `gh search`, or a `gh api` GET) is excluded, unless
the same command also runs `gh issue create`. A comment posted to an existing
issue returns an `issues/N#issuecomment-...` URL, which is not a bare line.

Each candidate is confirmed with `gh issue view N -R owner/repo --json createdAt`,
and dropped if it is older than 15 minutes. If `gh` fails or is slow the issue is
captured anyway: a spurious ledger line is cheap to close, a missed issue is the
leak this exists to stop.

Capture acts only inside an Eshu checkout, always exits 0, and stays silent when
there is nothing to attach the issue to.

## Which ledger

| Case | Ledger | Why |
|---|---|---|
| The session has a goal (its own file, or the shared one it is held to) | that goal's sidecar | The lookup is the hooks' own, in `gi_resolve_goal_file`. |
| The session has none, and exactly one goal is live in the checkout | that goal's sidecar | A Muse observer or another session filed it for the one lead. |
| The session has none, and two or more goals are live | `.claude/active-goal.orphan.issues` | Guessing would pin the issue on the wrong lead. |
| No goal is live | nothing written; one stderr line per issue | An orphan nobody claims would be adopted by the next unrelated goal. |

With no live goal the hook is not silent. For each issue it prints one line on
stderr, `issue-capture: no live goal in <worktree>; <owner/repo#N> not ledgered`,
so the owner can tell the issue was seen and deliberately held out of any ledger.

A "live goal" is a non-retired `active-goal.<x>` file (never a sidecar, lock, or
temp file), plus the shared `active-goal` when it is headed for a session. An
unheaded shared goal is the owner's and every session resolves it directly.

`goal-refresh.sh` merges the orphan ledger into the lead's own ledger on that
lead's next prompt and restates it. Whichever lead prompts first claims the
orphans; with two leads that can be the wrong one, which the ledger cannot tell
apart. Only a session that holds an active goal merges, so an idle session never
swallows them.

**Orphan hand-off.** A lead whose restatement shows a claimed orphan that belongs
to another live goal moves that line, by hand, into the other goal's sidecar
(`<goal file>.issues`). The other goal's DONE check then refuses on it. The lead
says so in its PR or goal note. There is no code for the move: the lead is the only
party that knows which goal the issue belongs to.

### Recorded, not assumed

Two payload facts were recorded live for this change. The fixtures are
`scripts/testdata/issue-capture/claude-posttool-bash-lead.json`,
`scripts/testdata/issue-capture/claude-posttool-bash-subagent.json`, and
`scripts/testdata/issue-capture/muse-posttool-bash.json`, and
`scripts/test-issue-capture-hook.sh` replays all three:

- **Recorded: Claude Code subagents share the lead's `session_id`.** A subagent's Bash
  PostToolUse carries the same `session_id` as the lead, plus `agent_id` and
  `agent_type`. So a Claude subagent resolves the lead's goal directly and never
  needs the fallback. The fallback exists for Muse observers and other sessions,
  which do have their own ids.
- **Recorded: Muse sends `tool_response` as a JSON-encoded string.** Its object has
  `output` and `exit_code`, where Claude sends an object with `stdout`. The
  extractor reads a string, an object, `stdout`, or `output`.

Recorded with `claude 2.1.310` and `Muse Code 1.4.13`.

## The DONE check

`goal-continue.sh` calls `gi_done_gate` at every place a stop is allowed because
the goal is DONE: the first-line DONE, the DONE beneath the `SESSION:` header, and
the lookup that finds nothing because `/goal done` retired this session's file.
That last one matters most, since `/goal done` is how an owner retires a goal.

An unheaded `DONE` is gated too, at the per-session and shared worktree paths
(`.claude/active-goal.<session>` and `.claude/active-goal`). The agent writes its goal
file, so exempting an unheaded DONE would be an escape: rewrite the file without the
`SESSION:` header, put `DONE` first, and the check never runs while capture keeps
filling the sidecar. A `DONE` headed for **another** session is skipped; it is that
session's. The per-user `~/.claude/active-goal` is the exception: an unheaded `DONE`
there is not gated, so one stale file cannot refuse the stops of every goal-less
session on the machine.

For a hand-written goal the owner has two exits: `CLAUDE_GOAL_OFF=1` on Claude, and,
on both harnesses, editing the sidecar to mark the record `closed` or `deferred`.
Under Muse the environment is cleared, so the sidecar is the exit there.

The check applies `DEFER` lines, then asks `gh issue view N -R owner/repo --json
state` for each record still open:

- **closed** rewrites the record `closed`.
- **open** refuses the stop with `{"decision":"block","reason":...}` naming the
  issues and the two exits: work it, or write `DEFER: #N <path>`.
- **gh unreachable or over the cap** allows the stop and prints
  `goal-continue: NOT_CHECKED issues: ...` on stderr.

### DEFER is a checkable escape

The agent writes the ledger, so a bare `DEFER` would be a self-issued permission
slip, the same trap `BLOCKED:` fell into before it required a live `WATCH=<pid>`.
`<path>` must exist, be non-empty, and mention `#N` (`#9101` does not satisfy
`#91011`). A relative path is tried against the session's cwd, then the directory
above `.claude/`. The path must also not be the ledger, its `.lock` or `.tmp.*`
files, the goal file, or any other `.claude/active-goal*` file. Every ledger record
names its own issue and the goal file can too, so either would pass "names the
issue" for free. Paths are compared by file identity (device and inode), not
spelling, so a symlink, a hardlink, `..`, or another letter case on a
case-insensitive volume does not get around it. A plain copy of the ledger is still
honoured; the stderr echo of every honoured `DEFER` is the mitigation. A `DEFER` that
fails the check is refused, and the refusal names the path and the reason.

### Timeouts

The hook caps its own `gh` calls, because the harness will not: a probe hook that
slept 75 seconds under Muse was not cut off.

| Scope | Cap |
|---|---|
| One `gh` call (DONE check or capture) | 5 s |
| One DONE check, all issues together | 30 s |
| One capture, all candidate issues together | 10 s |

The overrides are `CLAUDE_GOAL_ISSUE_CALL_TIMEOUT` and
`CLAUDE_GOAL_ISSUE_TOTAL_TIMEOUT` (the capture cap is the smaller of 10 s and the
total). **They do not survive Muse:** Muse clears the hook environment, so under
Muse the defaults always apply. The Claude Stop hook timeout in
`.claude/settings.json` is 45 seconds, enough for the 30-second cap plus
interpreter start-up. Muse has no setting for it in this repo, and 30 seconds fits
under the default.

`gh` authenticates from its keyring through `HOME`, which Muse keeps. With no `HOME`,
`gh` exits 4 ("gh auth login"), every check is NOT_CHECKED, and the stop is allowed.

### The refusal is unbounded, so it is loud

The refusal never reads or writes the goal's no-progress nudge counter. That
counter exists to release an agent that is stuck; if the issue refusal shared it,
three refusals would exhaust it and the fourth stop would release open issues
with no message, which reads exactly like a clean finish. So the refusal has no
ceiling of its own. Every exit is a file or GitHub state the agent can reach:
close the issue, write a valid `DEFER`, or (under Muse, where the file is the
control plane) the owner edits the ledger. `CLAUDE_GOAL_OFF=1` stays the owner's
override.

What replaces a ceiling is noise the owner can read:

- **Every refusal** prints the open issues and the consecutive-refusal count on
  stderr: `goal-continue: DONE refused (consecutive refusal N); open issues: ...`.
- **From the 3rd consecutive refusal with no tool calls since the previous one**,
  the block reason adds "Escalate to arbiter-eshu for the DEFER ruling now." A stop
  with tool calls since the last refusal resets the count to 1. A missing
  transcript (Muse exec mode) counts as no progress. The progress signal is the
  same tool-call scan the goal's budget uses (`lib/goal-progress.sh`).
- **Every honoured `DEFER:`** is echoed to stderr with its path
  (`goal-continue: DEFER honoured for <issue>: <path>`), the way `CONSENT:` is. The
  path is agent-writable, so the echo is how the owner reads the exact words.

The count is the session's, kept in its own file
(`$TMPDIR/claude-goal-issue-refusals-<session>`, holding the count and a transcript
offset), never the nudge counter's file. It is removed when no issue is left open.

## Fail-open

| Condition | Result |
|---|---|
| Payload is empty, malformed, or has no `cwd` or command | capture exits 0, silent |
| Not an Eshu checkout | capture exits 0, silent |
| An Eshu checkout with no live goal | capture exits 0, one `not ledgered` line per issue on stderr |
| `gh` missing, failing, or slow during capture | issue captured without the age check |
| `gh` missing, failing, or slow during the DONE check | stop allowed, `NOT_CHECKED` on stderr |
| Ledger holds garbage lines | ignored; the stop is not blocked by them |
| `python3` missing, or the lib cannot be sourced | each hook exits 0 |

The Stop hook never wedges a session. The per-turn restatement is what keeps an
unchecked issue in front of the agent afterwards.

## Known edges

- The ledger outlives `/goal <new text>` in the same session: the sidecar belongs
  to the per-session file path, so issues still open from an earlier goal remain
  owed. Close them or `DEFER` them.
- A `/goal done` typed by the owner is checked the same way as one the agent
  writes.
- A hand-written (unheaded) goal marked `DONE` at a worktree path is gated like any
  other. A newcomer session in a checkout whose shared goal is `DONE` with an open
  record is refused until the record is closed or deferred. See "The DONE check".
- Capture misses an issue created through `gh api graphql` (a `createIssue`
  mutation). Its output has no bare issue URL and no `repos/.../issues` endpoint in
  the command, so neither path fires.
- Capture is wired for Claude and Muse only. Neither Cursor (`.cursor/hooks.json`) nor Codex runs `issue-capture.sh`.

Tests: `scripts/test-issue-capture-hook.sh` (with its sourced companions
`scripts/test-issue-capture-hook-cases.sh` and
`scripts/test-issue-capture-hook-gate-cases.sh`), the issue-ledger layouts in
`scripts/test-goal-refresh-hook-parity-cases.sh`, and section K of
`scripts/test-muse-hooks.sh`. `gh` is a fake on `PATH`:
`scripts/lib/test-issue-capture-fake-gh.sh`.
