# Session pickup

You own the resume point. Read the trail; re-prove the claims.

Triggers: "take over this", "resume #NNNN", "continue where X left off", a
branch someone pushed for you to finish, work you left days ago, and the first
turn after context compaction.

## Steps

1. **Locate yourself before reading anything.** Run `pwd`, `git worktree list`,
   `git branch --show-current`, `git rev-parse --short HEAD`. Confirm you are in
   the feature worktree and not the main checkout. If the work has no worktree
   yet, create one under `.worktrees/<slug>` off fresh `origin/main` before any
   edit.

2. **Establish that the tree is yours to touch.** A branch you did not create is
   another agent's active surface. The GitHub account is shared, so
   `--author` proves nothing about who is driving a PR. Ownership evidence is a
   local worktree and branch you can point at. If the only evidence is a remote
   branch, treat the tree as live and ask before editing it.

3. **Rebase before you read — actually rebase, not just look.** Fetch, see what
   moved, then move the branch. Steps 4 and 6 both assume this happened; if you
   only inspect, you read and implement against stale history while believing
   you did not.

   ```bash
   git fetch origin main
   git log --oneline HEAD..origin/main            # what landed while you were away
   git rebase origin/main                          # the step that is easy to skip
   git diff --stat "$(git merge-base origin/main HEAD)"...HEAD
   ```

   Reading the diff before rebasing means reading conflicts that no longer
   exist, or missing ones that now do. If the rebase conflicts, resolve it now
   — a conflict discovered here is cheap, and the same conflict discovered
   after an hour of work is not.

   A clean rebase on a shared file deserves one look rather than relief: a
   line-merge of a generated artifact or a counter can be conflict-free and
   still semantically wrong. Regenerate anything generated and confirm your own
   change survived.

4. **Read the trail. Do not re-derive it.** In order of value: any resume note
   the prior session left, the PR body and its review threads, commit messages
   on the branch, then the diff itself. The prior agent already paid for reading
   the code and making the design calls. Inherit that.

   The tell that you are doing this wrong is a "let me just verify everything
   from scratch" pass. That is not diligence, it is spending a session to
   reproduce a conclusion you were handed.

5. **Sort what you found into inherited versus unproven.** Apply the split rule
   from `SKILL.md`. Design decisions, file locations, and rejected approaches
   are inherited. Every claim of the form "tests pass", "the gate is green",
   "this is verified", or "proven locally" is unproven until you run it on the
   current HEAD and paste the output.

   This is the step the #5441 failure skipped: an explore agent's citations were
   handed on as verified, and a dead feature shipped behind them.

6. **Assume the push stamp is gone.** `make pre-pr` writes a per-SHA stamp and
   `scripts/dev/prepr-stamp-verify.sh` blocks the push without it. Any rebase or
   amend — including the one in step 3 — invalidates it. Do not plan around a
   stamp you inherited.

7. **Verify each acceptance criterion against HEAD before implementing
   anything.** On aged work most criteria are already satisfied by changes that
   landed since. Sweep them one at a time and mark what is genuinely left.

8. **Name the resume point and route.** State the single next action, then hand
   off to the skill that owns the surface it touches. This playbook ends here.

## Reply

Your report must contain:

- worktree path, branch, HEAD SHA
- rebase result and whether the tree is clean
- what you inherited, and what you re-proved, with the command output
- acceptance criteria already satisfied versus still open
- the resume point: one sentence, one next action

## Traps specific to this repo

| Trap | Why it bites |
|---|---|
| Diffing against local `origin/main` without fetching | The main checkout is routinely many commits behind; the diff is fiction |
| Trusting an inherited green gate | A rebase invalidated the stamp, and the run predates the final edit |
| Editing the main checkout because the worktree is "just for the last task" | Main must stay a clean fast-forward of `origin/main` |
| Judging a sibling worktree abandoned by file mtime | A thinking agent writes nothing; see Liveness in `SKILL.md` |
| Re-invoking no skills after a compaction | Applicable skills must be re-invoked; the summary does not carry them |
