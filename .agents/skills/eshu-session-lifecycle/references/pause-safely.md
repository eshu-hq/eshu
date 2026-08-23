# Pause safely

You own a clean stop. Leave a checkpoint a cold agent can resume from.

Triggers: "pause", "I'm going offline", "restart", "board my flight", and the
turn where context is about to compact or summarize.

This playbook is explicit only. "Keep going", "going to bed, keep going", and
"don't stop" mean continue. Do not pause on those.

## Steps

1. **Stop at a boundary.** Finish the current atomic step or back out of it.
   Never stop mid-edit in a tree you know is broken. Start nothing new.

2. **Cancel what you spawned.** Stop nested subagents and kill background
   processes you started — load generators, compose stacks, poll loops. None of
   that is resumable state, and a leaked busy-loop keeps burning CPU on a
   machine other agents are sharing. Note the compose project names you brought
   down.

3. **Do not cross an irreversible line to pause.** No push, no PR, no
   merge, no deploy. Pausing is a local act. If a PR was already open before the
   pause, it stays as it is.

4. **Make the work durable on the branch.** Confirm HEAD is on a named branch
   first, then commit:

   ```bash
   git symbolic-ref -q HEAD
   git add -A
   git commit -m "wip: <one line on where this stopped>"
   ```

   `git add -A` here is the one sanctioned exception to the executor rule in
   `.opencode/agent/develop-eshu.md` ("Never `git add -A`, stage explicit
   paths"). That rule exists so a focused change does not sweep up unrelated
   edits. A pause checkpoint is the opposite job: its whole purpose is to
   capture the full uncommitted tree, and staging explicit paths is how you
   discover afterwards that the one file you forgot was the one that mattered.
   Anywhere other than a pause commit, the executor rule stands.

   If the tree is knowingly broken, say so in the commit body in one line.

   Never `--no-verify`. Never `git stash` — the stash stack is shared across
   every worktree of the repo, and a concurrent agent will corrupt it.

5. **Confirm the commit actually landed.** A pre-commit run that gets killed
   partway never restores its patch stash, and the tree afterwards looks clean
   whether it succeeded or silently rolled your work back. Clean is not the
   same as committed:

   ```bash
   git log --oneline -1
   git status --short
   git stash list
   ```

   An empty `git status` with your commit missing from `git log` means the work
   was rolled back, not saved. Recover before you walk away.

6. **Write the resume note somewhere compaction cannot reach.** An in-context
   plan does not survive summarization, and a note inside the worktree either
   pollutes the diff or gets pruned with the tree. Write it to a path outside
   the working tree and name that exact path in the `wip:` commit body so a cold
   agent can find it from `git log` alone. Default location:

   ```
   ~/.eshu/resume/<branch>.md
   ```

## Resume note: required slots

Mirror the handoff contract in
[Agent Orchestration Model](../../../docs/internal/agent-orchestration.md), since
a resume note is a handoff to your future self. Every slot is required; write
"none" rather than dropping one.

1. **Surface** — the exact files in play, one ownership boundary.
2. **Goal** — what done looks like, as a checkable statement.
3. **State** — branch, HEAD SHA, whether the tree is clean, whether the tree
   builds.
4. **Proven** — what you actually ran, with the output, and when relative to the
   last edit.
5. **Assumed** — every belief you did not verify. This is the slot that saves
   the next agent, and the one most often skipped.
6. **Gate commands** — the exact focused verification for this surface.
7. **Out of scope** — boundaries the resuming agent must not cross.
8. **Parallel work** — other active worktrees and PRs touching nearby code, read
   live from `git worktree list` and `gh pr list`, never hard-coded.
9. **Next action** — one sentence. Not a list of options.

## Reply

Your report must contain:

- where in the work you stopped
- what is on disk versus still only in your head, by path
- the commit SHA you made, and whether the tree is clean and builds
- the resume note path
- the first action on resume

Paths and SHAs, not diff dumps. This is a pause, not a final report.
