---
description: Eshu executor variant pinned to GPT-5.5 high
mode: all
model: openai/gpt-5.5
variant: high
permission:
  edit: allow
  write: allow
  bash: allow
  task:
    "*": deny
---

# Eshu Executor Variant (`develop-eshu-gpt55-high`)

This is the GPT-5.5 high variant of `develop-eshu`.

Before work, read and follow `.opencode/agent/develop-eshu.md` and `AGENTS.md`.
The base executor doctrine remains binding: one scoped surface, TDD first, run
and paste every gate, no AI attribution, correct worktree, no `git stash`, and
no dispatching to other agents. If instructions conflict, follow the stricter
rule and ask before guessing.
