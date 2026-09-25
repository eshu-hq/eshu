# Agent Git And Worktree Hygiene

Detail for the git, worktree, and push rules listed in the root `AGENTS.md`. Those files carry the trigger — the sentence that tells you a rule
applies. This file carries the reasoning and the incident behind each one, which
you only need once you know it applies.

Each rule here exists because the failure below actually happened.

## Pre-commit hooks and the pre-push floor

Install the repo's hooks once per clone: `scripts/dev/bootstrap-hooks.sh`. It is
idempotent and shared across worktrees.

Never `--no-verify` a commit or a push. Commit-stage gates are fast. Before
every push, run `make pre-push` (`scripts/dev/pre-push.sh`): the fast local
floor of changed-package `go test`, the 500-line file cap, changed-package
gofumpt/lint/build/vet, the allowlisted fast registry gates
(`local.pre_push: floor`; other triggered gates print `DEFER-CI` and still run in
`make pre-pr` and CI), and the advisory docs-contradiction gate. It has no race
lane, no live Docker/NornicDB/Postgres lane, and writes no push stamp.

There used to be a per-SHA push stamp here: `make pre-pr` wrote one on success
and a pre-push hook refused to push a commit without it. It is removed. The
measured problem was structural, not a one-off: `make pre-pr` takes roughly 20
minutes, every rebase changes the SHA and voids the stamp, and GitHub's `main`
ruleset does not require up-to-date branches — so the rebase-and-rerun loop the
stamp forced was entirely self-imposed, and PRs sat for days waiting on it.

The non-strict ruleset alone later proved unsafe: at 5–8 merges a day, two
green PRs could land a red `main` because neither was tested against the other.
That was the leading cause of CI reds in the September 2026 CI failure review
tracked in #7111. `main` now merges through a GitHub merge queue. The queue
builds each entry on top of the entries ahead of it and runs the required
contexts on that exact commit (`merge_group`), so correctness no longer
depends on authors rebasing, and strict mode stays off. Enter the queue from the PR page; do not rebase just because `main` moved.

This does **not** weaken the Ifá/Odù protection for contracts, performance, or
end-to-end behavior. That protection was never fully local to begin with: the
live Ifá/Odù cells (fault injection, determinism and dead-letter matrices,
golden-corpus, e2e) need Docker/NornicDB/Postgres and only ever ran in CI, or
locally on explicit request. The hermetic Ifá rows (load saturation,
contract-layer, materialized-edge coverage) run in `make pre-pr`; `make
pre-push` defers them, so run `make pre-pr` for changes under `go/internal/ifa`
or reducer materialization.
The blocking, non-bypassable authority for all of it is CI's
`required-gates-complete` aggregate (alongside `go-core-complete` and
`go-race-complete`), which `.github/workflows/required-gates.yml` computes from
every Ifá/Odù, contract, performance, and end-to-end gate the registry marks
`blocking: true`. A merge still requires it green; dropping the local stamp
only changes when a developer or agent *pushes*, not what *merges*.

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

## Remote test machines

Synchronize source to remote test machines through a Git fetch and a
checkout/fast-forward of the reviewed branch. Never `rsync` or copy an unreviewed
worktree and present the result as performance evidence: what ran is then not
what was reviewed, and the measurement cannot be reproduced from the branch.
