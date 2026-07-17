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

The fix (`go/internal/query/impact_anchor_resolve.go`, `impact.go`) resolves the
caller identifier to a node with a per-label `CALL{UNION}` that matches on either
the node `id` **or** `name` (callers and the MCP tools pass human identifiers such
as a repository name, not the hashed canonical id), then anchors the traversal on
the resolved canonical id:

- **trace-resource-to-code** resolves the start node (id-or-name), then traverses
  `MATCH path = (start:<label> {id: <resolved id>})-[*1..N]->(repo:Repository)`.
  Hop provenance is unwound from the raw `relationships(path)` list in Go.
- **explain-dependency-path** resolves the source and target (id-or-name), then
  runs `shortestPath` with single-label inline-property anchors on both resolved
  ids. Hops are built in Go by zipping `nodes(path)` with `relationships(path)`,
  in path traversal order (source toward target) — the pinned NornicDB does not
  expose per-relationship start/end node identity in `relationships(path)` (only
  `_edgeId`/`type`/`properties`), and a `startNode(rel).id`/`endNode(rel).id`
  comprehension is mangled to null, so edge-direction endpoints are unavailable;
  the hop `type` carries the semantic direction.
- Both `relationships(path)` (neo4j.Relationship on Neo4j / map on NornicDB) and
  `nodes(path)` (neo4j.Node on both) are decoded across backends.

Two failure modes were confirmed against the live golden-corpus NornicDB (see the
Verification section): the label disjunction returns **zero rows even with a
matching id** (per-label inline anchors resolve it correctly), and the corpus
Repository ids are hashed (`repository:r_<hash>`) while the reads are called with
the repository **name** — so an id-only anchor cannot resolve them, which is why
the resolver matches id-or-name.

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
| OLD disjunction shapes (trace + explain) | 463.2 µs | 0 rows / mangled hops |
| NEW resolve + single-label shapes (trace + explain) | 1.562 ms | correct |

Both reads resolve the caller identifier with a per-label `CALL{UNION}` (id-or-
name, one indexed lookup per branch) and then anchor the traversal / `shortestPath`
on the resolved canonical id. That is +~1.1 ms over the (broken) disjunction
shapes on this minimal fixture — the cost of the id-or-name resolve round-trip
plus the traversal, which is what routing around the label-disjunction bug **and**
supporting name-based callers requires. Against a baseline that returns **no valid
results** (zero rows / a 404 on a name-based call), this is a `Correctness win`;
the absolute latency stays low-single-digit-millisecond and every anchor is an
indexed inline-property lookup.

## Observability Evidence

No-Observability-Change: the fix changes no metric, span, log field, queue stage,
worker knob, or schema phase. Each read still runs through `Neo4jReader.Run`/
`RunSingle`, whose existing `neo4j.query` spans expose every statement.

## Verification

- `go test ./internal/query -run 'ImpactAnchor|TraceResource|ExplainDependency|DependencyPath' -count=1`
  — resolver/traversal guards (per-label, no disjunction),
  handler flows, limit/truncation, and the resolved-label shape assertions.
- `ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 go test ./internal/query -run TestLiveByIdImpactAnchorReads -count=1 -v`
  — backend-required live before/after through the real handlers.
- `go test ./internal/query -count=1` — full query package green.
- **Golden-corpus validation.** Against the live B-7 gate NornicDB
  (`scripts/verify-golden-corpus-gate.sh --keep`), on the bootstrapped 20-repo
  corpus: the label disjunction `MATCH (n:A|B|C) WHERE n.id = <hashed id>`
  returned 0 rows while the per-label inline anchor returned the node; the
  Repository `orders-api` has id `repository:r_ea78e8bb` (name `orders-api`); and
  the shipped handlers, called with the corpus's own MCP arguments
  (`explain_dependency_path {source: "orders-api", target: "lib-common"}`,
  `trace_resource_to_code {start: "orders-api"}`), returned HTTP 200 with a real
  shortest path (`orders-api -DEPENDS_ON-> lib-common`, depth 1) and a repo path
  — the id-or-name resolver is what makes the name-based call resolve.

## Scope

Completes the label-disjunction by-id impact reads (#5286). The remaining audit
carve-outs (call-chain / route / exposure reads with no single-clause form) are
tracked separately.

Refs #5286
