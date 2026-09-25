# Sustained Drives And Waiters

## The goal contract

A `/goal` prompt for a long drive is five slots, nothing else:

- **endpoint** — the issue plus "closed per eshu-issue-driver Completion
  Evidence".
- **lane** — what this drive owns, or "per the claim comments on #N".
- **decision pointers** — comment ids or a design doc.
- **machine constraints** — anything about this host or checkout the agent
  cannot discover on its own (a pinned worktree, a port already in use, a
  live gate another session holds).
- **consent** — the acts the owner is granting up front, or none.

A goal must NOT restate proof lists, gate names, tool names, model names,
polling cadences, or restack/trap lore. All of that already lives in
`AGENTS.md`, this skill, and the reference docs it points at; copying it into
the goal text is exactly the repeated-prompt problem this contract exists to
end, and a paraphrase of a rule drifts from the rule the moment either one
changes.

## Per-harness rendering

**Claude Code** — one `/goal consent <acts> -- <goal text>` prompt carries the
objective and the grant together, in a single chat turn:

```
/goal consent push, pr-open -- endpoint: issues <list> closed per eshu-issue-driver Completion Evidence.
lane: <what this drive owns, or "per the claim comments on #N">.
decision pointers: <comment ids or a design doc>.
machine constraints: <e.g. worktree X only, checked before verify-golden-corpus-gate>.
```

`goal-refresh.sh` splits on the FIRST ` -- ` only: the acts are everything
before it, the goal text everything after, so a goal that itself discusses
` -- ` or the word "consent" later on stays objective text. It writes a
`SESSION:` header, a leading `CONSENT:` line, then the goal text, and both
hooks honour the grant from the very first Stop -- there is no window between
setting the goal and consenting to it, and no second chat turn to race.

When a launcher, not a chat turn, starts the session, set consent before the
first prompt instead:

```
CLAUDE_GOAL_CONSENT="push, pr-open" <launch command>
```
```
/goal endpoint: issues <list> closed per eshu-issue-driver Completion Evidence.
lane: <what this drive owns, or "per the claim comments on #N">.
decision pointers: <comment ids or a design doc>.
machine constraints: <e.g. worktree X only, checked before verify-golden-corpus-gate>.
```

