# Agent Git And Worktree Hygiene

Detail for the git, worktree, and push rules listed in the root `AGENTS.md`. Those files carry the trigger — the sentence that tells you a rule
applies. This file carries the reasoning and the incident behind each one, which
you only need once you know it applies.

Each rule here exists because the failure below actually happened.

## Pre-commit hooks and the pre-push floor

Install the repo's hooks once per clone: `scripts/dev/bootstrap-hooks.sh`. It is
idempotent and shared across worktrees.

Never `--no-verify` a commit or a push. Commit-stage gates are fast. Before
every push (except row 4a, tier S, or an owner push order; see Rebase Waiver in
`eshu-code-review`), run `make pre-push` (`scripts/dev/pre-push.sh`): the fast local
floor of changed-package `go test`, the 500-line file cap, changed-package
gofumpt/lint/build/vet, the allowlisted fast registry gates
(`local.pre_push: floor`, which includes changed-package gosec, measurement
citations and the docs build; other triggered gates print `DEFER-CI` and still
run in `make pre-pr` and CI), and the advisory docs-contradiction gate. It has no
race lane, no live Docker/NornicDB/Postgres lane, and writes no push stamp. The
git pre-push hook runs those three checks again on the push: gosec 0.3-8 s wall (40-85 s when a trigger file forces the whole module),
measurement citations 0.7 s, the docs build 0-18 s (idle host). A marker that
skipped the repeat would be a stamp, so there is none. A finding the hook would
reject (a G301, say) now fails the floor first, before the review receipt. gosec
fails closed: exit 143 (SIGTERM), a missing result, or a package it could not
analyze fails the run; rerun `make pre-push`. A package whose file patterns
exceed go/packages' 16383-byte limit (`internal/storage/postgres`, #8339) is
scanned through a short `go/_g<pid>` alias, and the run prints `scanned via alias`.

There used to be a per-SHA push stamp here: `make pre-pr` wrote one on success
and a pre-push hook refused to push a commit without it. It is removed. The
measured problem was structural, not a one-off: `make pre-pr` takes roughly 20
minutes, every rebase changes the SHA and voids the stamp, and GitHub's `main`
ruleset does not require up-to-date branches — so the rebase-and-rerun loop the
stamp forced was entirely self-imposed, and PRs sat for days waiting on it.

The non-strict ruleset alone later proved unsafe: at 5–8 merges a day, two
green PRs could land a red `main` because neither was tested against the other.
That was the leading cause of CI reds in the September 2026 CI failure review
tracked in #7111. A GitHub merge queue was tried to close that gap: it built
each entry on the entries ahead of it and ran the required contexts on that
exact commit (`merge_group`). The owner turned it off on 2026-10-16 because an
entry sat for more than six hours and PRs backed up. `main` has no merge queue
now, and no `merge_group` run tests the combined tree.

The merge path is the `main` ruleset (check it with
`gh api repos/eshu-hq/eshu/rules/branches/main`): `pull_request`,
`required_status_checks`, `non_fast_forward`, and `deletion`. The required
contexts are `go-core-complete` and `go-race-complete`, and strict mode
(`strict_required_status_checks_policy`) is off. On 2026-10-25 the owner
disabled `required-gates-complete` as a required check, for now, because its CI
failures were holding up every merge; whether the check is still needed is an
open question. It still runs and still publishes. The ruleset allows merge,
squash, and rebase merges. Strict mode is off, so a branch need not be rebased
just because `main` moved. Nothing tests two green PRs together, so the
[pre-push merge step](../public/reference/local-testing/pre-push-merge.md)
is the local guard against a red `main`.
A red `main` is reported by the Main Health watcher: one pinned `main is red @<sha>` issue and a `main-health` commit status (see [Main Health Watcher](../public/reference/local-testing/main-health-watcher.md)).

### When a PR may merge

This is the single statement of the merge rule; other files point here.

A PR merges when `go-core-complete` and `go-race-complete` are green on the
pushed head AND no gate on that head has completed red without a disposition.
The `required-gates-complete` status is not itself a merge input: it only
summarizes the registry's `blocking: true` gates
(`.github/workflows/required-gates.yml`), and its own red is often the
false-failure publisher stamp described in
[babysit-prs](../../.agents/skills/eshu-session-lifecycle/references/babysit-prs.md).
Read the gates under it. A gate that completed red (an Ifá/Odù cell, a
contract, performance, or end-to-end gate) is a finding: fix it, or link an
issue that the active goal works. A cancelled gate is not red: rerun it. A
pending check that the ruleset does not require does not block the merge; name
every still-pending check in the merge report. Main Health is the backstop: if
`main` goes red on a gate a PR merged past, the fix-forward belongs to that
PR's goal.
`scripts/dev/pre-enqueue-check.sh` checks this rule. It fails on a required
check that is not green and on a completed red not named with
`--dispositioned '<check>=<reason>'`, and it only notes a pending or absent
check that the ruleset does not require.

