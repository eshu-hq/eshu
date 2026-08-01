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
fronted by a per-SHA stamp: `make pre-pr` writes one on success
(`scripts/dev/prepr-stamp-verify.sh`), and the push is blocked unless every
commit being pushed carries it. A green local gate is therefore a hard push-time
requirement, not something CI discovers later. The slower pre-push hooks still
run after the stamp check passes.

A rebase or amend after `make pre-pr` invalidates the stamp — re-run it before
pushing. This bites most often when a coverage or generated artifact is committed
after the gate: regenerate BEFORE the promotion run, not after.

The only sanctioned bypass is `ESHU_ALLOW_UNSTAMPED_PUSH=1`, used only when you
accept CI as the first gate for that push. CI re-checks every gate regardless and
remains the non-bypassable source of truth.

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
leaked in, stop, `git restore <file>` the uncommitted change, fetch, and re-apply
the regeneration inside a worktree if the result is still needed.

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
message or PR body unless that exact issue is meant to close on merge. Reference
issues as `#NNNN` with no keyword otherwise.

Note that git strips message lines beginning with `#NNNN` during
`rebase --continue` and some commit paths — use `Refs #NNNN` or
`--cleanup=verbatim` when the reference must survive.

## Remote test machines

Synchronize source to remote test machines through a Git fetch and a
checkout/fast-forward of the reviewed branch. Never `rsync` or copy an unreviewed
worktree and present the result as performance evidence: what ran is then not
what was reviewed, and the measurement cannot be reproduced from the branch.
