# Agent instructions: internal/reducer/intents/phase/repair

Scoped rules for this directory. The root `AGENTS.md` and
`internal/reducer/gpphase/AGENTS.md` still apply.

## What this package is

The graph-projection-phase repair-queue drain loop (issue #6061). See
[doc.go](doc.go) and [README.md](README.md).

## Hard rules

**Never import the reducer root**, directly or transitively.

**Keep `GraphProjectionPhaseRepairer`/`GraphProjectionPhaseRepairerConfig`
root aliases in sync.** `cmd/reducer` and `internal/reducer/service.go`
reach `Repairer`/`Config` only through those root aliases. Renaming a field
or method here changes the root API too.

**Do not skip enqueueing to the repair queue on a publish failure**, and do
not make this queue's drain best-effort in a way that silently drops a row:
phase publications and graph writes are not atomic (root `AGENTS.md`'s
"Invariants" section), and this package is the only mechanism that recovers
a committed write whose readiness publication failed.
