# #5287 — change-surface impact-traversal NornicDB-safe rewrite: before/after

Fixes the two change-surface impact traversals:

- `POST /api/v0/impact/change-surface/investigate` (`changeSurfaceImpactRows`) —
  old shape: `MATCH (start:L {id}) MATCH path=(start)-[*1..N]->(impacted) …
  RETURN DISTINCT …, length(path) AS depth`;
- `POST /api/v0/impact/change-surface` (`findChangeSurfaceImpactRows`, legacy) —
  old shape: `MATCH (start:L {id}) OPTIONAL MATCH path=(start)-[rels*1..N]->(impacted)
  … UNWIND relationships(path) AS rel WITH impacted, rel, length(path) AS depth
  RETURN DISTINCT …, type(rel), rel.confidence, rel.reason, depth`.

Both are multi-clause shapes the pinned NornicDB build mis-executes. Both collapse
to a single `MATCH path = (start:Label {id:$target_id})-[*1..N]->(impacted)` clause
that folds the start anchor into the path pattern: the investigate read groups
distinct impacted nodes with `min(length(path))` in the same clause; the legacy
read projects the raw `relationships(path)` list and unwinds the per-edge
provenance (`rel_type`/`confidence`/`reason`) in Go
(`go/internal/query/impact_change_surface_response.go`,
`impact_change_surface_legacy.go`).

Backend: NornicDB `eshu-nornicdb-pr261:149245885258`
(commit `1492458852588c884c32f70d27ea2ee07086769c`), standalone isolated
container, no-auth Bolt on `bolt://localhost:17687`, database `nornic`.
Reproduced through Eshu's own driver path (`query.Neo4jReader.Run` →
`session.Run`, `AccessModeRead`) and the shipped handler methods.

Machine profile: local developer workstation (macOS, Apple silicon); localhost
Bolt. Same-machine relative before/after on a minimal representative fixture
(a Workload `start` → `Repository` at depth 1 via `DEPENDS_ON` → `CloudResource`
at depth 2 via `CONTAINS`). Absolute microseconds are not a scaled-corpus SLO;
OLD and NEW were measured back-to-back on the identical seeded graph.

## Accuracy Evidence (behavior fix — corrected delta, not identity)

Measured live in `TestLiveChangeSurfaceImpactTraversal`:

| read | OLD output (pinned NornicDB) | NEW output |
|---|---|---|
| investigate (2× MATCH + DISTINCT + length) | **0 rows** — every impacted node dropped | 2 rows: `r1` (depth 1), `cr` (depth 2) |
| legacy (OPTIONAL + UNWIND + WITH + DISTINCT) | 1 row, **all fields null** (`id/rel_type/confidence/depth = null`) | 3 per-edge rows: `r1` DEPENDS_ON 0.9 (depth 1), `cr` DEPENDS_ON 0.9 (depth 2), `cr` CONTAINS 0.8 (depth 2) |

The rewrite is also correct on Neo4j: `min(length(path))` is the standard shortest-
depth grouping, and unwinding `relationships(path)` in Go reproduces the old
per-edge provenance without the fragile `RETURN DISTINCT` over a `UNWIND`.

Two supporting findings from the proof (recorded in the pitfalls reference):

- A **bare untyped** `(start)-[*1..N]->(impacted)` traverses correctly in a single
  clause; only the surrounding multi-clause shape corrupted the old reads.
- A server-side environment predicate of the form `($env = '' OR
  coalesce(impacted.environment, '') = '' OR impacted.environment = $env)`
  silently drops **every** row when combined with a `relationships(path)`
  projection on this backend — the `$env = ''` param disjunct is the offender.
  The narrower `(impacted.environment = $env OR coalesce(impacted.environment,
  '') = '')` form (added only when an environment is requested) is safe with the
  `relationships(path)` projection and is applied **server-side before LIMIT** so
  an environment-scoped read cannot under-report, with the Go-side filter kept as
  a re-check.

- `relationships(path)` is serialized differently by the two backends: the Neo4j
  Go driver returns `neo4j.Relationship` values (with `Type`/`Props`), while
  NornicDB returns each relationship as a `map[string]any` with a nested
  `properties` map. `changeSurfaceRelEdges` decodes **both** shapes so the
  per-edge provenance survives on either backend (unit test
  `TestChangeSurfaceRelEdgesDecodesBothBackendShapes`).

## Performance Evidence (correctness win; also faster)

No-Regression Evidence: warm median over 21 iterations on the seeded fixture,
back-to-back on the pinned backend, both change-surface reads per iteration:

| path | warm median | result |
|---|---:|---|
| OLD multi-clause (investigate + legacy) | 494.5 µs | empty / all-null |
| NEW single-clause (investigate + legacy) | 446.8 µs | correct |

The NEW single-clause shape is ~48 µs (~10%) faster because it drops the
`OPTIONAL MATCH` + `UNWIND` + `WITH` pipeline for one anchored traversal per read;
the per-edge unwind moves to Go. Classification: `Correctness win` (also a small
handler win). The traversal is anchored on a single indexed start node with a
bounded depth (`*1..N`, N ≤ 8), so cost scales with the bounded neighborhood.

## Observability Evidence

No-Observability-Change: the fix changes no metric, span, log field, queue stage,
worker knob, or schema phase. Both reads still run through `Neo4jReader.Run`,
whose existing `neo4j.query` span continues to expose each traversal statement to
an operator.

## Verification

- `go test ./internal/query -run 'ChangeSurface|TestChangeSurfaceTraversalQueriesAreNornicDBSafe' -count=1`
  — investigate/legacy handler flows, depth clamping, truncation, bare-target
  resolution, and the static single-clause guard. The guard was proven to fail
  when an `UNWIND` / `RETURN DISTINCT` is reintroduced.
- `ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 go test ./internal/query -run TestLiveChangeSurfaceImpactTraversal -count=1 -v`
  — backend-required live before/after against the pinned NornicDB, asserting the
  shipped `changeSurfaceImpactRows` and `findChangeSurfaceImpactRows` return the
  correct impacted nodes and per-edge provenance.
- `go test ./internal/query -count=1` — full query package green.

## Scope

Completes the #5287 change-surface sweep item. The remaining audit carve-outs
(call-chain / route / exposure reads with no single-clause form) are tracked
separately.

Refs #5287
