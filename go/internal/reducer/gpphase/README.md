# internal/reducer/gpphase

## Purpose

Owns the identity vocabulary for graph-projection readiness: the conflict
domain a write belongs to (`Keyspace`), the durable milestone it has reached
(`Phase`), the bounded slice identity that names one readiness fact
(`PhaseKey` and its `Validate`), and the two function shapes a domain family
uses to read readiness (`ReadinessLookup`, `ReadinessPrefetch`).

It exists so a domain family can gate a write on graph-projection readiness
**without importing the reducer root**. The root imports the families, so a
family importing the root closes an import cycle. That cycle is exactly what
blocked the crossrepo family (issue #6061): `GraphProjectionPhaseKey`,
`GraphProjectionReadinessLookup`, and `GraphProjectionReadinessPrefetch` were
its only remaining blockers, all three defined in one root file.

## Ownership boundary

**Owns:** the `Keyspace` and `Phase` enums and their constants, the `PhaseKey`
identity struct and its validation, and the `ReadinessLookup` /
`ReadinessPrefetch` function types a family uses to ask "has this slice
reached that phase yet."

**Does not own:** anything that publishes or repairs readiness. `PhaseState`
(one durable readiness publication) and the `GraphProjectionPhasePublisher`
interface that persists it stay at the root in `graph_projection_phase.go` —
they are read and written by phase-publish and phase-repair machinery across
roughly two dozen files, and add no identity concept beyond what `PhaseKey`
and `Phase` already carry. The `EndpointPresenceRow`/`Writer`/`Lookup` trio
also stays at the root: it is a distinct uid-exact, cross-scope presence
primitive (issue #1380), not a same-scope/same-generation readiness fact, and
no family needs it to move.

## Exported surface

| symbol | what it is |
|---|---|
| `Keyspace` | the conflict domain a graph write belongs to |
| `KeyspaceCodeEntitiesUID` etc. | the 13 keyspace identity constants |
| `Phase` | one durable readiness milestone |
| `PhaseCanonicalNodesCommitted` etc. | the 7 phase constants |
| `PhaseKey` | identity of one bounded graph-write readiness slice |
| `PhaseKey.Validate()` | checks the bounded readiness identity contract |
| `ReadinessLookup` | reports whether a slice has reached a phase |
| `ReadinessPrefetch` | resolves readiness for a bounded set of keys |

The reducer root keeps aliases under the original names —
`GraphProjectionKeyspace`, `GraphProjectionPhase`, `GraphProjectionPhaseKey`,
`GraphProjectionReadinessLookup`, `GraphProjectionReadinessPrefetch`, plus one
alias per constant — so every existing caller reaches these through the root
unchanged.

## Dependencies

The standard library only. No I/O, no `context.Context` beyond the two
function-type signatures that pass one through to a caller-supplied
implementation.

**This package must never import `internal/reducer`**, directly or
transitively. Adding that import defeats the entire reason the package exists
and re-blocks every family that depends on it.

## Telemetry

None. This package registers no metric, span, or log field, and performs no
I/O — it is enums, a struct, and a validation method. The phase-publish and
phase-repair instrumentation lives with that machinery at the root and is
unchanged by this hoist. See the `No-Observability-Change` row for this
package in `docs/public/observability/telemetry-coverage.md`.

## Gotchas / invariants

**`PhaseKey.Validate` rejects any blank identity field**, including a blank
`Keyspace`. All five fields — `ScopeID`, `AcceptanceUnitID`, `SourceRunID`,
`GenerationID`, `Keyspace` — participate in the bounded slice's identity, so
a caller that only fills in some of them gets a validation error rather than a
silently under-specified key that would collide with an unrelated slice.

**Keyspace and Phase are plain strings, not opaque tokens.** Their constant
values are the literal strings persisted in Postgres and compared against in
Cypher (e.g. `"cross_repo_evidence"`, `"canonical_nodes_committed"`). Renaming
a constant's value, not just its Go identifier, is a storage-format change
that orphans every already-published readiness row under the old string.

**Adding a new `Keyspace` or `Phase` constant is safe.** Removing or
renaming one that a domain family or the phase-publish machinery still
references is not — grep the full tree for the exact constant name before
touching it, since the alias means both the `gpphase.` and `reducer.`
spellings are live call sites.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/graph_projection_phase.go` — the root aliases, `PhaseState`, and `GraphProjectionPhasePublisher`
- `docs/internal/design/package-restructure.md` — the #6061 restructure and this hoist's no-regression evidence
- `docs/public/observability/telemetry-coverage.md` — the coverage row for this package