The Ifá/Odù protection was never fully local: the live cells (fault injection,
determinism and dead-letter matrices, golden-corpus, e2e) need
Docker/NornicDB/Postgres and run in CI, or locally on explicit request. The
hermetic Ifá rows (load saturation, contract-layer, materialized-edge coverage)
run in `make pre-pr`; `make pre-push` defers them, so run `make pre-pr` for
changes under `go/internal/ifa` or reducer materialization.

`make pre-pr` and `make pre-pr-full` still exist as deeper, optional
preflights. They are recommended, not required, before pushing a change to
queue/lease/claim code, schema DDL, hot-path Cypher or graph writes, reducer
projection/materialization, or a package move (prefer `make pre-pr-full` for
moves: build tags can hide files from `./...`, so only the whole-module race
lane actually exercises them).

## Verify `pwd` before any edit

Run `pwd` and confirm it is the intended feature worktree, not the main checkout,
before any Edit or Write. If an edit lands in the wrong path, stop editing there,
report the affected paths, and recover without discarding work you did not
author. Escalate the recovery plan to an arbiter model when it is not obvious;
never untangle it silently.

## Mutating commands belong in a worktree

Any command that mutates a tracked file — regenerators, formatters, `go mod
tidy`, `go run ./cmd/... -mode generate` — runs inside a worktree, including for
diagnostic or investigative purposes.

The main checkout must stay a clean fast-forward of `origin/main` between merges.
A dirty main checkout confuses the next agent and makes the owner's own
uncommitted work look like an agent's. If a diagnostic mutation has already
leaked in, stop and report the affected paths. Preserve the diff, choose the recovery with
an arbiter model when it is not obvious, and do not silently restore files that
may include another contributor's work. Apply the recovery in the intended
worktree.

## Never `git stash` across concurrent worktrees

The stash stack is shared across every worktree of a repo, so concurrent agents
stashing in different worktrees corrupt each other's uncommitted work. To compare
against a clean tree use `git diff`, `git show <ref>:<path>`, or a throwaway
worktree.

## Commit on a named branch, and confirm the push landed

`git symbolic-ref -q HEAD` must succeed before every commit. A detached HEAD —
from a rebase, a `checkout <sha>`, or an interrupted operation — advances no
branch ref, so committing there advances nothing and a later push silently omits
the commit. Reattach with `git switch <branch>` or `git rebase --continue` first.

After any push, confirm the branch ref advanced (pushed SHA equals local HEAD)
before opening or updating a PR.

## Issue-closing keywords

Never put `Fixes`, `Closes`, `Resolves`, `Partial-closes`, or similar in a commit
message or PR body unless that exact issue is meant to close on merge.

How to reference the issue instead depends on where the text goes:

- **PR title and body:** `#NNNN` on its own is fine and renders as a link.
- **Commit messages:** use `Refs #NNNN`. Git treats a line *beginning* with `#`
  as a comment and silently strips it during `rebase --continue` and other
  commit paths, so a bare `#NNNN` at the start of a line disappears without
  warning. `--cleanup=verbatim` also preserves it, but `Refs #NNNN` needs no
  flag and survives every path.

## Shallow clones hide history

Run `git rev-parse --is-shallow-repository` before you trust history. In a
shallow clone, `git log -S<text> -- <path>` returns the single squashed commit
that holds the text, so it looks like the change's origin when it is not.
`git show <sha>~1:<path>` fails for the same reason.

To read an older version of a file without fetching history, use the GitHub API:

```bash
gh api -H 'Accept: application/vnd.github.raw' \
  'repos/eshu-hq/eshu/contents/<path>?ref=<sha>'
```

Do not run `git fetch --unshallow` on a low-disk host without a reason.

## Remote test machines

Synchronize source to remote test machines through a Git fetch and a
checkout/fast-forward of the reviewed branch. Never `rsync` or copy an unreviewed
worktree and present the result as performance evidence: what ran is then not
what was reviewed, and the measurement cannot be reproduced from the branch.
