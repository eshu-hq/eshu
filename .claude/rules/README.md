---
paths:
  - ".claude/rules/**/*.md"
---

# Path-scoped rules for Claude Code

<!-- This file carries a `paths:` glob on purpose. A rule with no `paths:` field
loads unconditionally at launch, at the same priority as the root AGENTS.md, so an
un-scoped README here would spend context in every session to explain a directory
most sessions never touch. Scoped to the rules themselves, it loads exactly when
someone edits one. -->

Claude Code reads `AGENTS.md` natively (v2.1.277+) because the repository has no
`CLAUDE.md`; a subdirectory's `AGENTS.md` loads when Claude reads a file there.
These rules add what `AGENTS.md` does not carry: which project skill to load for
the file Claude just opened.

Each file here declares a `paths:` glob in YAML frontmatter. Claude Code loads
the rule when it **reads a file matching that glob** — not on every tool use, and
not at launch. A rule with no `paths:` field loads unconditionally at launch with
the same priority as the root `AGENTS.md`; nothing here should do that, because
that is what the root canon is for.

## What belongs here, and what does not

These files are a **routing layer**. They answer one question: *given the file
Claude just opened, which project skill must be loaded and which scoped document
must be read?*

They MUST NOT restate rules from `AGENTS.md`. Two files stating the same rule in
different words is not redundancy, it is a contradiction waiting to happen —
Claude Code's own guidance is that conflicting instructions get resolved
arbitrarily. The canon states the rule once; a rule here points at the skill that
carries the detail.

## Codex is unaffected

Codex does not read `.claude/rules/`. It gets the same routing from the
per-directory `AGENTS.md` files, which stay exactly as they are. Nothing in this
directory is a replacement for those files, and removing them would break Codex.

## Verifying a rule actually fires

A rule that never loads is worse than no rule, because the diff looks like
coverage. To check which instruction files loaded and why, use the
`InstructionsLoaded` hook, which logs exactly that. `scripts/dev/claude-rules-lint.sh`
checks the cheaper failure first: that every declared glob still matches at least
one file in the repo.
