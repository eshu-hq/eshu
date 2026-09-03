# Agent instructions: internal/reducer/gpphase

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The identity vocabulary for graph-projection readiness: `Keyspace` (which
conflict domain a write belongs to), `Phase` (which durable milestone it has
reached), `PhaseKey` (the bounded slice identity, plus `Validate`), and the
`ReadinessLookup` / `ReadinessPrefetch` function shapes a domain family uses
to ask "has this slice reached that phase yet."

It exists so a domain family can gate a graph write on readiness without
importing the reducer root. That is not a style preference: the root imports
the families, so a family importing the root is an import cycle, and that
cycle is exactly what blocked the crossrepo family in issue #6061.

## Hard rules

**Never import the reducer root**, directly or transitively. If you find
yourself wanting a type from `internal/reducer` — `EndpointPresenceRow`/`Writer`/
`Lookup`, say — the answer is either to move that type here too, or that the code
you are writing does not belong in this package. `PhaseState` and
`PhasePublisher` took the first route and now live here; the root aliases them.

**Keep it plain data, constants, and pure validation.** No queue handle, no
graph handle, no worker, no lease, no I/O. The current dependency set is the
standard library only. A new dependency is a design change to be justified in
the PR, not a convenience.

**Constant string values are a storage/query contract, not just a Go
identifier.** `Keyspace` and `Phase` constants are persisted in Postgres and
compared against in Cypher by their literal string value (e.g.
`"cross_repo_evidence"`). Renaming the Go identifier is fine as long as the
alias in the root stays in sync; changing the string value orphans every
already-published readiness row under the old string.

## Adding a new Keyspace or Phase

Adding a constant is safe. Removing or renaming one requires grepping the
full tree for the exact name first — the root's aliases mean both the
`gpphase.KeyspaceFoo` and `reducer.GraphProjectionKeyspaceFoo` spellings are
live call sites, and a family subpackage may reference either.

## `PhaseKey.Validate`

All five identity fields participate: `ScopeID`, `AcceptanceUnitID`,
`SourceRunID`, `GenerationID`, `Keyspace`. A blank field is rejected, not
defaulted — a partially-specified key must not silently collide with an
unrelated bounded slice. Do not relax this to allow any field to be optional
without an owner decision, since callers across the phase-publish and
phase-repair machinery assume `Validate() == nil` means fully bound.