`CLAUDE_GOAL_CONSENT` is an environment variable, read directly by both hooks
(`goal-continue.sh` and, via `CONSENT_ENV`, `goal-refresh.sh`) independently of
anything written to the goal file, and is the right tool when the first prompt
itself is not under the owner's control (a scripted launch with a fixed
opening message). `/goal consent push, pr-open` -- without ` -- <text>` --
remains the right tool for granting consent *mid-drive*, extending or
correcting a grant on a goal that is already running; that command edits the
CONSENT line of an existing goal file rather than starting a new one, and
`goal-continue.sh`'s Stop hook is what keeps the drive working in between. See
[the consent bug this contract fixes](#the-consent-bug-this-contract-fixes)
below for the failure mode both forms avoid, and
`scripts/test-goal-refresh-hook-oneline-consent-cases.sh` and
`scripts/test-goal-refresh-hook-atomic-consent-cases.sh` for the end-to-end
proof against both hooks.

**Codex** — the same five-slot text, plus an inline stop line, because Codex
has no `goal-continue.sh` Stop hook to enforce it:

```
endpoint: issues <list> closed per eshu-issue-driver Completion Evidence.
lane: <what this drive owns, or "per the claim comments on #N">.
decision pointers: <comment ids or a design doc>.
machine constraints: <...>.
Stop only when Completion Evidence is met or you are waiting on outside work
behind a live watcher. Escalate an open decision or blocker to an arbiter
model, act on its verdict, and post it on the issue. Consent: push, pr-open.
```

**Muse** — "Goal set — " plus the Codex text above, verbatim.

An act not already granted gets an arbiter model's review, not a question;
never write a consent grant on the owner's behalf. Grant only the acts you mean. **Add `merge`** if you want
the drive to land the PR unattended — left out of the template deliberately,
because a merge is the least reversible act in the canon's list and the one
nobody reviews afterwards, and a copy-pasted default is not the place to
grant it.

## The consent bug this contract fixes

The earlier template put `CONSENT: push, pr-open` on the **last line** of the
goal text passed to `/goal <text>`. Both hooks parse `CONSENT:` only as part
of a **leading metadata block** — confirmed by reading `goal-continue.sh`
around the metadata-strip loop (`.claude/hooks/goal-continue.sh:225-257`) and
`goal-refresh.sh` around its mirror (`.claude/hooks/goal-refresh.sh:399-418`):
both stop treating lines as metadata at the first ordinary line
and read everything after that as the objective, verbatim. A `CONSENT:` line
placed after the objective text is therefore body text, not a grant — it is
never parsed and never appears in the per-turn restatement.

Fable reproduced this live: submitting a goal with a trailing `CONSENT:` line
left the grant unread by both hooks, while a separate `/goal consent push,
pr-open` command — which `goal-refresh.sh` matches as its own case above the
generic producer and writes as a leading `CONSENT:` line — was honoured
immediately and echoed back on the next Stop and the next refresh. The fix is
structural, not cosmetic: always grant consent with its own `/goal consent
<acts>` command (or the Codex/Muse inline "Consent: ..." line, which is
prose read by the model rather than parsed by a hook), never as a trailing
line inside the goal text.

## The initial-grant race this contract also fixes

codex#4018964359 found the next layer of the same problem: even the correct
two-command form — `/goal <text>` then `/goal consent push, pr-open` — is two
separate chat turns, and `goal-continue.sh`'s Stop hook exists specifically to
keep a drive working once the first one lands. An unattended launch has no
guaranteed window to send the second command before the drive reaches its
first push, so the grant can arrive after the act it was meant to cover.

Two fixes close this, for the two situations the initial grant can start from.
From a **chat turn**, `/goal consent <acts> -- <goal text>` is atomic on its
own: it is one command, so there is no second one to race, and both hooks read
the `CONSENT:` line it writes from the very first Stop.
`scripts/test-goal-refresh-hook-oneline-consent-cases.sh` proves this end to
end, including the negative: `/goal consented users -- need a path` does not
match the consent arm at all (its patterns match only `consent` followed by
end-of-string or a space, so `consented` is an ordinary goal), and a later
` -- ` inside the goal text is not read as a second delimiter.

From a **launcher** that does not control the first chat turn's exact text,
`CLAUDE_GOAL_CONSENT` is the answer, and was already there before this
contract was documented: it is an environment variable, read directly by
`goal-continue.sh` (`.claude/hooks/goal-continue.sh:262-264`) and by
`goal-refresh.sh` via `CONSENT_ENV` into `lib/goal-refresh-note.py:37-39`,
independently of anything written to the goal file. Set by the launcher before
the session's first prompt, it is already in effect for the very first Stop
that first `/goal` produces — there is no second command, so there is nothing
to race. `scripts/test-goal-refresh-hook-atomic-consent-cases.sh` proves this
against both hooks end to end, and proves the negative alongside it: a goal
body that merely mentions "consent" in prose (no `CONSENT:` line, no env var)
grants nothing, exactly as leading-block parsing already requires.

`/goal consent <acts>` without ` -- <text>` remains correct for a grant made
*after* the drive is already running — extending it mid-flight, or covering an
act the launcher did not anticipate. It edits the `CONSENT:` line of a goal
file that already exists, rather than starting a new one.

## Polling and liveness

Use `/goal` only when the user requests a goal; invoking this skill alone
does not create one. The `/goal` evaluator reads the conversation, including
this skill, so "done per the skill" is checkable. Use the available
goal/wait facilities within active harness limits. While a PR is open, poll
conflicts, CI, and reviews about every 60 seconds; do not only wait for the
check rollup.

Poll with a **bounded background waiter that blocks until a condition holds**
(`until <check>; do sleep 40; done`, with an iteration cap), run as a
background command — a foreground sleep is refused by the Claude Code
harness — not by spending a turn per poll. One waiter per condition:
duplicates racing on the same condition waste turns, and a waiter whose match
pattern cannot occur — watching a log for a string that run never prints —
spins to its cap while reporting nothing. Stop your own superseded waiters
when the thing they watch is replaced. The cadence is a ceiling on staleness,
not a requirement to burn a turn each minute.

Confirm a detached launch actually took, by its process, not by the shell's
exit status: `setsid` does not exist on macOS, so `setsid nohup … &`
backgrounds an instant failure and returns 0. The log you then read is a
stale file from an earlier run, which reads exactly like progress. Check the
log's mtime and its size delta before believing its tail — mtime alone can
advance on a process stuck retrying, so size delta is the better liveness
probe.
