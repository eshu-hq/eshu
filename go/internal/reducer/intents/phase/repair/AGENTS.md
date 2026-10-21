# Agent instructions: internal/reducer/intents/phase/repair

Scoped rules for this directory. The root `AGENTS.md` and
`internal/reducer/gpphase/AGENTS.md` still apply.

## What this package is

The graph-projection-phase repair-queue drain loop (issue #6061). See
[doc.go](doc.go) and [README.md](README.md).

## Hard rules

**Never import the reducer root**, directly or transitively.

**Name this leaf directly; the root aliases are gone.** `cmd/reducer` and
`internal/reducer/service.go` reach `Repairer`/`Config` by importing this
package (#6626 retired the `GraphProjectionPhaseRepairer` /
`GraphProjectionPhaseRepairerConfig` root aliases). Do not reintroduce root
spellings for these names: the `TestGpphaseRepairRetired` guard fails any
re-declared alias. Renaming a field or method here still changes every
direct caller.

**Do not skip enqueueing to the repair queue on a publish failure**, and do
not make this queue's drain best-effort in a way that silently drops a row:
phase publications and graph writes are not atomic (root `AGENTS.md`'s
"Invariants" section), and this package is the only mechanism that recovers
a committed write whose readiness publication failed.
