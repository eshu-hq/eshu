# Agent instructions: internal/reducer/intents

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

A namespace-only parent with no declarations of its own (see
[doc.go](doc.go)). It groups `intents/shared` and `intents/phase`.

## Hard rules

**Do not add code here.** If you need to add a type, function, or constant
under `internal/reducer/intents`, put it in the relevant child package
(`intents/shared/worker` or `intents/phase/repair`), not in this directory's
`doc.go`. A namespace-only parent that starts accumulating code stops being
one, and every existing `intents/...` import path assumes it stays empty.

**Never import the reducer root** from any package under this tree,
directly or transitively.
