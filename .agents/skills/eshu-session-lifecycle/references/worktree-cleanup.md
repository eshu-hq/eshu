# Worktree cleanup

You own the disk and the safety gate. This is the one playbook that destroys
work with no code review behind it, so the gates below are the review.

Triggers: disk pressure, `git worktree list` grown long, trees that look
abandoned, or the user asking to clean up.

## Steps

1. **Snapshot before touching anything.**

   ```bash
   df -h /
   git worktree list | wc -l
   ```

   Record both. You will cite them again at the end.

2. **Classify every tree from the list, never from a hand-typed path.** For each
   path in `git worktree list`, collect: uncommitted tracked changes, untracked
   files, whether the branch is merged into `origin/main`, commits ahead, and
   whether a PR exists.

   ```bash
   git fetch origin main
   git worktree list --porcelain | awk '/^worktree /{print $2}' | while read -r p; do
     dirty=$(git -C "$p" status --porcelain | wc -l | tr -d ' ')
     br=$(git -C "$p" branch --show-current)
     ahead=$(git -C "$p" rev-list --count origin/main.."$br" 2>/dev/null || echo '?')
     printf '%s\tbranch=%s\tdirty=%s\tahead=%s\n' "$p" "$br" "$dirty" "$ahead"
   done
   ```

3. **Establish liveness before proposing anything for deletion.** See Liveness
   in `SKILL.md`: mtime is not evidence. A tree with an open PR, commits ahead
   of `origin/main`, or a process running inside it is in use, whatever its
   timestamps say.

   ```bash
   gh pr list --state open --json headRefName,number,title
   lsof -a -d cwd -- "$path" 2>/dev/null
   ```

4. **Stop on any uncommitted work.** `dirty > 0` means tracked edits nobody
   committed. Removing a clean worktree is recoverable from its branch;
   uncommitted work is gone for good. Show the diff, name the files, and get a
   decision. Do not decide this one yourself, and do not "helpfully" commit
   someone else's half-finished edits to clear the flag.

   **Untracked files need the same decision.** It is tempting to treat them as
   scratch, and the count often is. But an untracked file can be a new source
   file nobody has staged yet, and it is the only copy — no branch holds it, no
   stash, no reflog. Once the worktree goes, so does it. List them by name and
   get the same yes you would get for tracked changes; do not decide on the
   author's behalf that their unstaged work was disposable.

5. **Remove only the confirmed set, gently first.**

   ```bash
   git worktree remove "$path"          # no --force on the first attempt
   git worktree prune
   ```

   `--force` is for a tree whose only remaining content is build artifacts. If
   the directory survives removal because of ignored build output, `rm -rf` it
   and prune again. Branch refs survive worktree removal, so no commits are
   lost by this step.

6. **Sweep the processes the trees left behind.** Load generators, poll loops,
   and compose stacks outlive the worktree that spawned them. Bring down compose
   projects by their own project name, and confirm nothing is still running
   against a path you just deleted.

7. **Confirm and report.**

   ```bash
   df -h /
   git worktree list | wc -l
   ```

## Reply

Your report must contain:

- `df -h /` and tree count, before and after, with space reclaimed
- every worktree removed
- every worktree held back, each with a one-line reason: in use by which PR, or
  N uncommitted files
- background processes stopped

## Red flags: stop and ask

- The tree has tracked uncommitted changes.
- The branch has commits that are not on `origin/main`.
- An open PR points at the branch.
- A process is running with its working directory inside the tree.
- You are reaching for `--force` on the first attempt.
- You inferred "abandoned" from a timestamp.
- The path came from your memory of the layout rather than from
  `git worktree list`.

Any one of these means the tree stays until a human says otherwise. A worktree
left standing costs disk. A worktree deleted wrongly costs work that has no
backup.

## Never

- Never `git stash` to clean a tree for removal. The stash stack is shared
  across every worktree; you will corrupt a concurrent agent's parked work.
- Never delete a worktree belonging to a goal that is not yours. Scope
  discipline applies to disk as much as to code.
