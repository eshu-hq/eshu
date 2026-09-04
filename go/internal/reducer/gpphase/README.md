# internal/reducer/gpphase

## Purpose

Owns the identity vocabulary for graph-projection readiness: the conflict
domain a write belongs to (`Keyspace`), the durable milestone it has reached
(`Phase`), the bounded slice identity that names one readiness fact
(`PhaseKey` and its `Validate`), and the two function shapes a domain family
uses to read readiness (`ReadinessLookup`, `ReadinessPrefetch`).

It exists so a domain family can gate a write on graph-projection readiness
**without importing the reducer root**. The root imports the families, so a
family importing the root closes an import cycle. That cycle blocked the
crossrepo family (issue #6061): `GraphProjectionPhaseKey`,
`GraphProjectionReadinessLookup`, and `GraphProjectionReadinessPrefetch` were
defined only at the root, with no leaf home. A trial move of all five
crossrepo-prefixed files (`cross_repo_resolution.go`,
`cross_repo_resolution_retract.go`, `cross_repo_intent_row.go`,
`cross_repo_evidence_type.go`, `cross_repo_evidence_artifacts.go` — a full
build, `go build ./internal/reducer/crossrepotrial/...`, not a read-only
survey) confirms these three were the family's only symbols with no existing
leaf: once qualified as `gpphase.PhaseKey` / `gpphase.ReadinessLookup` /
`gpphase.ReadinessPrefetch` (plus the `gpphase.Keyspace*` /
`gpphase.Phase*` constants the family also reaches), every other undefined
name in the trial build resolved to an *already-hoisted* sibling leaf by
import or call-site rewrite alone — `sharedintent.Row`, `sharedintent.Input`,
`sharedintent.Build` (the root's `BuildSharedProjectionIntent` is a real
function, not an alias, so the leaf must be called directly), and
`contract.DomainRepoDependency` — including `payloadcore.ToStringSlice`,
which the root's unexported `toStringSlice` merely forwards to. With all of
those qualified, the five-file trial package built clean
(`go build -gcflags="-e" ./internal/reducer/crossrepotrial/...` exit 0).

## Ownership boundary

**Owns:** the `Keyspace` and `Phase` enums and their constants, the `PhaseKey`
identity struct and its validation, the `ReadinessLookup` /
`ReadinessPrefetch` function types a family uses to ask "has this slice
reached that phase yet," and — since issue #6061's second pass — the publish
side: `PhaseState`/`PhasePublisher`/`IntentAnchor`/`StateForIntent`/
`StateForIntentValue`/`PublishIntentGraphPhase` for readiness publication, and
`EndpointPresenceRow`/`EndpointPresenceWriter`/`PublishEndpointPresence`/
`EndpointPresenceLookup` for the uid-exact cross-scope presence primitive
(issue #1380).

`PublishIntentGraphPhase` and `PublishEndpointPresence` are this package's one
exception to "plain data, constants, and pure builders": they perform the
publish/upsert I/O through the `PhasePublisher` and `EndpointPresenceWriter`
interfaces a caller supplies. They moved here (rather than staying at the
root, or following platformfam's per-family local-wrapper pattern — see
`platform_materialization.go`'s `publishIntentPhase`, still a valid choice for
a family with no shared consumer) because they were the last root-owned
pieces blocking the ec2, s3, iam, and security_group families from splitting
out of the reducer root without importing it.

**Does not own:** the phase-repair machinery (retry / dead-letter on a failed
publish). That stays at the root: it is orchestration over a publisher and a
repair queue, not a pure builder, and no family needs it today.

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
| `KeyFromScope` | builds a `PhaseKey` from a scope generation and entity keys |
| `AcceptanceUnitID` | derives the acceptance-unit id `KeyFromScope` uses |
| `PhaseState` | one durable readiness publication (key + phase + timestamps) |
| `PhasePublisher` | persists `PhaseState` publications |
| `IntentAnchor` | the bounded intent identity a publish needs, without the durable `Intent` type |
| `IntentAnchor.AcceptanceUnitID()` | the anchor's own acceptance-unit derivation |
| `StateForIntent` | builds a `PhaseState` from an `IntentAnchor`; pure, reports `ok=false` rather than writing a partial key |
| `StateForIntentValue` | the same builder for a caller that already holds a `reducercontract.Intent`; delegates to `StateForIntent` |
| `PublishIntentGraphPhase` | builds and publishes a `PhaseState` for one intent through a `PhasePublisher` |
| `EndpointPresenceRow` | one uid-exact presence fact for a committed endpoint node |
| `EndpointPresenceWriter` | upserts/retracts `EndpointPresenceRow`s |
| `PublishEndpointPresence` | builds and upserts presence rows for a batch of node rows through an `EndpointPresenceWriter` |
| `EndpointPresenceLookup` | answers "which of these uids have no presence row" |

The reducer root keeps aliases under the original names —
`GraphProjectionKeyspace`, `GraphProjectionPhase`, `GraphProjectionPhaseKey`,
`GraphProjectionReadinessLookup`, `GraphProjectionReadinessPrefetch`,
`GraphProjectionPhaseState`, `GraphProjectionPhasePublisher`,
`EndpointPresenceRow`, `EndpointPresenceWriter`, `EndpointPresenceLookup`,
plus one alias per constant — and thin forwarder functions for
`publishIntentGraphPhase`, `graphProjectionPhaseStateForIntent`, and
`publishEndpointPresence` — so every existing caller reaches these through the
root unchanged.

## Dependencies

The standard library, plus `internal/reducer/contract` (for the
`reducercontract.Intent` value type `StateForIntentValue` and
`PublishIntentGraphPhase` accept). No I/O of its own beyond the two functions
above, which call through a caller-supplied `PhasePublisher` or
`EndpointPresenceWriter` interface rather than performing any I/O directly.

**This package must never import `internal/reducer`**, directly or
transitively. Adding that import defeats the entire reason the package exists
and re-blocks every family that depends on it.

## Telemetry

None of its own. This package registers no metric, span, or log field itself
— `PublishIntentGraphPhase` and `PublishEndpointPresence` call through to a
caller-supplied `PhasePublisher` / `EndpointPresenceWriter` implementation,
which is where any publish-path instrumentation lives (unchanged by this
move: it followed the function). The phase-repair instrumentation stays with
that machinery at the root. See the `No-Observability-Change` row for this
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
- `go/internal/reducer/graph_projection_phase.go` — the root aliases for `PhaseState` and `PhasePublisher`, and the phase-publish machinery
- `docs/internal/design/package-restructure.md` — the #6061 restructure and this hoist's no-regression evidence
- `docs/public/observability/telemetry-coverage.md` — the coverage row for this package
