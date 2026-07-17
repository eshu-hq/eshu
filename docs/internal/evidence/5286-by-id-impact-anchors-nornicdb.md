# #5286 — by-id impact reads NornicDB-safe rewrite: before/after

Fixes the two by-id impact reads:

- `POST /api/v0/impact/trace-resource-to-code` (`traceResourceToCode`);
- `POST /api/v0/impact/explain-dependency-path` (`explainDependencyPath`).

Both anchored on the `impactAnchorLabelDisjunction` label set with a
`MATCH (n:A|B|C|…) WHERE n.id = $id` disjunction anchor and projected hop
provenance with a map-valued `[rel IN relationships(path) | {type, confidence,
reason}]` comprehension. On the pinned NornicDB build the label-disjunction
anchor matches **zero rows** and the map-valued rel-property comprehension is
**mangled** (empty hops), so both reads returned nothing / no provenance.

The fix (`go/internal/query/impact_anchor_resolve.go`, `impact.go`):

- **trace-resource-to-code** folds label resolution into the traversal as a
  `CALL{UNION}` of per-label inline-property anchors, each traversing to a
  `Repository`, in a single round-trip. Hop provenance is unwound from the raw
  `relationships(path)` list in Go.
- **explain-dependency-path** resolves the source and target labels in one
  `CALL{UNION}` round-trip, then runs `shortestPath` with single-label
  inline-property anchors on both ends. Hops are built in Go by zipping
  `nodes(path)` with `relationships(path)`.
- Both `relationships(path)` (neo4j.Relationship on Neo4j / map on NornicDB) and
  `nodes(path)` (neo4j.Node on both) are decoded across backends.

Backend: NornicDB `eshu-nornicdb-pr261:149245885258`
(commit `1492458852588c884c32f70d27ea2ee07086769c`), standalone isolated
container, no-auth Bolt on `bolt://localhost:17687`, database `nornic`.
Reproduced through the real HTTP handlers (`httptest`) over
`query.Neo4jReader`.

Machine profile: local developer workstation (macOS, Apple silicon); localhost
Bolt. Same-machine relative before/after on a minimal representative fixture
(`CloudResource` → `Workload` → `Repository`, two `DEPENDS_ON` hops). Absolute
microseconds are not a scaled-corpus SLO.

## Accuracy Evidence (behavior fix — corrected delta)

Measured live in `TestLiveByIdImpactAnchorReads`:

| read | OLD output (pinned NornicDB) | NEW output |
|---|---|---|
| by-id anchor (`MATCH (n:A\|B\|C) WHERE n.id=$id`) | **0 rows** | per-label inline anchor resolves the node |
| trace-resource-to-code | no paths (disjunction 0 rows) + mangled empty hops | 1 path to the `Repository` at depth 2 with 2 decoded hops |
| explain-dependency-path | mangled hops with empty from/to endpoints | shortest path depth 2, 2 hops with correct `from_id`/`to_id` (src→mid→tgt) + aggregate confidence |

The rewrite is also correct on Neo4j: per-label inline-property anchors and a
single-label `shortestPath` are standard, and unwinding `relationships(path)` /
`nodes(path)` in Go avoids the fragile map-valued comprehension.

## Performance Evidence (correctness win)

No-Regression Evidence: warm median over 21 iterations on the seeded fixture,
both handlers per iteration, through `Neo4jReader`:

| path | warm median | result |
|---|---:|---|
| OLD disjunction shapes (trace + explain) | 543.7 µs | 0 rows / mangled hops |
| NEW single-label shapes (trace + explain) | 924.9 µs | correct |

`trace-resource-to-code` is on its own **faster** than the OLD disjunction shape
(the folded `CALL{UNION}` traversal warm-medians ~390 µs vs ~460 µs) because it
stays a single round-trip. `explain-dependency-path` adds one round-trip: the
source/target labels must be resolved before `shortestPath` can anchor them
single-label, which is the unavoidable cost of routing around the label-
disjunction bug. Against a baseline that returns **no valid results**, this is a
`Correctness win`; the absolute latency stays sub-millisecond, and the label
resolves are bounded per-label indexed id lookups.

## Observability Evidence

No-Observability-Change: the fix changes no metric, span, log field, queue stage,
worker knob, or schema phase. Each read still runs through `Neo4jReader.Run`/
`RunSingle`, whose existing `neo4j.query` spans expose every statement.

## Verification

- `go test ./internal/query -run 'ImpactAnchor|TraceResource|ExplainDependency|DependencyPath' -count=1`
  — resolver/traversal/dual-anchor guards (per-label, no disjunction),
  handler flows, limit/truncation, and the resolved-label shape assertions.
- `ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 go test ./internal/query -run TestLiveByIdImpactAnchorReads -count=1 -v`
  — backend-required live before/after through the real handlers.
- `go test ./internal/query -count=1` — full query package green.

## Scope

Completes the label-disjunction by-id impact reads (#5286). The remaining audit
carve-outs (call-chain / route / exposure reads with no single-clause form) are
tracked separately.

Refs #5286
