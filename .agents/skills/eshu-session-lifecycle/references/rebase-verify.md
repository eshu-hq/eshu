# Rebase And Verify

Prove that a rebase kept the work, and find out whether a red gate belongs to
the branch or to the new base. A rebase that is not proved is a new diff.

Triggers: `main` moved under a branch you have out; a verdict went stale
because the base changed; a handoff asks you to rebase and re-verify.

You rebase only a branch and worktree you created or were assigned. A worktree
that another live agent holds is not yours; see
[liveness](../SKILL.md#liveness-is-not-mtime).

## Steps

1. **Pin the base.** Confirm `pwd` is the branch's worktree, then record:

   ```bash
   git fetch origin
   OLD_HEAD=$(git rev-parse HEAD)
   OLD_BASE=$(git merge-base HEAD origin/main)
   NEW_BASE=$(git rev-parse origin/main)
   ```

   Work against `$NEW_BASE` until the run ends. If `main` moves again, report the
   new tip and do not chase it.

2. **Rebase onto the pin.** Run `git rebase "$NEW_BASE"`, not `git pull`. If it
   conflicts, stop and report each conflicting file. Do not resolve a semantic
   conflict by guessing. Never `git stash`.

3. **Prove the content did not change.** Run the content tests that
   [eshu-code-review](../../eshu-code-review/SKILL.md#rebase-waiver) defines:
   `git range-diff "$OLD_BASE..$OLD_HEAD" "$NEW_BASE..HEAD"` with every commit
   `=`, the cumulative patch-id, and the reversion scan. Use `$OLD_BASE`,
   `$OLD_HEAD`, and `$NEW_BASE` as its old and new bases.

4. **Choose the proof to rerun.** Follow the decision table in that same
   section: a waiver note (row 6 only), the scoped re-review with its
   [scoped rebase verdict](../../eshu-code-review/references/verdict.md#scoped-rebase-verdict)
   (rows 3 and 4), or the full review. Do not restate it here.

   A subagent runs focused proof and the registry-selected static gates its role
   names. Only the coordinator runs `make pre-push` or `make pre-pr`.

5. **Triage each red gate before you fix anything.** The gate selectors choose
   gates from a diff, so they select nothing on a bare base. Do not use them
   there. Run the gate's own command instead:

   ```bash
   BASE_TREE=$(mktemp -d)
   git worktree add --detach "$BASE_TREE" "$NEW_BASE"
   # read the gate's local.command in specs/ci-gates.v1.yaml; run it from the
   # repo root of "$BASE_TREE", then from the branch worktree
   git worktree remove --force "$BASE_TREE"
   ```

   Remove `$BASE_TREE` even when the gate fails. If the gate runs `golangci-lint`,
   build the custom plugins in `$BASE_TREE` first; see
   [the custom lint plugins](../../../../docs/internal/agent-verification-details.md#the-custom-lint-plugins).

   Do not run a gate twice when the registry marks it `ci-heavy` or CI-only, or
   when it needs Docker or fixed host ports. Read the failing step in the CI log
   and compare it with the same step in the base run.

   Compare the failing step and its message on both sides:
   - The same step and message on the base: `INHERITED`. Record the gate, its
     exit code, and `$NEW_BASE`. Do not fix it in this branch unless it fits the
     authorized scope; see [monitoring.md](../../eshu-issue-driver/references/monitoring.md).
   - Red only on the branch: `BRANCH`. Fix the branch.
   - Red on both with different messages: report both and class neither.
   - Killed by host load or a full disk: `ENV`. Check `uptime` and `df`, then
     rerun when the host is quiet. Never kill another session's gate.

6. **Hand off.** A subagent writes the handoff and stops; it does not push. The
   coordinator pushes with `--force-with-lease` after
   `ci-gates review-attest verify` matches.

## Reply

Your report must contain:

- `$OLD_BASE`, `$NEW_BASE`, `$OLD_HEAD`, and the new `HEAD`
- the range-diff summary, both patch-ids, and the reversion scan result
- each gate's command, exit code, and class: `PASS`, `BRANCH`, `INHERITED`, or `ENV`
- the table row you took, the proof you reran because of it, and why
- for row 3 or 4, the scoped rebase verdict file: its finding counts for the
  reviewed scope, with `PREPUSH_EXIT` and the head SHA it ran on
- anything `NOT_CHECKED`, with the reason

Include the head line described in
[Messages name their head](../SKILL.md#messages-name-their-head).

## Mistakes

| Mistake | What it costs |
|---|---|
| Blaming the branch for a gate that is red on the bare base | A fix inside the PR for a defect `main` already has |
| Using a gate selector on the bare base | An empty selection that reads as green |
| Skipping the range-diff because the rebase "had no conflicts" | A reordered or split patch carried under an old verdict |
| Chasing `main` through a second move mid-run | Proof against a base that no longer exists, and no result |
| Reusing a receipt after a rebase | `review-attest verify` fails and names the changed binding |
