---
name: eshu-session-lifecycle
description: >
  Use when the session itself starts, stops, or hands off, rather than when
  code changes. Taking over another agent's branch, transcript, or pushed
  work; resuming work that sat for hours or days; pausing before going
  offline or before context compaction summarizes the plan away; watching
  open PRs for review comments, CI results, and merge conflicts; judging
  whether a subagent is alive or dead; pruning stale git worktrees. Also use
  when an agent reports work finished but the evidence trail is unclear.
---

# Eshu Session Lifecycle

## Why this skill exists

Every other skill in `.agents/skills/` is keyed to a **surface**: Go, Cypher,
Postgres, contracts, telemetry, the golden corpus. This one is keyed to the
**session**.

The gate floor in [Agent Orchestration Model](../../../docs/internal/agent-orchestration.md)
is strong, but every gate in it fires on a diff. Nothing fires on a session. So
no CI check will ever catch:

- a subagent that died twenty minutes ago while you waited on it,
- a plan that context compaction summarized away,
- a worktree removed with uncommitted work still in it,
- a PR merged over a review thread nobody read.

That is not a gap in CI. Those failures are outside what a diff gate can see,
which is exactly the class the canon says to write down instead:

> Inline only role boundaries or actions whose ambiguity can mutate external
> state before CI runs. Everything CI enforces, let CI enforce.

## The rule behind all four playbooks

**Inherit the reading. Re-prove the claims.**

When you pick up work — from another agent, from your own pre-compaction self,
from a branch that sat for a week — two different things are in front of you,
and they get opposite treatment.

| What you found | Treatment |
|---|---|
| Which files matter, what the design is, what was ruled out and why | **Inherit it.** Re-deriving burns context and loses the reasoning. |
| Any claim that something passes, is verified, or is proven | **Re-prove it.** Run it yourself, cite the run. |

Both halves are load-bearing. Skip the first and you spend a session
rediscovering what was already known. Skip the second and you ship on a
self-report, which is how a dead feature reached main behind an explore agent's
citations.

A prior agent's summary is a map, never a measurement.

## Routing

| Situation | Playbook |
|---|---|
| "Take over this", "resume #NNNN", a branch handed to you, work you left days ago, the first turn after a compaction | [session-pickup.md](references/session-pickup.md) |
| "I'm going offline", "restart", context is about to compact | [pause-safely.md](references/pause-safely.md) |
| A PR is open and needs watching through review and CI to merge | [babysit-prs.md](references/babysit-prs.md) |
| `git worktree list` has grown long, disk is tight, trees look abandoned | [worktree-cleanup.md](references/worktree-cleanup.md) |
| "run until done", "going to bed", a `/loop`, or any goal handed over with no checkpoint schedule | [autonomous-run.md](references/autonomous-run.md) |

Each playbook ends with a **Reply** contract listing what your report must
contain. Copy its steps into your todo list verbatim. A step you skip stays on
the list with a one-line reason, because a silently dropped step is
indistinguishable from a step that was never there.

## Liveness is not mtime

This applies across all four playbooks, so it lives here.

A subagent that is thinking writes no files. Its transcript buffer stays small
until it completes. Judging "this agent is dead" or "this worktree is
abandoned" from a file timestamp will be wrong in the one direction that costs
you: it reads a live worker as dead, and then you collide with it.

Judge liveness from something that moves for a reason:

- a process whose working directory is that tree,
- an open PR or an unmerged branch with commits ahead of `origin/main`,
- a task the harness still reports as running,
- the user telling you.

When those disagree, the answer is "assume alive" and ask.

## Common mistakes

| Mistake | What it costs |
|---|---|
| Re-running the prior agent's whole investigation | A session of context, and the design reasoning that was already paid for |
| Accepting "tests pass" from a handoff without rerunning | Shipping on a self-report |
| Pausing by pushing or opening a PR "so it's saved" | An irreversible action taken for a reversible reason |
| `git stash` to park work | The stash stack is shared across all worktrees; concurrent agents corrupt each other |
| Editing in the main checkout because the worktree "was just for the last task" | Main must stay a clean fast-forward of `origin/main` |
| Reading one `pending == 0` and calling CI green | Large check sets register in waves |
| Deleting a worktree that looked idle | Uncommitted work is unrecoverable; the tree may be in active use |

## Related skills

- `eshu-issue-driver` owns driving an issue to merged. This skill owns the
  session mechanics that driver depends on.
- `eshu-code-review` owns the pre-push verdict. Pickup and babysit hand off to
  it, they do not replace it.
- `eshu-humanizer` is the last pass over any status update, resume note, or PR
  reply these playbooks produce.
