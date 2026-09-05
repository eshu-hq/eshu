# Agent Git And Worktree Hygiene

Detail for the git, worktree, and push rules listed in the root `CLAUDE.md` /
`AGENTS.md`. Those files carry the trigger — the sentence that tells you a rule
applies. This file carries the reasoning and the incident behind each one, which
you only need once you know it applies.

Each rule here exists because the failure below actually happened.

## Pre-commit hooks and the pre-pr stamp

Install the repo's hooks once per clone: `scripts/dev/bootstrap-hooks.sh`. It is
idempotent and shared across worktrees.

Never `--no-verify` a commit or a push. Commit-stage gates are fast. The push is
fronted by a per-SHA stamp: `make pre-pr` writes one on success, and
`scripts/dev/prepr-stamp-verify.sh` blocks the push unless the stamp exists.

Be precise about what it checks, because the looser reading gives false comfort:
it validates the **tip SHA of each non-delete ref being pushed**, one per ref —
not every commit in the range. Intermediate commits on a branch are never
stamped and are not expected to be. A green local gate on the tip is therefore a
hard push-time requirement; it is not a per-commit guarantee. The slower
pre-push hooks still run after the stamp check passes.

A rebase or amend after `make pre-pr` invalidates the stamp — re-run it before
pushing. This bites most often when a coverage or generated artifact is committed
after the gate: regenerate BEFORE the promotion run, not after.

The transport exposes `ESHU_ALLOW_UNSTAMPED_PUSH=1` for an explicit owner
exception. Its availability is not authorization: agents must not select it to
skip local proof or promotion. Report the blocked gate and obtain a specific
owner decision before any exception; normal PR requests retain the root proof
requirements. CI still re-checks the required gates.

## Verify `pwd` before any edit

Run `pwd` and confirm it is the intended feature worktree, not the main checkout,
before any Edit or Write. If an edit lands in the wrong path, stop immediately,
report it, and let the owner decide how to recover rather than trying to
untangle it silently.

## Mutating commands belong in a worktree

Any command that mutates a tracked file — regenerators, formatters, `go mod
tidy`, `go run ./cmd/... -mode generate` — runs inside a worktree, including for
diagnostic or investigative purposes.

The main checkout must stay a clean fast-forward of `origin/main` between merges.
A dirty main checkout confuses the next agent and makes the owner's own
uncommitted work look like an agent's. If a diagnostic mutation has already
leaked in, stop and report the affected paths. Preserve the diff and let the
owner choose recovery; do not silently restore files that may include another
contributor's work. Apply the agreed recovery in the intended worktree.

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
