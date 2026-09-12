# Agent instructions: internal/reducer/intents/shared

Scoped rules for this directory. The root `AGENTS.md` and
`internal/reducer/intents/AGENTS.md` still apply.

## What this package is

A namespace-only parent with no declarations of its own (see
[doc.go](doc.go)). It groups `intents/shared/worker`.

## Hard rules

**Do not add code here.** Put new code in `intents/shared/worker`, not in
this directory's `doc.go`.

**Never import the reducer root**, directly or transitively.
