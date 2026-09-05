# Session pickup

You own the resume point. Read the trail and verify the evidence.

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

3. **Refresh the base before continuing work.** Fetch and compare the intended
   base with the current branch. Inspect the handoff and working tree before
   rebasing; a read-only status request or unchanged base needs no rebase.
   For owned implementation work whose base moved, rebase before further edits
   and resolve conflicts. Preserve uncommitted work; never stash across shared
   worktrees. Inspect the resulting diff for unrelated reversions and generated
   drift even when the merge was conflict-free.

   ```bash
   git fetch origin main
   git log --oneline HEAD..origin/main
   git diff --stat "$(git merge-base origin/main HEAD)"...HEAD
   # For owned implementation work with a moved base and clean working tree:
   git rebase origin/main
   ```

4. **Read the trail. Do not re-derive it.** In order of value: any resume note
   the prior session left, the PR body and its review threads, commit messages
   on the branch, then the diff itself. The prior agent already paid for reading
   the code and making the design calls. Inherit that.

   The tell that you are doing this wrong is a "let me just verify everything
   from scratch" pass. That is not diligence, it is spending a session to
   reproduce a conclusion you were handed.

5. **Distinguish summaries from verifiable artifacts.** Follow the evidence
   rules in `../SKILL.md`. Inherit settled reasoning; inspect actual test logs,
   commands, exit status, commit/tree, inputs, and environment before reusing
   proof. Re-run affected checks when those no longer match or the evidence is
   only a claim. Attribute inherited proof honestly.

6. **Verify promotion state.** Any rebase or amend invalidates the per-SHA push
   stamp and review receipt. Before the next push, use the current
   `eshu-code-review` promotion sequence and `make pre-pr`; an inherited summary
   cannot replace either gate. If HEAD did not change, inspect the actual stamp
   and receipt rather than assuming they are absent or valid.

7. **Verify each acceptance criterion against HEAD before implementing
   anything.** On aged work most criteria are already satisfied by changes that
   landed since. Sweep them one at a time and mark what is genuinely left.

8. **Name the resume point and route.** State the single next action, then hand
   off to the skill that owns the surface it touches. This playbook ends here.

## Reply

Your report must contain:

- worktree path, branch, HEAD SHA
- base comparison, any rebase result, and whether the tree is clean
- inherited artifacts verified and new checks run, with evidence locations
- acceptance criteria already satisfied versus still open
- the resume point: one sentence, one next action

## Traps specific to this repo

| Trap | Why it bites |
|---|---|
| Diffing against local `origin/main` without fetching | The main checkout is routinely many commits behind; the diff is fiction |
| Trusting an inherited green gate | A rebase invalidated the stamp, and the run predates the final edit |
| Editing the main checkout because the worktree is "just for the last task" | Main must stay a clean fast-forward of `origin/main` |
| Judging a sibling worktree abandoned by file mtime | A thinking agent writes nothing; see Liveness in `SKILL.md` |
| Re-invoking no skills after a compaction | Reload applicable instructions missing from context; a summary is not the skill |
