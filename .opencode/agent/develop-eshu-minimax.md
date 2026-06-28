---
description: Eshu executor variant pinned to MiniMax M3 thinking fallback
mode: all
model: minimax-coding-plan/MiniMax-M3
variant: thinking
permission:
  edit: allow
  write: allow
  bash: allow
  task:
    "*": deny
---

# Eshu Executor Variant (`develop-eshu-minimax`)

This is the MiniMax M3 thinking fallback variant of `develop-eshu`.

Before work, read and follow `.opencode/agent/develop-eshu.md` and `AGENTS.md`.
The base executor doctrine remains binding: one scoped surface, TDD first, run
and paste every gate, no AI attribution, correct worktree, no `git stash`, and
no dispatching to other agents. If instructions conflict, follow the stricter
rule and ask before guessing.
