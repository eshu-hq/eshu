---
name: eshu-session-lifecycle
description: Resume or hand off Eshu work, monitor PRs, assess agent liveness, or clean up owned worktrees.
---

# Eshu Session Lifecycle

## Evidence at handoff

Preserve the prior investigation's file map, settled design decisions, and
rejected hypotheses. Verify the evidence behind claims before relying on them.
A summary saying "tests pass" is not proof.

For an inherited result, inspect the actual command, exit status, tested
commit/tree, relevant inputs, and environment. Reuse an artifact only when
these still match and its provenance is verifiable; name it as inherited proof,
not a run you performed. Re-run affected checks when the evidence is absent,
ambiguous, invalidated, or the environment matters and cannot be verified.
Refresh live PR status and ownership even when local proof remains reusable.

This does not waive final promotion: `make pre-pr` must stamp the intended
commit, and `review-attest verify` must match the reviewed inputs before push.
A rebase or amend invalidates prior stamps and review receipts. See
[local testing](../../../docs/public/reference/local-testing.md).

## Routing

| Situation | Playbook |
|---|---|
| "Take over this", "resume #NNNN", a branch handed to you, work you left days ago, the first turn after a compaction | [session-pickup.md](references/session-pickup.md) |
| "I'm going offline", "restart", context is about to compact | [pause-safely.md](references/pause-safely.md) |
| A PR is open and needs watching through review and CI to merge | [babysit-prs.md](references/babysit-prs.md) |
| `git worktree list` has grown long, disk is tight, trees look abandoned | [worktree-cleanup.md](references/worktree-cleanup.md) |
| "run until done", "going to bed", a `/loop`, or any goal handed over with no checkpoint schedule | [autonomous-run.md](references/autonomous-run.md) |

Choose the playbook for the current event. Preserve its applicable ownership,
evidence, and authorization checks; report the resulting resume point or
blocker. Do not copy an entire playbook into a todo list for a narrow task.

## Liveness is not mtime

This applies across all five playbooks, so it lives here.

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
| Accepting "tests pass" without inspecting verifiable evidence | Shipping on a self-report |
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
