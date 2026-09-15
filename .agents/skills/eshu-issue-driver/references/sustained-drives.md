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
`CLAUDE.md`, this skill, and the reference docs it points at; copying it into
the goal text is exactly the repeated-prompt problem this contract exists to
end, and a paraphrase of a rule drifts from the rule the moment either one
changes.

## Per-harness rendering

**Claude Code** — two commands, the goal and the grant kept separate:

```
/goal endpoint: issues <list> closed per eshu-issue-driver Completion Evidence.
lane: <what this drive owns, or "per the claim comments on #N">.
decision pointers: <comment ids or a design doc>.
machine constraints: <e.g. worktree X only, checked before verify-golden-corpus-gate>.
/goal consent push, pr-open
```

**Codex** — the same five-slot text, plus an inline stop line, because Codex
has no `goal-continue.sh` Stop hook to enforce it:

```
endpoint: issues <list> closed per eshu-issue-driver Completion Evidence.
lane: <what this drive owns, or "per the claim comments on #N">.
decision pointers: <comment ids or a design doc>.
machine constraints: <...>.
Stop only when Completion Evidence is met, an owner decision is needed (post
it on the issue and stop), or nothing local clears a blocker. Consent: push,
pr-open.
```

**Muse** — "Goal set — " plus the Codex text above, verbatim.

Ask only for an act not already authorized; never write a consent grant on
the owner's behalf. Grant only the acts you mean. **Add `merge`** if you want
the drive to land the PR unattended — left out of the template deliberately,
because a merge is the least reversible act in the canon's list and the one
nobody reviews afterwards, and a copy-pasted default is not the place to
grant it.

## The consent bug this contract fixes

The earlier template put `CONSENT: push, pr-open` on the **last line** of the
goal text passed to `/goal <text>`. Both hooks parse `CONSENT:` only as part
of a **leading metadata block** — confirmed by reading `goal-continue.sh`
around the metadata-strip loop (`.claude/hooks/goal-continue.sh:225-257`) and
`goal-refresh.sh` around its mirror (`.claude/hooks/goal-refresh.sh:314-326`,
`:364-383`): both stop treating lines as metadata at the first ordinary line
and read everything after that as the objective, verbatim. A `CONSENT:` line
placed after the objective text is therefore body text, not a grant — it is
never parsed, never lifts "you need consent" as a stop reason, and never
appears in the per-turn restatement.

Fable reproduced this live: submitting a goal with a trailing `CONSENT:` line
left the grant unread by both hooks, while a separate `/goal consent push,
pr-open` command — which `goal-refresh.sh` matches as its own case above the
generic producer and writes as a leading `CONSENT:` line — was honoured
immediately and echoed back on the next Stop and the next refresh. The fix is
structural, not cosmetic: always grant consent with its own `/goal consent
<acts>` command (or the Codex/Muse inline "Consent: ..." line, which is
prose read by the model rather than parsed by a hook), never as a trailing
line inside the goal text.

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
