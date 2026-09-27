# Pre-Enqueue Check

`scripts/dev/pre-enqueue-check.sh` is a read-only check that fails closed.
The coordinator runs it immediately before asking the arbiter to approve a
merge-queue enqueue. It re-derives the facts an arbiter used to collect by hand,
so the arbiter reviews one report instead of re-running each query.

```bash
GH_TOKEN="$(gh auth token --user <account>)" \
  scripts/dev/pre-enqueue-check.sh <pr-number> <expected-head-sha>
```

`<expected-head-sha>` is the full 40-character SHA that passed review. The
script uses the ambient `gh` authentication. On a machine where the active
`gh` account cannot read the repository, prefix `GH_TOKEN` as shown above.
`PRE_ENQUEUE_REPO=owner/name` overrides the repository slug from `gh repo view`, and
`PRE_ENQUEUE_REMOTE` overrides the git remote (default `origin`).

## Arms

Each arm prints one `PASS` or `FAIL` line. Any `FAIL` exits `1`, and a usage
error exits `2`.

| Arm | Passes when |
| --- | --- |
| `pr-state` | The PR is `OPEN`, not a draft, and `headRefOid` equals the expected SHA. |
| `merge-main` | `git merge-tree --write-tree origin/main <head>` is clean after fetching `main` and the head. |
| `merge-queue` | The head merges cleanly with the merge-queue tip: the `headCommit` of the last queued entry that is not this PR. A merge-group commit contains every PR ahead of it, so a queue collision that a check against `origin/main` alone misses still fails here. The arm prints the file overlap with each queued PR. An empty queue passes with `queue empty`. |
| `checks` | `gh pr checks` reports no pending, failing, or cancelled rows. Rows are counted by gh's state bucket, never by line text; skipped rows are allowed. The head's `required-gates-complete` commit status must be `success`, and `mergeStateStatus` must be `CLEAN`. |
| `threads` | GraphQL `reviewThreads` has zero unresolved threads. A truncated page fails closed. |
| `body` | The body contains at least one `Closes`/`Fixes`/`Resolves #N`, and none of the AI-attribution text `Generated with` or `Co-Authored-By`. The keywords are listed so the caller can confirm each issue is meant to close on merge. |

The script performs one read. It does not replace the two-consecutive-stable-reads
rule for CI completion. Run it only after the caller's watcher has seen the full
check set stable twice.

The script writes nothing to GitHub and never touches a branch, the index, or
the worktree. `git fetch origin main` advances the remote-tracking ref
`origin/main`. The head and queue-tip commits are fetched by SHA, which stores
objects only, and `git merge-tree --write-tree` writes loose objects only.

## Self-test

```bash
bash scripts/dev/test-pre-enqueue-check.sh
```

The self-test is hermetic. A fake `gh` on `PATH`
(`scripts/lib/test-pre-enqueue-check-fake-gh.sh`) serves JSON for each case. A
local bare repository stands in for `origin`, so the merge-tree arms run real
merges over real conflicts. One all-green case must exit `0`. Each arm has a
seeded RED that must exit `1` with a single `FAIL` line naming that arm. The
registry row is `pre-enqueue-check` in `specs/ci-gates.v1.yaml`, and CI runs the
self-test in the `Verify ci-gate registry test mirror` job.
