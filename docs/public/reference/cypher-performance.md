# Cypher Performance Discipline

Use this page before changing hot-path Cypher, graph schema, graph-write
batching, reducer projection, query handlers, materialization jobs, or pinned
graph backend versions. Maintainer implementation details live in
`go/internal/storage/cypher/README.md` and the `cypher-query-rigor` project
skill.

Accuracy comes first. A faster query that returns wrong graph truth is a
product failure.

## Mandatory Checks

Every hot-path Cypher change needs both checks before merge.

### 1. Research The Pinned Backend

Neo4j:

- Read the Cypher manual for the pinned major version.
- Check changelogs when planner or syntax behavior matters.
- Confirm recent features such as subqueries, dynamic labels, or vector indexes
  exist in the pinned version before using them.

NornicDB, Eshu's default backend:

- Use the current NornicDB-New checkout named by local config, repo docs, or the
  user. Do not rely on an older sibling checkout unless the run explicitly uses
  it.
- Read the relevant `pkg/cypher/` and `pkg/storage/` source for the production
  query shape.
- Check [NornicDB Pitfalls](nornicdb-pitfalls.md) and
  [NornicDB Tuning](nornicdb-tuning.md).

For both backends, prove any unfamiliar query pattern against the pinned binary
before designing production code around it. Record the backend version or
NornicDB-New commit in the PR evidence.

### 2. Measure The Same Shape Before And After

Unmeasured Cypher in a hot path is a regression risk. Capture before/after
evidence against the same inputs and pinned backend binary.

Preferred proof ladder:

| Shape | Use when | Evidence |
| --- | --- | --- |
| Focused Go benchmark | Writer code lives under `go/internal/storage/cypher` or a narrow adapter. | `go test -bench=. -benchmem`, with `ops/sec`, `ns/op`, and `B/op`. |
| Compose-stage timing | Query fires only through reducer, projector, or bootstrap flows. | Structured log duration, input size, output count, queue state, backend, and schema state. |
| Manual reproducer | Admin or one-off materialization query. | Wall time, row count, dataset shape, backend version, and schema/index state. |

Record:

- backend and version or image tag
- whether `eshu-bootstrap-data-plane` applied schema first
- input cardinality at each anchor
- indexes and constraints present
- Neo4j `PROFILE` or NornicDB statement summaries when available

Correctness-only Cypher changes still need a same-shape no-regression check.
If a benchmark is not load-bearing, say why in the tracked evidence note.

## CI Evidence Gate

`scripts/verify-performance-evidence.sh` checks changed hot-path Go files,
graph writes, collectors, workers, leases, batching, concurrency primitives,
Compose, Helm, pprof, and NornicDB knobs.

`scripts/verify-query-plan-regression.sh` validates the static hot-path
query-plan fixtures under `go/internal/queryplan/testdata/` against the NornicDB
schema statement contract. The gate also parses every non-test Go file
recursively beneath `go/internal/query` and compares each `Run` or `RunSingle` owner against
`query-source-coverage.yaml` by file, enclosing symbol, and exact call count.
Every callsite must link to registered hot entries or carry an explicit non-hot
disposition. Typed non-hot dispositions freeze the reviewed source symbol and
declare machine-checked key/result bounds. Grandfathered prose dispositions are
immutable source-digest records: new or changed source must use the typed form.
Hot dispositions also freeze the full graph-executing production symbol, so
retaining the same `Run` count while rerouting to another query fails the gate.
Handler and legacy entries contain no copied Cypher. Handler entries bind exact
query and builder-source SHA-256 values plus an anchor `query_fragment` to the
production builder; legacy entries bind exact query fingerprints to their
declared production builder or execution-path owners and freeze those owners by
source SHA-256. The regression script runs
both query-package binding tests, which fill the manifests with production bytes
before applying shape validation. New or stale execution sites, missing
dispositions, unknown entry links, source-fragment or fingerprint drift,
unbounded variable-length traversals, unlabeled anchors, unordered pagination,
missing schema evidence, and forbidden plan signatures fail the gate. The same
script provisions a pinned, isolated Neo4j container and runs the build-tagged
live proof in `go/internal/query/queryplan_profile_live_test.go`. That proof
profiles 22 handler entries and 30 legacy entries through Neo4j `PROFILE` using
production-owned bytes, plus 735 hash-frozen safe production variants: 787
shapes in total. The production-variant family includes 140 distinct
import-dependency queries mapped from all 244 valid API and MCP request shapes.
Cloud-resource browsing is covered separately by one UID-bounded graph
hydration plan and 64 hash-frozen Postgres page variants: 32 filter/cursor
combinations each for all-scope and scoped access. A label or relationship-type
scan is accepted only by the closed code-level operator policy; manifest data
cannot add an exception.
Static validation
does not replace live backend `EXPLAIN`,
`PROFILE`, or before/after runtime measurements for production Cypher changes.

Hot-path changes must update a versioned repo file with one benchmark marker:

- `Performance Evidence:`
- `Benchmark Evidence:`
- `No-Regression Evidence:`

and one observability marker:

- `Observability Evidence:`
- `No-Observability-Change:`

PR text alone is not enough.

Good:

```text
Performance Evidence: focused writer benchmark on NornicDB v1.0.45 with
50,000 File rows moved from 820ms to 310ms; full corpus stayed drained at
896/896 repositories with 0 open queue rows.

Observability Evidence: existing eshu_dp_canonical_phase_duration_seconds and
shared-edge summaries expose phase, row count, and relationship route.
```

Bad:

```text
Performance Evidence: looks faster locally.
Observability Evidence: logs are probably enough.
```

## Backend-Specific Behavior

Prefer backend-neutral Cypher. When behavior diverges, use this order:

1. Restructure the query into a shape both backends handle the same way.
2. Add a narrow dialect seam under `go/internal/storage/cypher/` for schema DDL,
   connection/runtime settings, retry classification, query builders, or
   measured adapters.
3. Patch NornicDB only for an evidence-backed correctness fix, general backend
   performance win, or measured Eshu runtime win.

Do not add backend branches in reducers, query handlers, MCP tools, or
collectors.

## Anti-Patterns

- no baseline
- Neo4j docs cited for NornicDB behavior
- unit tests used as production-cardinality performance proof
- Compose success without phase timing or queue evidence
- index changes without write-amplification discussion
- worker-count or batch-size serialization used as a concurrency fix
- CTE materialization treated as a reference-count rule instead of a measured
  plan contract (Postgres 12+ materializes side-effect-free CTEs referenced
  more than once by default, while single-reference side-effect-free CTEs are
  normally folded)
- a row-set equivalence differential treated as sufficient for a lock/claim/lease
  rewrite (it drops `FOR UPDATE`; a separate EvalPlanQual/lease-safety proof is
  required)
- a DSN-gated performance proof that skips in CI with no hermetic in-CI guard

## Quick Reference

| Need | Neo4j | NornicDB |
| --- | --- | --- |
| Cypher feature support | Cypher manual for pinned major | `pkg/cypher/*.go` in NornicDB-New |
| Storage/constraint behavior | Operations manual | `pkg/storage/*.go` in NornicDB-New |
| Known traps | Neo4j changelog | [NornicDB Pitfalls](nornicdb-pitfalls.md) |
| Runtime knobs | Neo4j config reference | [NornicDB Tuning](nornicdb-tuning.md) |
| Version pinning | `NEO4J_VERSION` | `NORNICDB_IMAGE` |

## Evidence Notes

### Call-Chain And Impact Unlabeled-Anchor Label Seed

Issue #3567 (surfaced during #3498/#3566): several Neo4j-compat reads resolved
their start/target anchor by id or name without a labeled anchor, so the Neo4j
planner had no label to seed an index from and resolved the predicate with an
all-node scan. The NornicDB default path already anchors these reads with inline
property patterns, so this was a Neo4j-compat-only shape defect — the same class
as the issue #3378 (`cloud_resource_candidates`) and #3384 (legacy
change-surface) all-node-scan fixes.

The three reads and their new anchors:

- `go/internal/query/code_call_chain.go` (`buildCallChainCypher`, Neo4j builder
  only): `MATCH (start)` / `MATCH (end)` →
  `MATCH (start:Function|Class|Struct|Interface|TypeAlias|File)` /
  `MATCH (end:...)`. The label disjunction mirrors the authoritative CALLS-source
  label set the canonical edge writer projects
  (`codeCallRetractSourceLabels` in `internal/storage/cypher`), so every
  call-chain endpoint still resolves. The id/uid and name predicates, the repo
  scoping, the `(start)-[:CALLS*1..N]->(end)` shortestPath, the projection, and
  the `LIMIT 5` are byte-identical; only the anchor label moved into the MATCH.
  `buildNornicDBCallChainCypher` keeps its existing inline-property anchor and is
  untouched.
- `go/internal/query/impact.go` (`traceResourceToCode`, ~line 215 and the ~line
  233 fallback hydration): `MATCH (start) WHERE start.id = $start_id` and
  `MATCH (n) WHERE n.id = $id` → `MATCH (start:<impact-anchor-disjunction>)` /
  `MATCH (n:<impact-anchor-disjunction>)`, predicate unchanged.
- `go/internal/query/impact.go` (`explainDependencyPath`, ~lines 312-313):
  `MATCH (source) WHERE source.id = $source_id` and the target equivalent →
  label-seeded anchors, `shortestPath((source)-[*1..8]-(target))` unchanged.

The impact-anchor label disjunction
(`Repository|Workload|WorkloadInstance|CloudResource|TerraformModule|DataAsset|Platform|Endpoint|CloudAction|EvidenceArtifact`,
`impactAnchorLabelDisjunction`) enumerates the id-bearing platform graph labels a
canonical entity id can resolve to. Plain `id` (as distinct from `uid`) is
written only on these deployment/infrastructure/repository nodes, each of which
declares an id uniqueness constraint or `nornicdb_*_id_lookup` index in
`internal/graph/schema.go`, so `MATCH (n:<disjunction>) WHERE n.id = $id`
resolves the same node set as the prior unlabeled `MATCH (n) WHERE n.id = $id`
while the planner seeds a per-label index seek. uid-keyed labels in the set
(TerraformModule, DataAsset) never satisfy the `id` predicate, so including them
does not widen the match set. The disjunction-with-property anchor is the shared
Cypher/Bolt shape the canonical edge writers already use
(`canonical_instantiates_edges.go`, `canonical.go`), so it is portable across
NornicDB and Neo4j and adds no backend branch.

No-Regression Evidence: this is a correctness-of-shape fix that strictly removes
the all-node anchor scan; no live PROFILE was available because no local
NornicDB-New checkout is present (stated per cypher-query-rigor). Input
cardinality at each anchor drops from all graph nodes to the labeled id-indexed
populations; the predicates, traversals, projections, ordering, and bounds are
unchanged, so the result set is preserved on both backends. Shape is proven by
`go test ./internal/query -run
'BuildCallChainCypher|TraceResourceToCode|ExplainDependencyPath' -count=1` (the
new `TestBuildCallChainCypherNeo4jAnchorsCodeCallLabels`,
`TestBuildCallChainCypherNeo4jAnchorsNameLookup`,
`TestBuildCallChainCypherNornicDBUnchanged`,
`TestTraceResourceToCodeAnchorsLabeledStart`,
`TestTraceResourceToCodeFallbackHydrationAnchorsLabeled`, and
`TestExplainDependencyPathAnchorsLabeledEndpoints` regressions, which fail if any
of these anchors reverts to an unlabeled scan), the full
`go test ./internal/query/... -count=1` (3278 tests),
`go test ./cmd/api ./internal/mcp -count=1` (666 tests), and
`scripts/verify-query-plan-regression.sh`.

No-Observability-Change: the handlers keep the existing `GraphQuery.Run`/
`RunSingle` adapters, `neo4j.query` spans, and query-duration metrics; the
response shapes (`truncated` flags, path/hop projections) are unchanged. No new
worker, queue, metric, span, or runtime knob is introduced; the query shape
changed but the per-query telemetry surface did not.

### Relationships Verb Catalog Live Scaling Fix

Current contract note: `relationshipVerbCatalog` holds 20 verbs as of #5360 PR
B (19 as of #5369; the historical measurements below were taken at 16 verbs,
before the three Atlantis governance verbs -- `MANAGES`, `ATLANTIS_DEPENDS_ON`,
`USES_WORKFLOW` -- were added; the type-indexed count and index-ordered edge
slice shapes below are unchanged by either addition, so the measured
per-verb costs still apply). #5360 PR B's `RECONCILES_FROM` verb reuses the
same builder functions with `targetIdentityProperty` unset (like every
non-MANAGES verb), so it adds no new shape, only one more type-indexed count
call. The verb count itself is not restated
elsewhere in this doc; `relationshipVerbCatalog`
(`go/internal/query/relationships_catalog_cypher.go`) is the source of truth.

No-Regression Evidence (#5369): the three added verbs use the exact same
`relationshipCountCypher`/`relationshipEdgesCypher` builder functions as every
other catalog entry -- `relationshipCountCypher` is unmodified, and
`relationshipEdgesCypher`'s only change is an additive per-entry
`targetIdentityProperty` override (empty for every non-MANAGES verb) that
keeps the emitted Cypher byte-identical for the `QP-RELATIONSHIPS-EDGES`
`CALLS` representative (`cypher_sha256` unchanged in
`go/internal/queryplan/testdata/hot-cypher.yaml`). Each new verb's count is
one bare `MATCH ()-[r:VERB]->() RETURN count(r)` relationship-type-index
lookup (O(1) per verb, the same shape measured above), so the catalog
sequentially issues 19 type-indexed counts instead of 16 -- three more calls
on an already-sub-second (0.42s for 16, per the table above) type-indexed
path, not three more source-label scans. `MANAGES`'s edge slice keeps the
same source-label-anchored, index-ordered, `LIMIT`-bounded shape as every
other verb; only its `target_id` coalesce order changed (to resolve the
`Directory` target's canonical `path` instead of falling through to the
non-unique basename), which does not add a traversal, index lookup, or sort
pass.

No-Observability-Change (#5369): the three new verbs reuse the existing
`getRelationshipsCatalog`/`getRelationshipEdges` handler telemetry
(`WriteSuccess`/`BuildTruthEnvelope`, the shared `GraphQuery.Run`/`RunSingle`
spans) with no new metric, span, log line, or runtime knob.

Performance Evidence: issue #3429. At post-merge E2E scale (~900k typed edges /
~500k nodes) `POST /api/v0/relationships/catalog` timed out warm (>30s, HTTP
000), and `POST /api/v0/relationships/edges` ran 5-8.5s. Live profiling against
the local Compose NornicDB backend (`/db/nornic/tx/commit`, db `nornic`)
isolated two distinct root causes, neither of which the static query-plan gate
can see because it validates query *shape*, not live wall-clock.

Backend: NornicDB via local Compose Bolt-HTTP. Corpus: ~900k typed edges /
~500k nodes (warm). Timings are `time -p` real seconds of the Bolt-HTTP call.

Root cause 1 — catalog count scanned the source-label population per verb. The
original shape `MATCH (s:<SourceLabel>)-[r:<VERB>]->() RETURN count(r)` forced a
scan of the entire source-label population for each of the 16 verbs, *regardless
of how many edges of the verb exist*. The largest label (`File`, used by
`IMPORTS`) cost 8.88s by itself to return `0`. The bare relationship-type
aggregate `MATCH ()-[r:<VERB>]->() RETURN count(r)` is answered by the
relationship-type index and is near-instant. The anonymous `()` endpoints are
not the gate's unlabeled-bound-node pattern `(s)`, so the shape still passes the
static gate (`unlabeledMatchPattern` only flags a bound, unlabeled node).

- `MATCH (s:File)-[r:IMPORTS]->() RETURN count(r)` — **8.88s**, returns 0.
- `MATCH ()-[r:IMPORTS]->() RETURN count(r)` — **0.04s**, returns 0.
- `MATCH (s:Function)-[r:CALLS]->() RETURN count(r)` — **2.46s**, returns 20389.
- `MATCH ()-[r:CALLS]->() RETURN count(r)` — **0.76s** (then 0.04s warm),
  returns 21355.

The count value also changes on purpose: the source-label anchor silently
undercounted verbs whose edges originate from more than one source label (CALLS
originates from Function 20389, File 954, Class 11, Struct 1). The OpenAPI
contract already documents this field as a "bounded whole-graph edge count," so
the relationship-type aggregate (21355 for CALLS) is the *correct* whole-graph
truth, and the prior 20389 was a contract-violating subset. All 16 type-indexed
counts together run well under 2s.

Root cause 2 — edge slice sorted on a non-indexed `coalesce()` expression. The
edge query keeps the source-label anchor (a bare-type edge match with bound,
unlabeled endpoints profiled at 18-29s on NornicDB — far worse), but its
`ORDER BY source_name, source_id, target_id` over projected `coalesce()`
expressions forced NornicDB to materialize and sort the verb's full edge set
before applying `LIMIT`. Re-basing the order onto the indexed source-anchor
property (`ORDER BY s.<sourceProperty>`) lets the index-ordered scan
short-circuit at the page boundary.

- `MATCH (s:Function)-[r:CALLS]->(t) ... ORDER BY source_name, source_id,
  target_id LIMIT 51` — **2.46-2.65s**.
- `MATCH (s:Function)-[r:CALLS]->(t) ... ORDER BY s.uid LIMIT 51` — **0.11s**.
- `MATCH (s:Repository)-[r:DEPLOYS_FROM]->(t) ... ORDER BY s.id LIMIT 51` —
  **0.11s**.

Live endpoint before (`curl -w` total seconds, warm, against
`http://localhost:8080`, against the running main-built stack):

| Endpoint | Before |
| --- | --- |
| `POST /api/v0/relationships/catalog` | 22.16s (HTTP 200, over budget; >30s timeout when cold) |
| `POST /api/v0/relationships/edges` (CALLS) | 5.02s |
| `POST /api/v0/relationships/edges` (IMPORTS) | 8.58s |

Endpoint-equivalent before/after measured directly against the same NornicDB
(the dominant cost is the per-verb graph work; HTTP/handler overhead is
negligible). The catalog row replays the 16 source-anchored counts vs the 16
type-indexed counts sequentially, exactly as the handler issues them:

| Work | Before | After | Speedup |
| --- | --- | --- | --- |
| Catalog: 16 per-verb counts (sequential) | 19.87s | 0.42s | ~47x |
| Edges (CALLS), `LIMIT 51` | 3.85s | 0.05s | ~77x |
| Edges (DEPLOYS_FROM), `LIMIT 51` | 0.62s | 0.15s | ~4x |

Both bring the catalog and the populated-verb edge slices well under the
few-second budget.

Residual: `IMPORTS` has `0` edges in the corpus yet anchors on the large `File`
label, so its *edge slice* still scans `File` (`~9.9s` even without ORDER BY).
This is bounded and only reached if the UI drills into a verb the catalog count
already reports as `0`, so it is out of scope for this fix; a future change can
re-anchor empty/large-label verbs or drive the edge slice from the
relationship-type index if drill-down latency on empty verbs becomes a problem.

The gate entries `QP-RELATIONSHIPS-CATALOG-COUNT` and `QP-RELATIONSHIPS-EDGES`
in `go/internal/queryplan/testdata/hot-cypher.yaml` are updated to the new
shapes (count drops the `Function` source-label schema evidence and declares a
`RelationshipTypeScan`; edges keep the source-label anchor and `s.uid` ordering).
The capability budget for `platform_impact.relationships_catalog` is unchanged
(2000 ms local p95, 3000 ms production p95).

**PR review follow-up (3 P2 threads):**

Thread 1 (count scope): the whole-graph count is the only index-served option on
NornicDB — there are no composite relationship-type+label indexes (confirmed via
`SHOW INDEXES`). A source-label-anchored count (`MATCH (s:Label)-[r:VERB]->()`)
measured at 30.7s for all 16 verbs. The fix is to document the divergence
explicitly: the count is whole-graph, the edge slice is source-label-anchored;
when a verb has edges from multiple source labels (e.g. `DEPENDS_ON` written for
both `Repository` and `Workload` sources), the tile count may exceed the
drill-down count. OpenAPI description and gate caveat updated accordingly.

Thread 2 (tie-breaker): added `coalesce(t.id, t.uid)` as a deterministic
secondary ORDER BY. Measured impact:

- `ORDER BY s.uid LIMIT 51` (CALLS) — **0.05s**
- `ORDER BY s.uid, coalesce(t.id, t.uid) LIMIT 51` (CALLS) — **0.10s**

Negligible overhead; the tie-breaker resolves within the already-bounded first-page
set without a separate sort pass.

Thread 3 (gate schema): `required_schema` updated from `function_name` /
`function_lang` to `function_uid_unique` / `nornicdb_function_uid_lookup` — the
two indexes that actually back `ORDER BY s.uid` on the `Function` label. The gate
now enforces the real backing and will fail if those indexes are removed.

Observability Evidence: the handlers retain the existing query-handler envelope
and shared `GraphQuery.Run`/`RunSingle` adapters. The count and edge query
shapes plus the static query-plan gate add no new spans, runtime knobs, queue
behavior, or graph writes. The source-tool breakdown fan-out described below
now emits `eshu_dp_relationship_breakdown_permit_wait_seconds`,
`eshu_dp_relationship_breakdown_queued`, and
`eshu_dp_relationship_breakdown_in_flight`.

#### Retained Dashboard Source-Tool Breakdown Follow-Up

The candidate-isolation timings in the following three retained-dashboard
sections were collected while narrowing the source-tool, incoming-story, and
anchor-resolution bottlenecks. They remain the OLD/NEW theory proof, but they
are not used as final-source image identity. An intermediate browser acceptance
checkpoint was collected against API image
`sha256:9d7c22bca6d063de04c024e02f8c44aa11f12b7acbd35ad83a15b1bc155d8faa`,
whose binary embeds the exact Go/Docker input manifest
`94ede3d9188dbf38421adbd3537e9678153420a62cf614fa7cabfd6aa099c687`.
That sidecar used the unchanged retained stores and NornicDB v1.1.11 digest
`sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`.
The response-backed browser gate passed Relationships in 6.773 seconds and Code
Graph in 12.232 seconds; the complete bearer sweep passed 37/37 routes in
128.224 aggregate route seconds with no route above 18.007 seconds. These are
named intermediate checkpoint values, not the final browser-run acceptance
table. The final runner identity and route timings are recorded in the
repository-only evidence note
`docs/internal/evidence/5244-5253-dashboard-correctness.md`, so later
console-only proof reruns do not silently overwrite this query-shape checkpoint
or manufacture a cross-run comparison.

Performance Evidence: the retained dashboard graph contained 150,422 edges
across the catalog's 16 fixed relationship types. A direct authenticated
`POST /api/v0/relationships/catalog` against the shipped binary returned no
bytes before a 90.0017s client timeout. The 16 type-indexed counts took about
0.06s total; the seven sequential anonymous-endpoint `source_tool` groupings
took 142.39s total (17.788-27.414s each). A combined anonymous whole-graph scan
was rejected after it hit the API's 30.012s query deadline.

The proven shape starts each stamped-verb aggregate at its owning source label.
`DEPENDS_ON` intentionally uses `Repository` because its
source-tool evidence is stamped on Repository-to-Repository edges, while the
separate edge drill-down continues to browse Workload-to-Workload edges. The
seven narrowed reads took about 17.0s sequentially and 2.82s when overlapped.
The built API completed the full route in 2.4486s on its first retained-data
request and 3.1931s, 0.0618s, and 0.0032s on three following requests.
Those four sequential samples do not establish p95, and the first two exceed
the checked-in 2-second local-full-stack budget. No production distribution was
collected to prove its separate 3-second p95. The capability target is therefore
**missed**, not closed by this handler win. Open issue
[#5244](https://github.com/eshu-hq/eshu/issues/5244) owns the next bounded proof:
separate cold graph-client, handler/query, transport, API, and MCP time, then
tune the measured long pole without reducing the fixed read concurrency.

| Exactness check | Old | New | Bidirectional diff |
| --- | ---: | ---: | ---: |
| Catalog verb counts | 16 | 16 | 0 / 0 |
| Catalog total edges | 150,422 | 150,422 | 0 / 0 |
| Stamped verb/tool maps | 7 | 7 | 0 / 0 |

The handler writes each concurrent result into its original catalog index, so
response ordering is unchanged. It waits for every bounded independent read
and reports the first real error in catalog order; it does not generate an
internal cancellation that can mask the owning backend failure.

Observability Evidence: the route retains its existing handler envelope and
per-query graph telemetry, and now exposes bounded-read contention through
`eshu_dp_relationship_breakdown_permit_wait_seconds`,
`eshu_dp_relationship_breakdown_queued`, and
`eshu_dp_relationship_breakdown_in_flight`. The change adds no graph writes,
queue behavior, runtime knobs, or spans.

#### Retained Dashboard Argo CD Bootstrap Search

Performance Evidence: the console bootstrap requests the bounded Argo CD
category without a free-text or secondary structured filter. The general
NornicDB shape matched all nodes and then evaluated
`n:ArgoCDApplication OR n:ArgoCDApplicationSet`; returning the retained 365-row
universe took 5.086224s. Two direct label-anchored reads completed in a combined
0.035001s. The second excludes nodes that also carry the first label, so their
merged result contained the same 365 rows, zero duplicates, and bidirectional
set diff 0/0.

Production applies the optimization only to that exact category-only shape.
Each label read carries the same repository-access predicate and its own
`limit + 1` bound; Go performs one deterministic global merge and then applies
the response limit and truncation flag. A query, kind, provider, environment,
resource service, or resource category keeps the general search path. The
reads are sequential and bounded, so this adds no backend concurrency or
partial-result behavior.

No-Observability-Change: both label reads remain inside the existing
`query.infra_resource_search` handler span and emit their normal graph-query
duration/error telemetry. No metric, span name, graph write, queue, worker, or
runtime knob changed.

#### Retained Repository Inventory Rejected Concurrency Candidate

Performance Evidence: after the Argo CD correction, the cold console bootstrap
long pole moved to `GET /api/v0/repositories` at 2.443s. Existing
`repository_query.stage_*` telemetry attributed 1.729s to the bounded
dependency-cluster prepass; repeats were below one millisecond. The exact page
and cluster queries were then measured sequentially and concurrently against
the same retained graph before changing the handler. Cold sequential execution
took 1.294344s; after a clean graph restart, cold parallel execution took
1.299524s, with the cluster read still dominant at 1.280843s. Both shapes
returned 101 repository rows and 29 dependency edges. Five warm comparisons
kept byte-equivalent outputs and saved only 1.9-6.4ms when concurrent.

Disposition: rejected. The candidate did not recover a seconds-scale share and
would add backend contention. The handler remains sequential. A cache requires
separate proof of graph-generation invalidation, authorization boundaries,
negative-result behavior, stampede protection, bounded memory, restart
behavior, and API/MCP/frontend consistency; no cache was introduced here.

#### Retained Dashboard Incoming Relationship Story Planner Seed

Performance Evidence: on the same retained graph, the console's six-type
relationship story for one Java `Function` exceeded 35s. Per-type timing
isolated `CALLS` at 17.331s; `IMPORTS`, `REFERENCES`, `INHERITS`, `OVERRIDES`,
and `TAINT_FLOWS_TO` each completed in 0.001-0.003s. Splitting `CALLS` by
direction isolated incoming at 18.411s and outgoing at 0.002s.

The current incoming core starts from the unknown caller:
`MATCH (source)-[:CALLS]->(anchor:Function {uid: $entity_id})`. It exceeded
25.002s. Writing the identical traversal target-first so NornicDB seeds the
indexed node -- `MATCH (anchor:Function {uid: $entity_id})<-[:CALLS]-(source)`
-- completed in 0.0439s. Both returned the same eight edges, with bidirectional
row diff 0/0 and duplicate counts 0/0. The hydration projections, repository
access predicates, order, offset, limit, and type/direction merge remain
unchanged.

Built-binary proof reduced the first full six-type story from a greater-than-35s
timeout to 24.976s, but repeated full requests still took 23.695s and 23.145s.
Therefore the planner-seed fix is a measured seconds-scale improvement, not the
complete browser-budget fix. Single-type requests were all 0.001-0.021s after
warmup, leaving the multi-type target-property fallback path as the next
bounded candidate; the route must not be presented as under the browser cutoff
until that residual is separately proven.

No-Observability-Change: this is a planner-seed reorder inside the existing
bounded graph read. It adds no query fan-out, graph writes, queue behavior,
runtime knobs, metrics, or spans.

#### Retained Dashboard Relationship Story Anchor Resolution

Performance Evidence: the residual six-type delay came from repeating the
`uid`-then-legacy-`id` anchor fallback for every relationship type and
direction. An empty relationship type cannot distinguish "the `uid` anchor
exists but has no such edge" from "the anchor is legacy `id`-only", so the old
path repeatedly paid the labeled legacy lookup.

The output-preserving candidate resolves the root anchor property once and
reuses it across the six non-transitive types and both directions. A content
entity ID is the canonical graph `uid`; the resolver checks legacy `id` only
when that canonical anchor is absent. Legacy `id`-only and missing anchors are
resolved once. Transitive stories deliberately
keep per-hop fallback because later hop identities can use a different property.
Single-type stories deliberately keep the original direct `uid`-then-`id`
query path: the one-time resolver is useful only when several type/direction
reads amortize its preflight.

The one-time resolver is limited to `Function`, the only label covered by the
retained multi-type proof. Other entity labels keep the shipped per-query
`uid`-then-`id` fallback. The Function path pays one indexed `uid` lookup and,
only for a legacy graph without that canonical anchor, one indexed `id` lookup.

| Retained-data proof | Old | New | Delta |
| --- | ---: | ---: | ---: |
| Six-type graph calls | 23 | 14 | -9 |
| Six-type graph wall time | 38.692691s | 5.827756s | -32.864935s |
| Relationship rows | 1 | 1 | 0 |
| Duplicate rows | 0 | 0 | 0 |
| Bidirectional row diff | 0 / 0 | 0 / 0 | exact |

The Tier-4 no-regression proof used a retained `Function` with 466 incoming
`CALLS` edges. The unindexed legacy-`id` collision preflight alone took
5.754621 seconds, while the underlying `CALLS` and empty `TAINT_FLOWS_TO`
reads took 0.024095 and 0.002327 seconds. The corrected production dispatcher
therefore bypasses resolution for one type. Against the warmed legacy direct
path, `CALLS` completed in 0.003525 seconds versus 0.004961 seconds and the
empty type in 0.000631 seconds versus 0.001053 seconds. Both comparisons kept
ordered diff 0/0, duplicate count zero, and zero resolver preflights. The same
anchor's six-type story still improved from 32.842229 seconds and 21 calls to
5.902045 seconds and 14 calls, with 983 rows and ordered diff 0/0. This proves
the multi-type win without making the millisecond single-type path pay a
seconds-scale scan.

The intermediate 5.75-second guard was a full `Function` label scan on the legacy
`id` property. A throwaway `Function.id` index showed that this scan is
optimizable, but the candidate was rejected before production implementation:

| Retained 887-repository proof | Without index | With index | Exactness |
| --- | ---: | ---: | ---: |
| Legacy-ID collision check | 8.837783s | p50 0.000493s / p95 0.000842s | 0 / 0 |
| Full 14-call story, 983 rows | 5.902045s retained no-index helper | 0.008286-0.011195s warm | 0 / 0 |
| Fresh graph-client first story | not comparable | 4.296237s | 0 / 0 |

The first index build took 24.39 seconds on the populated graph. Preliminary
review then found that NornicDB v1.1.11 calls the property-index backfill even
when `AddPropertyIndex` reports that the index already exists, while
`PropertyIndexInsert` appends node IDs without deduplication. The exact source
paths are the pinned
[`executeCreateIndex`](https://github.com/orneryd/NornicDB/blob/v1.1.11/pkg/cypher/schema.go#L597-L646)
and
[`PropertyIndexInsert`](https://github.com/orneryd/NornicDB/blob/v1.1.11/pkg/storage/schema.go#L1874-L1898)
implementations. One retained reissue took 15.345136 seconds and left graph
node/edge counts unchanged, but it was not a safety proof; the index was removed
in 0.000539 seconds with the same 961,472 nodes and 1,180,403 edges before and
after. Two repository-narrowed replacement predicates also failed exactness on
a retained collision case. That first candidate was therefore rejected rather
than shipped on an unsafe repeated-DDL assumption.

The accepted schema delta fixes that application hazard directly. Bootstrap
first inspects NornicDB schema names and forwards only missing constraints and
indexes to strict DDL, so an existing populated index never reaches NornicDB's
non-idempotent backfill path. On the retained graph,
`nornicdb_function_legacy_id_lookup` was absent, one DDL statement applied in
16.056306s, and an immediate second application issued zero DDL statements.
The exact NornicDB schema is now 290 statements with fingerprint
`cfff663a3a7cae4e7c36823e0304b25f7f046eed2e139951e8a9bf8121b9ba69`;
the immediately preceding fingerprint remains writer-compatible.

This repaired retained proof manually reconstructs only the repeated-fallback
OLD baseline. The NEW side invokes the shipped
`relationshipStoryRelationships` production helper with all six relationship
types. The harness asserts one canonical-`uid` lookup, or two lookups for a
legacy `id`-only/missing anchor, plus exactly 12 direction/type reads using only
the resolved property. Focused production-path tests also prove that a confirmed
missing anchor skips all relationship reads and that an unrelated node whose
legacy `id` collides with a canonical `uid` cannot contribute edges.

The intermediate production-binary proof then removed the rejected index and
verified `function_legacy_id` index count zero before and after the run. On the
same retained 887-repository corpus, all 39 browser workflows passed. Code
Graph completed in 9.647 seconds with four owned HTTP responses, all status
200, zero console errors, and 7.353 seconds of margin inside the runner's
17-second liveness timeout. The API and MCP proof sidecars reported the same
source manifest; their independently built immutable image IDs are recorded in
the retained evidence note. The timeout proves
the workflow settled before the harness aborted; it is not portable performance
acceptance because the run did not record a machine resource envelope or an
`absolute_target_applicable` classification. The measured 9.647-second result
also does not meet the desired 2-3 second read SLO. Open issue
[#5244](https://github.com/eshu-hq/eshu/issues/5244) owns
cold-client, API, MCP, transport, and residual query attribution after that
boundary.

The final populated UID-first proof completed in 0.009856s with 13 graph calls
and one row. The prior collision-check shape took 4.424432s and 14 calls; the
ordered row diff was 0/0. A separate post-restart empty-edge target completed
in 0.031363s versus 4.955485s with the same 13/14-call split. A startup
warmup was separately disproven: warming one Function did not help a different
selected Function and delayed readiness, so no warmup code or startup event was
retained.

The console also stopped issuing `POST /api/v0/code/relationships` after the
story response. The bootstrap dead-code candidate already owns the selected
Function's repository/file/line metadata, while the story owns all six typed
edges, related-node source metadata, and provenance. Code Graph therefore waits
only for the story and the independent import-cycle read; tests prove one
relationship request and preserve source navigation and truth labels.

Accuracy Evidence: the built readback also exposed a separate existing NornicDB
compatibility defect: `type(rel)` was returned literally as `"type(rel)"` for
all ten rows. Each query already has a normalized allowlisted relationship
type, so the projection now returns that static value. This is an intentional
expected delta from the wrong literal to `CALLS` in this retained story; entity
IDs, directions, ordering, pagination, de-duplication, and the ten-row set are
unchanged. An ordered comparison of every relationship field except the
intentionally corrected `type` field produced a bidirectional diff of 0/0.

The same built readback exposed NornicDB returning projected
`coalesce(source.id, source.uid)` and related repo/language expressions as
literal strings. The NornicDB-only direct, class-method, and inheritance story
queries now return each property under a distinct raw alias; Go applies the
existing first-nonempty precedence and removes raw aliases before response
serialization. Exact placeholder rejection covers the direction-specific
`source.*`, `target.*`, and `anchor.*` forms without broadly rejecting arbitrary
identifiers. The repaired production-path proof measured 38.692691s and 23
calls for the repeated fallback versus 5.827756s and 14 calls for the resolved
shape, with an ordered normalized row diff of 0/0. The separate built response
had ten unique rows, nine distinct source IDs, three distinct target IDs, and no
expression-shaped identity placeholders.

No-Observability-Change: anchor resolution and the static relationship-type
projection remain inside the existing graph-query envelope. They add no graph
writes, queue behavior, runtime knobs, metrics, or spans; existing per-query
duration and error telemetry still covers every graph read.

#### Repository-Scoped Non-Function Story Anchor Resolution

Performance Evidence: issue
[#5256](https://github.com/eshu-hq/eshu/issues/5256) made Code Graph send the
selected canonical `repo_id` with its six-type relationship story. The retained
887-repository browser proof selected an authorized `Variable` content entity
that had no canonical graph anchor. The built API returned the correct resolved,
zero-relationship story, but the request took 7.602292 seconds because the
Function-only preflight did not apply: each type/direction path repeated the
same unindexed `Variable.uid` and legacy-`id` miss.

The output-preserving replacement uses the request's already-authorized,
indexed Repository identity as the preflight seed:

```cypher
MATCH (repo:Repository {id: $repo_id})
      -[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(anchor:Variable)
WHERE anchor.uid = $entity_id
RETURN true AS found
LIMIT 1
```

It checks legacy `anchor.id` only after the canonical `uid` is absent. This is
the same ownership path and repository predicate the relationship query already
requires for its anchor, so a miss can safely short-circuit every type/direction
read. Unscoped non-Function stories retain the prior per-query fallback; the
change does not introduce a broad label scan.

On the same retained data and pinned NornicDB v1.1.11 image digest
`sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`,
the old unscoped `Variable.uid` plus `Variable.id` miss took 7.839 seconds. The
repository-anchored pair took 0.011 seconds and returned the same missing anchor.
A populated Function resolved through the same ownership shape in 0.002
seconds. After rebuilding the API, the exact six-type missing-Variable request
completed in 0.084 seconds with HTTP 200, `target_resolution.status=resolved`,
authoritative-graph truth, matching target/scope repository IDs, and zero
relationships. A populated control completed in 0.009 seconds with the same
repository agreement and five relationships.

Accuracy Evidence: missing-anchor output stayed resolved with zero
relationships; the populated control retained all five scoped relationships.
Focused production-path tests cover canonical `uid`, legacy `id`, missing
anchor, unscoped non-Function fallback, and repository-bounded query shape.

No-Observability-Change: the preflight remains inside the existing graph-query
telemetry and relationship-story HTTP duration metric. It adds no route, graph
write, queue, worker, runtime knob, metric label, or span.

#### Repository-Anchored Entity Discovery And Missing-Entity Probes

The retained Code Graph bootstrap exposed another planner defect after the
relationship-story fix: repository-scoped entity resolution and exact code
search started at `MATCH (e)` and applied repository membership only after
discovering the entity. On the same retained entity, the old resolution shape
took 35.800930 seconds. Starting at the already-required indexed repository
identity and traversing `Repository-[:REPO_CONTAINS]->File-[:CONTAINS]->entity`
completed in 0.002801 seconds. Both returned one identical ordered row.

Canonical `content-entity:` identifiers now take the authoritative content-row
path before graph name resolution. A missing content row returns an explicit
empty result. The NornicDB direct-relationship compatibility path still checks
for a graph-only entity, but it uses a fixed-label `UNION` probe by canonical
`uid` and then legacy `id`, with repository scope when supplied. The retained
missing identity returned zero rows from both probes in 0.404645 and 0.188298
seconds instead of paying an unlabeled property scan. The final exact-image API
returned canonical entity resolution in 0.004492 seconds, exact repository code
search in 0.128055 seconds, the 16-verb catalog in 0.903421 seconds, and a
missing direct relationship as HTTP 404 in 0.623642 seconds. The first
direct-relationship compatibility control on a fresh API process took 5.334448
seconds; five immediate settled-stack repeats took 0.002036-0.002547 seconds
with the same empty relationship set. Code Graph no longer owns or waits for
that redundant endpoint; its story response owns the route's typed graph and
source evidence.

Accuracy Evidence: the old and new populated resolver queries produced an
ordered bidirectional row diff of 0/0. Both missing labeled probes returned
zero rows. Repository aliases are resolved to the canonical repository ID
before graph access, and scoped authorization is applied before either the
repository anchor or content-row result can be returned.

No-Observability-Change: these are read-plan and authoritative-resolution
changes inside the existing entity, code-search, relationship, graph-query,
HTTP, and truth-envelope telemetry. They add no graph writes, cache, retry,
worker, queue, metric, span name, response field, or runtime knob.

#### Retained Dashboard File Import Cycle Anchor

Performance Evidence: the live development console issues two identical
file-cycle requests because React StrictMode replays the Code Graph effect. The
effect cleanup suppresses stale state updates but does not cancel or share the
network read. Direct timing of the exact selected-repository body showed the
real backend cold penalty: 8.820754s, followed by identical warm requests in
0.000696s and 0.000440s. The old query expanded two repository import paths and
only then applied `repo.id = $repo_id`.

The retained proof candidate moved that already-required repository identity
into the first indexed match:
`MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->...`. The repaired
harness derives the legacy OLD query from the shipped NEW builder and fails
unless the two strings differ, the OLD first match is unanchored with a late
`repo.id = $repo_id` filter, and the NEW first match is repository-property
anchored with no late filter. NEW ran in 0.010009s; OLD took 16.052487s on the
same retained repository scope. Both returned the same ordered empty result,
diff 0/0. The non-empty handler fixture preserves row content, ordering,
pagination, and cycle-edge construction, while a separate test preserves the
broader discovery shape when a request has no `repo_id`. NornicDB returned no
PROFILE tree for either query, so the proof records measured wall time and the
explicit indexed query shape rather than inventing operator statistics.

The rebuilt API completed two simultaneous copies of the exact StrictMode body
in 0.180405s and 0.180382s; both returned HTTP 200, 416 bytes, and byte-identical
responses. The intermediate browser sweep then passed Code Graph in 9.122s,
down from the prior failing 16.086s route, with its live-canvas workflow passing
and zero console errors. The exact final-image acceptance result is recorded in
[Dashboard Correctness and Bounded-Read Evidence](https://github.com/eshu-hq/eshu/blob/main/docs/internal/evidence/5244-5253-dashboard-correctness.md#final-live-console-and-corpus-proof).
Since the backend fix makes the duplicated development-only reads cheap and
settled, no frontend cache or request-sharing semantics were added.

No-Observability-Change: the endpoint keeps the existing handler span, graph
read telemetry, response envelope, and request bounds. The change adds no graph
writes, queue behavior, runtime knobs, metrics, spans, or client-side caching.

### Relationships Verb Catalog And Per-Verb Edge Slice

Historical No-Regression Evidence: issue #3397 introduced the two bounded read
surfaces in `go/internal/query/relationships_catalog_cypher.go`
(`relationshipCountCypher`, `relationshipEdgesCypher`) backing
`POST /api/v0/relationships/catalog` and `POST /api/v0/relationships/edges`.
The original #3397 implementation anchored both reads on the catalog entry's
source label. That description is retained only as the historical baseline;
issue #3429 subsequently replaced the count shape after representative
NornicDB proof showed that 16 sequential source-label counts exceeded 30
seconds at approximately 900,000 edges.

The current contract deliberately uses different anchors for the two reads:

- `relationshipCountCypher` uses the anonymous typed aggregate
  `MATCH ()-[r:<VERB>]->() RETURN count(r)`. NornicDB serves this through its
  relationship-type index, so the result is the whole-graph population for the
  verb. Anonymous `()` endpoints do not bind unlabeled nodes and do not request
  an all-node scan. When more than one source label writes the same verb, the
  catalog count can legitimately exceed the companion edge-slice cardinality.
- `relationshipEdgesCypher` remains source-label-anchored with
  `MATCH (s:<SourceLabel>)-[r:<VERB>]->(t)`, orders by the indexed source
  property plus a deterministic target tie-breaker, and applies
  `LIMIT $limit`. The handler over-fetches `limit+1` rows to derive the
  truncation flag.

The verb, source label, and source property come only from the fixed
`relationshipVerbCatalog`, never from request input. The
`QP-RELATIONSHIPS-CATALOG-COUNT` gate requires `RelationshipTypeScan` and the
`QP-RELATIONSHIPS-EDGES` gate retains the `Function` source-label index evidence
for the representative `CALLS` slice. Both forbid `AllNodesScan`,
`CartesianProduct`, and `UnboundedExpand`. Catalog cost is one typed count per
fixed verb at page load; the capability matrix records a 2000 ms local p95 and
3000 ms production p95 budget for `platform_impact.relationships_catalog`.

Observability Evidence: the two handlers reuse the existing query-handler
envelope (`WriteSuccess` + `BuildTruthEnvelope` with
`TruthBasisAuthoritativeGraph`) and the shared `GraphQuery.Run`/`RunSingle`
adapters. The count and edge query shapes add no new spans, runtime knobs,
queue behavior, or graph writes. The route's bounded source-tool breakdown
fan-out now emits `eshu_dp_relationship_breakdown_permit_wait_seconds`,
`eshu_dp_relationship_breakdown_queued`, and
`eshu_dp_relationship_breakdown_in_flight`; the static query-plan gate opens no
backend session.

### Catalog Deployment-Environment Resolution Cold Plan

No-Regression Evidence: issue #3172 adds
`go/internal/queryplan/testdata/hot-cypher.yaml` and
`scripts/verify-query-plan-regression.sh` so this connected catalog path stays
registered in the graph backend query-plan fixture. The gate validates the
fixture against `graph.SchemaStatementsForBackend(graph.SchemaBackendNornicDB)`,
requires the workload/environment schema evidence names, and rejects known bad
static shapes such as unbounded traversal or cartesian-plan signatures.

No-Observability-Change: the query-plan gate is static validation only. It does
not execute graph reads or writes, open backend sessions, add metrics or spans,
change runtime knobs, or alter queue behavior.

Performance Evidence: issue #1731. The `GET /api/v0/catalog` handler resolves
per-workload deployment environments through `catalogWorkloadEvidenceEnvironmentCypher`
in `go/internal/query/catalog_workload_environments.go`. The earlier shape used
two MATCH clauses both anchored on `repo`
(`MATCH (repo:Repository)-[:DEFINES]->(w:Workload)` then
`MATCH (repo)<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(:EvidenceArtifact)-[:TARGETS_ENVIRONMENT]->(env:Environment)`).
On NornicDB that re-anchor cold-plans as a per-repository fanout.

Backend: NornicDB via local Compose Bolt-HTTP (`/db/nornic/tx/commit`). Corpus:
33 Repository, 21 Workload, 148 EvidenceArtifact, 2 Environment, 148
EVIDENCES_REPOSITORY_RELATIONSHIP edges, 55 TARGETS_ENVIRONMENT edges. Cold plan
forced with a unique leading comment per query string; result row count 53 for
both shapes.

- Before (double-MATCH re-anchor), cold: **21.33s**, 53 rows.
- After (single connected path
  `MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(:EvidenceArtifact)-[:TARGETS_ENVIRONMENT]->(env:Environment)`),
  cold: **0.005s**, 53 rows.

The other three catalog queries are unaffected and already cold-fast: workload
base 0.018s, repo 0.017s, instance 0.003s. The before/after row sets were diffed
and are byte-identical (53 ordered `(id, environment)` pairs), so deployment
environment accuracy is preserved exactly; the union/dedup stays in
`mergeCatalogEnvironments`. With the evidence query no longer dominating, the
cold catalog response drops from ~15-21s (client-timeout territory) to well
under the console's 15s budget, so the first load after an API restart populates
the Catalog and Dashboard atlas instead of timing out.

Observability Evidence: the catalog handler keeps the existing
`GraphQuery.Run` adapter, `neo4j.query` spans, and query-duration metrics for
each of its four bounded queries. The query shape changed but the per-query
telemetry surface, scope, limit, and deterministic ordering did not, so an
operator still sees per-query duration and error signals for the catalog read
path.

### Deployment Trace Config Reads

No-Regression Evidence: `go test ./internal/query -run
'TestTraceDeploymentChainKeepsConfigDerivedCloudResourcesAsUncorrelatedCandidates|TestConfigDerivedCloudResourceDependenciesRequireConfigReadEvidence|TestBuildDeploymentTraceResponseExplainsUncorrelatedCloudCandidates'
-count=1` proves explicit deployment-config matches remain visible without
being promoted into canonical cloud-resource truth.

The covered backend/version contract is the existing NornicDB/Neo4j
`GraphQuery.Run` deployment-trace read path. The input shape starts from one
resolved workload or service context and issues config-derived CloudResource
reads only for explicit `READS_CONFIG_FROM` deployment artifacts. It collects
at most 50 distinct anchors, escapes them into one regex-union query, applies
one global `LIMIT 51` sentinel, and orders the bounded result by resource name
and canonical ID. The response reports truncation when upstream artifacts or
the 50-anchor input cap omit anchors, or when the global result sentinel
saturates. The negative guard proves zero graph reads and zero rows when the
artifact relationship is not `READS_CONFIG_FROM`.

No-Observability-Change: the helper uses the existing `GraphQuery.Run` adapter,
`neo4j.query` spans, query-duration metrics, and deployment-trace response
fields. It introduces no new runtime stage, queue, worker, or telemetry surface.
The change is safe because only a materialized
`WorkloadInstance-[:USES]->CloudResource` relationship populates canonical
`cloud_resources`. Config-read matches carry
`match_basis=deployment_config_read_evidence` in bounded
`uncorrelated_cloud_resources`, together with
`missing_relationship=workload_cloud_relationship` and explicit truncation
when the probe saturates.

### Code-Edge Resolution Provenance Write Shape

The `CALLS`, `REFERENCES`, and `USES_METACLASS` edge-write templates
(`go/internal/storage/cypher/canonical_code_call_edges.go` and the label-scoped
builders) carry per-edge `resolution_method`, `confidence`, and `reason` from
[design 2222](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/2222-resolution-provenance-code-edges.md).
These were previously a hard-coded `confidence = 0.95` literal in the `SET`
clause.

No-Regression Evidence: the change is `SET`-clause only. The `UNWIND $rows`
batching, the `MATCH … MERGE (source)-[rel:…]->(target)` shape, the endpoint
label families, and the batch sizes are unchanged; the `SET` writes three
row-sourced scalar properties instead of two literals plus carried parameters.
No new `MATCH`, traversal, index lookup, or statement is added, so the query
plan shape is invariant on both NornicDB and Neo4j. The marginal cost is three
bounded scalars per row inside the already-batched `$rows` parameter.
`go test ./internal/storage/cypher -count=1` covers the parameterized templates
and the per-tier confidence derivation.

No-Observability-Change: the existing `CodeCallEdgeDuration` histogram and
`CodeCallEdgeBatches` counter, plus the `domain=code_calls relationship=… rows=…`
statement summary, expose any edge-write regression with no new metric labels or
backend branches. Provenance is carried as edge properties, not as new
instrumentation.

### Relationship Story Token Budget And Multi-Type Filter

The relationship story (`POST /api/v0/code/relationships/story`,
`go/internal/query/code_relationship_story.go`) gained two additive,
backward-compatible parameters from [issue #2232](https://github.com/eshu-hq/eshu/issues/2232):
`token_budget` (cap the response by an estimated serialized token cost) and
`relationship_types` (a multi-type filter that supersedes the singular
`relationship_type`).

No-Regression Evidence: no Cypher shape changed. `token_budget` is a pure
in-process trim applied **after** the existing count limit, iterating the
already-bounded result rows (`n ≤ limit+1 ≤ 201` per direction/type) and
estimating cost from each row's compact JSON length — `O(n)` over a bounded set,
no graph access. `relationship_types` reuses the **identical** per-type bounded
query (`MATCH (source)-[rel:TYPE]->(target) … ORDER BY … SKIP $offset LIMIT
$limit`, `code_relationship_story_graph.go`) once per requested type and merges
the rows in requested-type order; each per-type call is byte-identical to the
existing single-type query, so the query plan is invariant on both NornicDB and
Neo4j. Worst-case graph work for a request is `len(relationship_types) ×` the
existing single-type bound (the type set is capped at five and rejected for the
transitive, class-hierarchy, and override paths). When neither parameter is
supplied the response is byte-identical to the prior behavior, asserted by
`TestRelationshipStoryWithoutTokenBudgetOmitsBudgetAccounting`. No live backend
benchmark was run because this environment has no provisioned NornicDB/Neo4j
corpus; correctness rests on exact reuse of the already-shipped bounded
single-type shape plus the unchanged-default byte-equivalence test. Covered by
`go test ./internal/query ./internal/mcp -count=1`.

No-Observability-Change: the route still emits the existing
`call_graph.relationship_story` truth envelope and `neo4j.query` spans/duration
metrics from `GraphQuery.Run`. Budget and multi-type cuts are reported in-band in
the response (`summary.token_budget`, `coverage.truncated`,
`coverage.relationship_types`) with explicit `dropped`/`available_before_budget`
counts and narrowing `guidance`; no new runtime stage, queue, worker, metric, or
span is introduced.

### Relationship Story Provenance Block

[Issue #2535](https://github.com/eshu-hq/eshu/issues/2535) adds a uniform
`provenance` object to every returned relationship-story row. The block is built
from fields already present on the bounded row (`confidence`,
`resolution_method`, `confidence_basis`, `resolution_source`, `reason`, and
evidence metadata when available). It does not add a graph predicate, MATCH,
traversal, ORDER BY, or backend-specific branch.

No-Regression Evidence: `relationshipStoryRowsWithHandles` shapes the block
after confidence-floor filtering and before response serialization, over the
already-bounded result rows. The empty-result path still returns
`relationships=[]`, and a positive `min_confidence` floor still filters on the
same numeric `confidence` value before provenance is attached. Covered by
`go test ./internal/query -run
'TestHandleRelationshipStory(SurfacesRelationshipProvenanceBlock|ProvenanceSurvivesMinConfidenceAndEmptyResults)|TestOpenAPIRelationshipSchemaDocumentsProvenanceBlock'
-count=1`.

No-Observability-Change: this is response metadata only. Operators still use the
existing relationship-story truth envelope, HTTP request metrics, and graph
query spans/duration metrics; no metric, span, queue, worker, runtime knob, or
storage schema changes.

### Relationship Story Bounded Centrality Ranking

[Issue #2233](https://github.com/eshu-hq/eshu/issues/2233) ranks relationship
story rows by bounded centrality
(`go/internal/query/code_relationship_story_centrality.go`,
`relationshipStoryRankByCentrality`) before the count limit and token budget, so
the most-connected neighbors survive a small budget.

No-Regression Evidence: no Cypher changed. Centrality is the neighbor's degree
**within the already-fetched bounded result set** — a single in-process pass that
counts neighbor-id occurrences and a `sort.SliceStable`, both `O(n)` /
`O(n log n)` over `n ≤ limit+1` per direction/type (≤ ~201 rows for the common
single-type case, capped by `limit × relationship_types × directions`). No graph
access, no per-node graph degree lookup, and no whole-graph traversal is added,
so this deliberately avoids an unverified graph-degree Cypher shape on a backend
this environment cannot benchmark. The default single-type single-direction
response keeps its prior name-then-id order because every neighbor then has
degree 1 and the stable sort preserves input order (asserted by
`TestRelationshipStoryCentralityStableTieBreak`). Centrality differentiates rows
only for multi-type or both-direction results, where it is exactly the
budget-relevant signal. Covered by `go test ./internal/query -run
RelationshipStory -count=1`. A follow-up could add graph-computed global degree
once a live NornicDB/Neo4j benchmark is available.

No-Observability-Change: ranking is in-band; each row carries a `centrality`
integer and `coverage.ranked_by=bounded_centrality`. No new metric, span, stage,
or backend branch is introduced.

### Code-Call Delta Scoped Retraction

Issue #2257 scopes code-call shared-edge cleanup for delta generations to the
changed or deleted file paths emitted by the git delta fact. Full repository
refreshes still use the existing repository-wide retract path. Delta refreshes
carry a bounded, de-duplicated `delta_file_paths` list through the reducer
repo-refresh intent and into `EdgeWriter.RetractEdges`, which dispatches a
static `CALLS` / `REFERENCES` / `USES_METACLASS` delete statement anchored on
`source.path IN $file_paths` rather than deleting every code-call relationship
for the repository.

No-Regression Evidence: `go test ./internal/reducer -run
'TestBuildCodeCall(RefreshIntentsCarriesDeltaFileScope|DeltaFilePathsByRepoIDUsesRepositoryDeltaFact)|TestCodeCallProjectionRunnerRetractRepoPreservesDeltaFileScope|TestBuildCodeCallRetractRowsKeepsMalformedDeltaScoped'
-count=1` proves the reducer extracts changed/deleted file paths from the
repository delta fact, carries them into the code-call repo-refresh intent,
preserves that scope through the dedicated code-call projection runner, and does
not silently downgrade malformed delta scope to a repo-wide retract. `go test
./internal/storage/cypher -run
'TestEdgeWriterRetractEdgesCodeCall(DeltaScopesToFilePaths|RejectsDeltaWithoutFilePaths)'
-count=1` proves the storage writer switches valid delta rows to the file-path
retract statement instead of the repo-wide `source.repo_id IN $repo_ids`
statement and rejects malformed delta rows before executing Cypher. The input
cardinality is the delta file-path count for one accepted
repository/source-run unit; the normal full-refresh path is unchanged when no
delta scope is present. The changed Cypher keeps static relationship families
and source labels, binds only a positive `$file_paths` list, and relies on
existing code-entity `path` properties rather than adding a traversal or
backend-specific branch.

No-Observability-Change: the delta retract path uses the existing
`EdgeWriter.RetractEdges` executor call, statement summary, graph query duration
metrics, retry classification, timeout handling, and reducer code-call cycle
timings. It adds no worker, queue domain, runtime knob, metric instrument,
metric label, or backend-specific telemetry.

Issue #2541 added a focused statement-construction benchmark for the existing
CALLS cleanup paths: `cd go && go test ./internal/storage/cypher -run '^$' -bench BenchmarkEdgeWriterCodeCallRetractAndWrite -benchtime=1x -benchmem -count=1`.

Local evidence on Apple M4 Pro:

| Scenario | Input rows | Delta file paths | Write rows | Retract statements | Result |
| --- | ---: | ---: | ---: | ---: | ---: |
| Repo-wide full refresh | 5000 | 0 | 5000 | 1 | 3.212166 ms/op |
| Delta changed files | 5001 | 50 | 5000 | 1 | 2.926708 ms/op |
| Delta deleted-only files | 1 | 50 | 0 | 1 | 0.013750 ms/op |

This benchmark isolates Eshu-owned row shaping, retraction dispatch, and write
batching behind a no-op executor, so it does not claim backend delete latency.
It proves that deleted/no-call delta rows stay file-scoped and avoid writes.
Issue #2622 extends CALLS file-scoped cleanup to safe full-refresh acceptance
units by deriving durable parsed-file ownership before projection; unsafe or
over-cap full refreshes still use the existing repo-wide retract path.

### SQL Relationship Delta Scoped Retraction

Issue #2257 also scopes SQL relationship cleanup for delta generations to the
changed or deleted SQL source files. The SQL reducer now loads repository delta
metadata with a bounded repository fact query while preserving the existing
payload-filtered `content_entity` query for SQL entity types. Deleted-only
delta generations can therefore retract stale `READS_FROM`, `HAS_COLUMN`,
`TRIGGERS`, and `EXECUTES` edges without requiring current SQL entity rows, and
ordinary full refreshes keep the existing repository-wide retract path.

No-Regression Evidence: `go test ./internal/reducer -run
'TestSQLRelationship(MaterializationHandler(ScopesDeltaRetractToFiles|DeletedOnlyDeltaRetractsWithoutWrites)|HandlerUses(KindFilteredFactLoader|PayloadFilteredContentEntities))|TestBuildSQLRelationshipRetractRowsKeepsMalformedDeltaScoped'
-count=1` proves the reducer extracts repo-qualified delta file paths from the
repository fact, carries them into SQL retract rows, handles deleted-only delta
generations without writes, preserves bounded SQL content-entity loading, and
does not silently downgrade malformed delta scope to repo-wide cleanup. `go test
./internal/storage/cypher -run
'TestEdgeWriterRetractEdgesSQLRelationship(DeltaScopesToFilePaths|RejectsDeltaWithoutFilePaths|Dispatch|RunsPerLabelStatementsSequentially)|TestSQLRelationshipRetractCoversEveryWrite(EndpointLabel|RelationshipType)|TestSQLRelationshipRetractStatementsUseSingleSourceLabel|TestBuildRetractSQLRelationshipEdgeStatementsUsesSharedParameters'
-count=1` proves valid delta rows dispatch one file-scoped retract statement per
source label (anchored inline on `{path: file_path}`), malformed delta rows
execute no Cypher, and non-delta SQL retracts dispatch the same per-label
statements sequentially for every executor. The input cardinality is the delta
file-path count for one repository generation; the changed Cypher keeps static
source labels and relationship tokens, binds only a positive `$file_paths`
list, and does not add a traversal or backend-specific branch.

No-Observability-Change: SQL delta retraction uses the existing
`EdgeWriter.RetractEdges` executor path, SQL materialization completion log
fields, graph query duration metrics, retry classification, and timeout
handling. It adds no worker, queue domain, runtime knob, metric instrument,
metric label, or backend-specific telemetry.

### Inheritance Delta Scoped Retraction

Issue #2257 also scopes inheritance cleanup for delta generations to changed or
deleted source files. The inheritance reducer now loads repository delta
metadata beside its existing payload-filtered `content_entity` query for
inheritance-capable entity types. Deleted-only delta generations can therefore
retract stale `INHERITS`, `OVERRIDES`, `ALIASES`, and `IMPLEMENTS` edges without
requiring current child entities, while full refreshes keep the existing
repository-wide retract path.

No-Regression Evidence: `go test ./internal/reducer -run
'TestInheritance(MaterializationHandler(ScopesDeltaRetractToFiles|DeletedOnlyDeltaRetractsWithoutWrites)|MaterializationHandlerUsesKindFilteredFactLoader|MaterializationHandlerUsesPayloadFilteredContentEntities)|TestBuildInheritanceRetractRowsKeepsMalformedDeltaScoped'
-count=1` proves the reducer extracts repo-qualified delta file paths from the
repository fact, carries them into inheritance retract rows, handles
deleted-only delta generations without writes, preserves bounded content-entity
loading, and keeps malformed delta scope scoped instead of silently downgrading
to repo-wide cleanup. `go test ./internal/storage/cypher -run
'TestEdgeWriterRetractEdgesInheritance(DeltaUsesFileScope|RejectsDeltaWithoutFilePaths|Dispatch)|TestEdgeWriterRetractEdgesInheritanceIncludesOverrides|TestBuildRetractInheritanceEdgeStatementsByFilePath'
-count=1` proves valid delta rows dispatch the file-scoped inheritance retract
statement with `child.path IN $file_paths`, malformed delta rows execute no
Cypher, and non-delta inheritance retracts keep the existing repo-wide dispatch.
The input cardinality is the delta file-path count for one repository
generation; the changed Cypher keeps a static relationship-token set, binds only
a positive `$file_paths` list, and does not add a traversal or backend-specific
branch.

No-Observability-Change: inheritance delta retraction uses the existing
`EdgeWriter.RetractEdges` executor path, inheritance materialization completion
logs, graph query duration metrics, retry classification, and timeout handling.
It adds no worker, queue domain, runtime knob, metric instrument, metric label,
or backend-specific telemetry.

### Rationale EXPLAINS Delta Scoped Retraction

Issue #2257 also scopes rationale `EXPLAINS` cleanup for delta generations to
changed or deleted source files. The rationale reducer now loads repository
delta metadata beside `content_entity` facts that can carry
`rationale_comments`. Deleted-only delta generations can therefore retract stale
`EXPLAINS` edges without current rationale rows, while full refreshes keep the
existing repository-wide `rationale.repo_id` retract path.

No-Regression Evidence: `go test ./internal/reducer -run
'TestRationaleMaterializationHandler(ScopesDeltaRetractToFiles|DeletedOnlyDeltaRetractsWithoutWrites)|TestBuildRationaleRetractRowsKeepsMalformedDeltaScoped|TestLoadRationaleMaterializationFactsUsesSingleLegacyFallback'
-count=1` proves the reducer extracts repo-qualified delta file paths from the
repository fact, carries them into rationale retract rows, handles deleted-only
delta generations without writes, preserves one legacy fallback fact load, and
keeps malformed delta scope scoped instead of silently downgrading to repo-wide
cleanup. `go test ./internal/storage/cypher -run
'Test(BuildRetractRationaleEdgeStatementsByFilePath|RationaleRetractCoversEveryWriteTargetLabel|EdgeWriterRetractEdgesRationale(DeltaRunsPerLabelStatementsSequentially|RejectsDeltaWithoutFilePaths))'
-count=1` proves valid delta rows dispatch one file-scoped rationale retract
statement per target label with `target.path IN $file_paths` (a single
target-label disjunction matches zero rows on NornicDB v1.1.11), malformed
delta rows execute no Cypher, and non-delta rationale retracts keep the
existing repo-wide dispatch.
The input cardinality is the delta file-path count for one repository
generation; the changed Cypher keeps static target labels and the `EXPLAINS`
relationship token, binds only a positive `$file_paths` list, and does not add
a traversal or backend-specific branch.

No-Observability-Change: rationale delta retraction uses the existing
`EdgeWriter.RetractEdges` executor path, rationale materialization completion
logs, graph query duration metrics, retry classification, and timeout handling.
It adds no worker, queue domain, runtime knob, metric instrument, metric label,
or backend-specific telemetry.

### Documentation DOCUMENTS Delta Scoped Retraction

Issue #2321 scopes documentation `DOCUMENTS` cleanup for delta generations by
documentation identity instead of raw repository path. The reducer maps changed
and deleted git documentation paths to stable `doc:git:<repo_id>:<path>`
document ids. Storage also supports section-uid scoped retraction for future
sources that emit explicit section deltas, but repository path deltas are
file-granular and therefore use document-id cleanup so stale edges from deleted
sections do not survive a changed file. External documentation sources such as
Confluence are not matched by repository path metadata, so a repo delta cannot
retract unrelated external documentation edges.

No-Regression Evidence: `go test ./internal/reducer -run
'TestDocumentationMaterializationHandler(ScopesDeltaRetractToDocuments|DeletedOnlyDeltaRetractsWithoutWrites)|TestBuildDocumentation(RetractRowsKeepsMalformedDeltaScoped|DeltaScopeIgnoresExternalDocumentPathMetadata)'
-count=1` proves the reducer extracts repo-qualified git documentation paths,
uses document identity for changed and deleted docs, ignores external docs with
path-like metadata, handles deleted-only delta generations without writes, and
keeps malformed delta scope scoped instead of silently downgrading to scope-wide
cleanup. `go test ./internal/storage/cypher -run
'Test(BuildRetractDocumentationEdgesBy(DocumentID|SectionUID)|EdgeWriterRetractEdgesDocumentation(DeltaUses(Document|Section)Scope|RejectsDeltaWithoutIdentity))'
-count=1` proves valid delta rows dispatch document-id or section-uid scoped
`DOCUMENTS` retract statements, malformed delta rows execute no Cypher, and
non-delta documentation retracts keep the existing scope-wide dispatch. The
input cardinality is bounded by the delta document path count; the changed
Cypher keeps static `DocumentationSection` and `DOCUMENTS` tokens, binds only
positive identity lists, and does not add a traversal or backend-specific
branch.

No-Observability-Change: documentation delta retraction uses the existing
`EdgeWriter.RetractEdges` executor path, documentation materialization
completion logs, graph query duration metrics, retry classification, and timeout
handling. It adds no worker, queue domain, runtime knob, metric instrument,
metric label, or backend-specific telemetry.

### Uncorrelated CloudResource Candidate Scan

Issue #3378: `GET /api/v0/services/{service_name}/story` hung past the 60s curl
budget at 900-repo scale (481,728 graph nodes). The service-story dossier and
service context share `enrichServiceQueryContextWithOptions`, which calls
`loadUncorrelatedCloudResourceCandidates` whenever a service has no materialized
`cloud_resources` (the common case at scale). The prior Cypher anchored on an
unlabeled node and filtered the label in `WHERE`:

```cypher
MATCH (n)
WHERE (n:CloudResource)
  AND (coalesce(n.name,'') CONTAINS $query OR ... 11 more predicates ...)
ORDER BY n.name
LIMIT $limit
```

On NornicDB an unlabeled `MATCH (n)` does not push the label down to a label
scan, so the planner scanned all 481,728 nodes, evaluated a 12-way
`CONTAINS`/`=` predicate per node, and sorted the full matched set by name
before `LIMIT` applied. That whole-graph scan is the dossier hang.

The fix anchors the label in the MATCH pattern so the scan is bounded to the
CloudResource label population, which is the repo-standard shape the static
query-plan gate (`go/internal/queryplan/testdata/hot-cypher.yaml`) enforces by
rejecting unlabeled anchors:

```cypher
MATCH (n:CloudResource)
WHERE (coalesce(n.name,'') CONTAINS $query OR ... )
ORDER BY n.name, n.id
LIMIT $limit
```

The predicates, projection, and bound are unchanged. The canonical `id`
tie-breaker makes the capped subset deterministic when resources in different
accounts or regions share a name; the uncapped matching set is unchanged.
The query now over-fetches one row beyond the service-story item limit (50) so
the handler can set `uncorrelated_cloud_resources_truncated` when the backend
held more matches than the bound, instead of silently capping.

No-Regression Evidence: this is a correctness-of-shape fix that strictly removes
the all-node scan; no live PROFILE was available because no local NornicDB-New
checkout is present (stated per cypher-query-rigor). The shared shape is proven
by `go test ./internal/query -run
'CloudResource|ServiceStory|Story|EnrichServiceQueryContext|TraceDeployment'
-count=1` (216 tests) plus the full `go test ./internal/query -count=1` (3094
tests) and `scripts/verify-query-plan-regression.sh`. Input cardinality at the
anchor drops from all graph nodes (481,728) to the CloudResource label
population; output is bounded by the service-story item limit (50) plus the
single over-fetch row.

Observability Evidence: the `uncorrelated_cloud_resource_candidates` stage timer
in `enrichServiceQueryContextWithOptions` keeps its `row_count` field and now
also emits a `truncated` boolean, so an operator can see whether the bound was
hit. The query keeps the existing `GraphQuery.Run` adapter, `neo4j.query` spans,
and query-duration metrics; no new worker, queue, or runtime knob is introduced.

### Legacy Change-Surface Service-Kind Traversal

Issue #3384: `POST /api/v0/impact/change-surface` with
`{"kind":"service","target":"<id>"}` hung past the 40s budget at 900-repo scale
(481,728 graph nodes). Repository, module, XRD, and SQL-table targets were fine;
only the densely connected service (Workload) kind hung. The legacy
`findChangeSurface` Cypher had the same two anti-patterns the static query-plan
gate rejects:

```cypher
MATCH (start) WHERE start.id = $target_id
OPTIONAL MATCH path = (start)-[rels*1..8]->(impacted)
WHERE impacted.id <> $target_id AND any(label IN labels(impacted) WHERE ...)
UNWIND relationships(path) as rel
...
```

1. The `MATCH (start)` anchor is unlabeled, so NornicDB resolves the id by an
   all-node scan over the entire graph (the same class as issue #3378). A second
   unlabeled `MATCH (n) WHERE n.id = $id` ran to hydrate the target name.
2. The variable-length `*1..8` expansion is hardcoded and the target-label set is
   filtered only after expansion. For a dense Workload node the engine
   materializes the full 8-hop neighborhood frontier before any label filter
   applies, which is the service-kind explosion.

The fix resolves the start node through the existing label-anchored, indexed
resolver probes (`resolveChangeSurfaceTarget`) — driven by the optional
`kind`/`target_type` hint, with an ordered label fallback when it is absent — and
anchors the resolved label in the traversal start
(`changeSurfaceTraversalStartMatch`, e.g. `MATCH (start:Workload {id: $target_id})`,
which uses the `Workload.id` uniqueness constraint and `nornicdb_workload_id_lookup`
index). The traversal depth is parameterized and clamped (default 4, max 8). The
unlabeled target-hydration scan is removed entirely; the target name comes from
the resolved candidate. The legacy per-relationship projection
(`rel_type`/`confidence`/`reason`) and the flat `impacted`/`count`/`limit`/`truncated`
response shape are preserved, so edge provenance and the wire contract are
unchanged for existing callers (`kind` and `max_depth` are additive optional
fields). The query over-fetches one row beyond limit so `truncated` stays honest.

No-Regression Evidence: this is a correctness-of-shape fix that removes the
all-node start scan and bounds the traversal depth; no live PROFILE was available
because no local NornicDB-New checkout is present (stated per cypher-query-rigor).
Input cardinality at the start anchor drops from all graph nodes (481,728) to a
single indexed point lookup, and the traversal frontier is bounded by `max_depth`
(default 4) instead of a hardcoded 8. Output is bounded by `limit` plus one
over-fetch row. Shape is proven by `go test ./internal/query -run ChangeSurface
-count=1` (18 tests, including the labeled-anchor and depth-clamp regressions) and
the full `go test ./internal/query -count=1` (3105 tests).

Observability Evidence: No-Observability-Change. The handler keeps the existing
`GraphQuery.Run` adapter, `neo4j.query` spans, and query-duration metrics; the
`truncated` flag in the response already signals when the bound was hit. No new
worker, queue, span, or runtime knob is introduced.

### Entity-Map Neighborhood Two-MATCH Re-Anchor

Performance Evidence: issue #3549. `POST /api/v0/impact/entity-map` for a service
(Workload) node did not return within the console's 15s topology budget, so the
Dashboard 'Code-to-cloud topology' rendered empty (1 node, 0 edges) with all
category counts at 0 and 'Relationship atlas unavailable ... timed out after
15000ms'. Every other dashboard call returned <2s; entity-map was the sole slow
path. The console sends `{from: <name>, depth: 2}`
(`apps/console/src/api/eshuGraph.ts`).

Backend: NornicDB via local Compose Bolt (`bolt://localhost:7687`, db `nornic`).
Corpus: ~951 Workloads, ~29.1k typed edges, ~19.8k nodes (warm).

Root cause — the bounded neighborhood traversal in
`go/internal/query/entity_map_traversal.go` split the indexed anchor and the
expansion across two MATCH clauses:

```cypher
MATCH (start:Workload {id: $from_id})
MATCH (start)-[rel:DEPENDS_ON|USES|...]->(entity)
WHERE ...
RETURN ... ORDER BY name, id LIMIT $limit
```

On NornicDB a second MATCH that re-references a node bound in a prior MATCH is
re-planned independently of the resolved anchor: instead of expanding from the
single indexed `start` node, the planner scans the relationship-family
population and filters, so the cost scales with whole-graph edge volume rather
than node degree. This is the same class as the issue #3172 double-MATCH
cold-plan re-anchor, but it fired on every entity-map traversal (direct depth-1
and the depth-2 variable-length spec), not just cold.

The fix collapses anchor and expansion into one connected MATCH pattern so the
planner uses the `Workload.id` (or resolved label/property) index to anchor and
expands only from that node:

```cypher
MATCH (start:Workload {id: $from_id})-[rel:DEPENDS_ON|USES|...]->(entity)
WHERE ... RETURN ... ORDER BY name, id LIMIT $limit
```

Live backend isolation via the read-only Cypher path (same NornicDB):

- `MATCH (s:Workload {id:$id}) MATCH (s)-[r:DEPENDS_ON]->(e) RETURN count(*)` —
  upstream request timeout (does not return).
- `MATCH (s:Workload {id:$id})-[r:DEPENDS_ON]->(e) RETURN count(*)` — instant.
- `MATCH (s:Workload {id:$id})-[r:DEPENDS_ON|USES|...8 verbs]->(e) RETURN count(*)`
  (full outgoing family) — instant.
- Connected variable-length form
  `MATCH path = (s:Workload {id:$id})<-[rels:...*2..2]-(e) ... LIMIT 26` —
  instant, returns correct 2-hop paths.

Live endpoint before/after on the same NornicDB, console payload
`{"from":"files","depth":2}` / `{"from":"files","depth":1}` (the fixed API
binary built from this branch was run against the running Compose NornicDB and
Postgres on a spare port `:8099`; the unchanged Compose build on `:8080` served
the before). `files` resolves to `Workload {id: workload:files}`, a high-degree
service node:

| Endpoint | Before (two MATCH) | After (connected MATCH) |
| --- | --- | --- |
| `POST /api/v0/impact/entity-map` (service, depth 2) | HTTP 000, >30s curl timeout (never returns; well past the 15s console budget) | HTTP 200, 0.48s, 25 relationships (`query_shape: typed_entity_map_bounded_relationship_family`, `truncated: true`) |
| `POST /api/v0/impact/entity-map` (service, depth 1) | HTTP 000, >30s curl timeout | HTTP 200, 0.03s, 2 relationships (`query_shape: typed_entity_map_relationship_family`) |

The result set is preserved: same relationship families, same direction specs,
same WHERE filters, same `ORDER BY name, id`, same `LIMIT $limit` over-fetch,
same Go dedupe/sort. Only the anchor binding moved into the expansion pattern, so
the rendered neighborhood is byte-for-byte the same truth, now bounded by node
degree instead of whole-graph edge volume. Covered by `go test ./internal/query
-run EntityMap -count=1` (12 tests), including the new
`TestEntityMapTraversalAnchorsExpansionInSingleConnectedMatch` regression that
fails if either traversal builder ever re-introduces a second standalone MATCH.

No-Observability-Change: the handler keeps the existing `GraphQuery.Run`
adapter, the `query.entity_map` span with `eshu.entity_map.traversal_seconds`,
`result_count`, and `truncated` attributes, and the query-duration metrics. The
`truncated` flag already signals when the bound was hit. No new worker, queue,
metric, span, or runtime knob is introduced; the query shape changed but the
per-query telemetry surface did not.

### MCP Dispatch Response-Size Budget

Issue #3498 (performance bar) adds a tool-agnostic response-size budget at the
MCP dispatch boundary (`go/internal/mcp/dispatch_budget.go`). Every MCP tool
response is serialized and dispatched through `dispatchTool`, so an honestly
bounded graph read can still produce an arbitrarily large *response* (a wide
relationship story, a deep visualization packet, a large `execute_cypher_query`
subgraph) that serializes straight into the model context window and blows the
repo-scale performance contract. Per-route token budgets (for example the
relationship-story `token_budget`) bound their own rows, but nothing bounded the
aggregate tool response.

`applyResponseBudget` measures the serialized envelope/value size and, when it
exceeds `defaultToolResponseByteBudget` (256 KiB, ~64k tokens at the repo's
~4-bytes-per-token heuristic), replaces the oversized payload with a small
bounded error envelope (`error.code=mcp_response_over_budget`) carrying
`response_bytes`, `budget_bytes`, `estimated_tokens`, the tool name, and
narrowing guidance. It is the response-size sibling of the dispatch deadline
guard (#2469) and runs after per-route budgets.

No-Regression Evidence: the budget is a pure post-dispatch in-process size check
over the already-serialized response; it adds no graph, storage, queue, or HTTP
round trip and does not change any Cypher shape. The before state is an unbounded
response body returned verbatim (`dispatch.go` read `rec.Body.Bytes()` with no
size cap); the after state caps it at 256 KiB and substitutes a bounded
refusal. Input shape: any tool response routed through `dispatchToolWithOptions`.
`go test ./internal/mcp -run 'TestDispatchToolResponse|TestDispatchToolZeroBudget|TestDefaultDispatchAppliesResponseBudget' -count=1`
covers over-budget replacement, within-budget pass-through, disabled-budget
(`budget<=0`), and default-entrypoint enforcement; `go test ./internal/query
./internal/mcp ./cmd/api ./cmd/mcp-server -count=1` (3929 tests) stays green,
proving the 256 KiB budget sits above every honestly bounded read fixture so no
legitimate response is refused. No live NornicDB/Neo4j benchmark is load-bearing
because the change is an in-process byte check, not a Cypher change.

Observability Evidence: every budget hit emits the structured log
`mcp tool response over budget` with `tool`, `response_bytes`, and `budget_bytes`
fields (3 AM operable), mirroring the dispatch-deadline log precedent, and the
budget accounting is returned in-band in `error.details`. The `internal/mcp`
package declares no metric instruments by design, consistent with its existing
dispatch observability surface.

### CloudResource / Security-Group Retract Source-Anchoring (#4836/#4858/#4881)

The reducer-owned CloudResource (AWS/Azure/GCP/observability) and security-group
reachability edge retracts previously matched by relationship property alone
(`MATCH (source:CloudResource)-[rel]->(:CloudResource) WHERE rel.scope_id IN
$scope_ids AND rel.evidence_source = $evidence_source DELETE rel`), which
NornicDB executes as an O(total CloudResource store) label-anchored scan — the
15-47s/repo warm-reingest cost epic #4836 reported. The change anchors the
retract on the prior-generation source uids recorded in the `projected_source_edge`
ledger: `MATCH (source:CloudResource)-[rel]->() WHERE source.uid IN $source_uids
AND rel.scope_id IN $scope_ids AND rel.evidence_source = $evidence_source DELETE
rel` (security-group anchors two families on `sg.uid` / `rule.uid`). This is
O(scope source count), not O(total store).

Performance Evidence: the speedup depends on NornicDB's IN-list start-node index
seed (orneryd/NornicDB#258/#259), so all numbers below are measured on the
fix-branch backend, not on the Compose default. On the built fix-branch binary,
`BenchmarkInListAnchoredRelMatch` (50k-node label, 100-uid sublist) goes
109,930,838 -> 301,410 ns/op (~365x), 83MB -> 228KB, 1.39M -> 2.8k allocs.
Real query path (HTTP, fix-branch binary, 20k-CloudResource store,
`NORNICDB_ASYNC_WRITES_ENABLED=false`): the node-only `WHERE source.uid IN $u`
seed is flat ~0.10s at N=100/500/5000; the full anchored retract is 0.26s (N=100)
and 0.70s (N=500) versus the OLD label scan at ~7s and growing with the whole
store.

Backend / version: the measured win needs the IN-list seed merged by
orneryd/NornicDB#259, which also fixes the multi-MATCH relationship-binding
correctness bug #257. The temporary default Compose source commit `149245...`
is four commits ahead of that merge and includes it. This note still records
the original fix-branch measurement; replacing the temporary source pin with a
released image requires an immutable digest and renewed representative proof.

Input shape: warm re-ingest of a scope that already has a prior generation (cold
ingest still skips the retract on first projection). Leak-safe by construction:
the ledger is recorded before the graph write (a superset of graph edges), so
`ListSourceUIDsForScopes` returns the prior generation's full source set even for
a source removed this generation; the one-time startup backfiller seeds the
ledger from existing edges so the first post-deploy retract is not a no-op.

No-Regression Evidence: row-set equivalence holds — the anchored retract deletes
the identical edge set as the whole-scope retract, because every edge carrying a
writer's `evidence_source` is reachable from one of that writer's recorded source
uids by construction; the leak-safety regression tests assert it (gen N records
{A,B}; gen N+1 drops B; the retract still anchors on {A,B}).

Real warm-reingest gate (built binary, real AWS account, patched NornicDB
`nornicdb-relseed`, `NORNICDB_ASYNC_WRITES_ENABLED=false`): a live EC2 scan
emitted 974 `aws_relationship` facts; a second scan of the same scope produced
the warm re-ingest. On the second generation the retract fires
(`skip_retract=false`): `aws_relationship_materialization` retracts in 0.676s and
`security_group_reachability_materialization` in 0.250s. Row-set equivalence
holds in the graph — after gen 2, the aws-relationship edges are 956 for the new
generation and 0 for the prior generation (fully retracted, no leak, no
over-delete); security-group edges are 382 (191 rule + 191 endpoint) for the new
generation and 0 prior. The `projected_source_edge` ledger prunes the prior
generation and records only the new one, and all reducer/projector work items
finish terminal-clean (0 failed / 0 pending / 0 dead-letter).

No-Observability-Change: the retract keeps its existing statement metadata
(phase/entity/summary) and the reducer materialization spans/metrics
(`eshu_dp_reducer_executions_total`, `eshu_dp_reducer_run_duration_seconds`, and
`eshu_dp_postgres_query_duration_seconds` for the ledger reads/writes); no metric
or span is added or removed. The ledger and backfill stage files are covered in
`docs/public/observability/telemetry-coverage.md`.

### Orphan Sweep Anti-Join Connectivity-Read Chunking (#5147)

The #5147 anti-join redesign of `OrphanSweepStore` (see [NornicDB
Pitfalls](nornicdb-pitfalls.md) for why the sweep cannot use `NOT (n)--()`,
`(n)--()`, or `COUNT { (n)--() }`) replaced the relationship-existence
predicate with a Go-side anti-join between an S1 candidate read and an S2
connected-keys read (`UNWIND $keys AS candidate_key MATCH (n:Label {key:
candidate_key})-[r]-(m) RETURN DISTINCT n.key`). That S2 read's own
per-statement cost scales super-linearly with the size of the `$keys` list on
both pinned NornicDB backends (v1.1.11 and PR261/compose-default), independent
of the anti-join's correctness, which was proven separately.

Measured (throwaway shim, deleted after recording; 5,000-node populated `File`
label, 4,000 connected + 1,000 orphan, `17688`): the UNWIND-anchored form goes
200 keys 14ms -> 1,000 keys 197ms -> 2,000 keys 815ms -> 4,000 keys 3.1s ->
5,000 keys 4.7s (~0.07ms/key at 200, ~0.95ms/key at 5,000 -- roughly
quadratic, exponent ~1.8-2.0 between adjacent scale points). This is bounded
today by `OrphanSweepPolicy.CountLimit`
(`ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT`, default 10,000) and runs at most once
per label per hourly cycle, so it is correctness-safe, but a heavily
populated label could cost 10-20s/cycle at the default `CountLimit`.

Two alternative shapes were measured on the same populated label and
rejected:

- **Bounded IN-list, no UNWIND** (`MATCH (n:Label)-[r]-(m) WHERE n.key IN
  $keys RETURN DISTINCT n.key`): *worse*, not better -- 200 keys 16ms, 5,000
  keys 7.2s (slower than the UNWIND form at every scale point). Rejected.
- **Unbounded full-label scan** (`MATCH (n:Label)-[r]-(m) WHERE
  <evidence_predicate> RETURN DISTINCT n.key`, no key anchor at all): fast at
  the 5,000-node and 15,000-node scale (27ms and 200ms respectively) and
  proven correct (identical connected-key set to the anchored form), but it
  is **not bounded by `CountLimit`** -- its cost is proportional to total
  label population matching the evidence predicate, not to the S1 candidate
  count. `evidence_source` has no index (see [NornicDB
  Tuning](nornicdb-tuning.md)), and a follow-up shim that grew a *different*
  synthetic population toward 20,000-40,000 nodes hit a server-side "Txn is
  too big to fit into one request" failure and >2-minute unindexed-scan
  timeouts on unrelated large single-statement operations against the same
  backend -- direct evidence that this backend's cost for unindexed
  full-label operations does not stay flat at larger populations, and that
  removing `CountLimit`'s bound on the S2 read would trade one scaling
  cliff (UNWIND, bounded) for a potentially worse one (full-label scan,
  unbounded) on exactly the years-old-backlog deployment this fix targets.
  Rejected for that boundedness risk, not for a correctness defect.

**Adopted: chunk the existing anchored UNWIND form into fixed-size round
trips** (`defaultOrphanSweepConnectedKeysChunkSize = 500` in
`go/internal/storage/cypher/orphan_sweep.go`). This keeps the same bounded,
CountLimit-relative, key-anchored read shape -- no relationship-existence
predicate, no unbounded scan -- and only changes how many keys one round trip
anchors on. Measured on the real `(*OrphanSweepStore).readConnectedKeys`
production method (not a hand-rolled loop), same 5,000-key/4,000-connected
scenario, both backends: **4.7s (unchunked) -> ~0.60-0.61s (chunked at 500)**,
identical connected-key result (4,000/4,000). A manual sweep of chunk sizes on
the same data showed 500 (10 round trips, ~572-610ms total) beating 1,000 (5
round trips, ~950-964ms total) -- more, smaller round trips outperform fewer,
larger ones here, consistent with the underlying per-statement cost being
super-linear in list length.

No-Regression Evidence: `go test ./internal/storage/cypher -run
'TestReadConnectedKeys' -race -count=1` proves chunking is transparent below
`defaultOrphanSweepConnectedKeysChunkSize` (exactly one round trip, matching
pre-#5147-finding-2 behavior) and correctly unions/dedupes results across
chunks above it, including a mid-chunk reader-error propagation case.
`TestLiveOrphanAntiJoinReplacesBrokenNotDashDashPredicate` passes unchanged on
both `17688` and `17689` after the chunking change, proving the chunked read
still discriminates orphan vs. connected correctly at the discriminating-fixture
scale that matters for correctness.

No-Observability-Change: `readConnectedKeys` keeps the same `Reader.Run`
seam, same `Operation: OperationCanonicalRetract` statement tagging per round
trip, and the same public `SweepOrphanNodes`/`GraphOrphanNodeCounts` result
shape; chunking is an internal round-trip-count change, not a new metric,
span, log field, or config surface.

### SQL Table Blast-Radius Branch Reduction (#5330)

`blastRadiusSqlTableCypher` (`go/internal/query/impact_blast_radius.go`)
dropped from six `CALL {...UNION...}` branches to five: the never-written
`MIGRATES` and `MAPS_TO_TABLE` branches were removed outright, and the
combined `EXISTS { MATCH (sql_node)-[:READS_FROM|TRIGGERS_ON|INDEXES]->(table)
}` subquery branch was replaced with a direct typed `MATCH
(:SqlTrigger)-[:TRIGGERS]->(table)` branch (the reducer only ever emits
`TRIGGERS`, never the `TRIGGERS_ON` name the old branch checked) plus a direct
typed `MATCH (:SqlIndex)-[:INDEXES]->(table)` branch (INDEXES is now wired,
prior commits this PR). `blastRadiusSqlTableBranches` tracks the branch count
exactly and gates the caller's `limit * blastRadiusSqlTableBranches`
over-fetch before the app-side per-repo min-hop dedup, so 6 -> 5 also lowers
the over-fetch multiplier and the row volume Go merges per call.

This is primarily a correctness fix (#5330's honest-coverage rationale is
above in `impact_blast_radius.go`), not a claimed speedup, so the required
proof here is the same-shape no-regression check called for by
`cypher-query-rigor`: prove NEW is not slower than OLD on the same data.

No-Regression Evidence: theory shim (throwaway `_test.go` in
`go/internal/replay/offlinetier`, deleted after capture) against an isolated
NornicDB (build `eshu-nornicdb-pr261:149245885258`, Bolt driver, database
`nornic`, schema bootstrapped first). Seeded 200 `Repository`/`File`/`SqlTable`
triples (all named `target_table`, so one `CONTAINS` query name-matches every
row) plus 50 each of `QUERIES_TABLE`, `REFERENCES_TABLE`, `TRIGGERS`, and
`INDEXES` edges into the matched table, using the same
`UNWIND $rows AS row CREATE ...` write shape the production canonical writers
use (a multi-clause `WITH`/`CREATE` chain silently returns wrong results on
this pinned NornicDB build, so the seed avoided that shape — see [NornicDB
Pitfalls](nornicdb-pitfalls.md)). Ran the exact pre-#5330 OLD query text
(`limit=51*6=306`) and the current NEW query text (`limit=51*5=255`) through
the same `Run` adapter production uses (not the HTTP transactional endpoint —
`docker exec`-wrapped HTTP calls floor out around 30ms and cannot resolve
sub-millisecond differences), 20 reps each after a warmup rep, repeated across
4 independent test runs:

| Run | OLD (6 branches, ms/op) | NEW (5 branches, ms/op) | Delta |
| --- | ---: | ---: | ---: |
| 1 | 0.822 | 0.677 | -17.6% |
| 2 | 0.854 | 0.708 | -17.1% |
| 3 | 0.940 | 0.711 | -24.4% |
| 4 | 0.957 | 0.667 | -30.3% |

NEW was faster than OLD in every run (never slower), consistent with the
lower branch count and lower over-fetch multiplier. Row counts matched the
seeded data on both shapes (OLD 300 rows including the `INDEXES` edges it
picks up incidentally through its `READS_FROM|TRIGGERS_ON|INDEXES` EXISTS
disjunction; NEW 255 rows through its five direct branches), confirming
neither shape errored or silently truncated on this dataset. This is a
same-shape performance check only — OLD and NEW are intentionally NOT
asserted result-set-equivalent, because dropping the dead `MIGRATES`/
`MAPS_TO_TABLE` branches and renaming `TRIGGERS_ON` to `TRIGGERS` is the
correctness fix under test (an accuracy delta, not an optimization); that
delta is proven by `TestBlastRadiusSqlTableCypherDropsDeadBranchesKeepsLiveOnes`
and `go/internal/query/impact_blast_radius_coverage_test.go`, not by this
shim.

No-Observability-Change: the query still runs through the existing
`h.Neo4j.Run` adapter and per-query graph telemetry; no new span, metric,
runtime knob, or queue behavior was added. The response gained the `complete`/
`coverage` fields (documented separately in
[HTTP API — IaC, content, and infra routes](http-api/iac-content-infra.md)),
which are computed in Go from the existing `EdgeMaterializationCoverage`
registry and add no graph read.

### SQL Table Blast-Radius READS_FROM Branch Addition (#5345)

`blastRadiusSqlTableCypher` grew from five `CALL {...UNION...}` branches back
to six: the dead `(:SqlTable)-[:REFERENCES_TABLE]->(table)` branch (never had
a writer — the parser never stamped `source_tables` onto the SqlView/
SqlFunction entity, so the reducer's derivation had nothing to key on) was
replaced with two endpoint-label-constrained branches,
`(:SqlView)-[:READS_FROM]->(table)` and `(:SqlFunction)-[:READS_FROM]->
(table)` — two branches, not one, because NornicDB matches zero rows on a
node-label disjunction (#5116), so `(:SqlView|SqlFunction)` cannot cover both
source labels in a single `MATCH`. `blastRadiusSqlTableBranches` moved 5->6 to
track the new branch count.

This is a correctness fix (the SqlView/SqlFunction read edge was silently
returning zero via `REFERENCES_TABLE`, which no writer ever produced), not a
claimed speedup, so per `cypher-query-rigor`'s "pure correctness fixes can
trade a full bench for a no-measurable-regression check" allowance, the
required proof here is same-shape no-regression, not a full before/after
optimization write-up.

No-Regression Evidence: theory shim (throwaway `_test.go` in
`go/internal/replay/offlinetier`, deleted after capture) against an isolated
NornicDB (same pinned build as the #5330 evidence above,
`eshu-nornicdb-pr261:149245885258`, Bolt driver, database `nornic`, schema
bootstrapped first). Seeded 200 `Repository`/`File`/`SqlTable` triples (all
named `target_table`, so one `CONTAINS` query name-matches every row) plus 50
each of `QUERIES_TABLE`, `TRIGGERS`, `INDEXES`, and the two new
`READS_FROM` sources (`SqlView`, `SqlFunction`) into the matched table, using
the same `UNWIND ... CREATE` write shape as the #5330 evidence. Ran the exact
pre-#5345 OLD query text (five branches, `REFERENCES_TABLE` present) and the
current NEW query text (six branches, `READS_FROM` x2) through the same `Run`
adapter production uses, with the same `$limit` for both shapes so row output
is directly comparable, 50 reps each after a warmup rep, repeated across 5
independent test runs (the first 3 at 20 reps, the last 2 at 50 reps, all
after a warmup rep):

| Run | OLD (5 branches, ms/op) | NEW (6 branches, ms/op) | Delta |
| --- | ---: | ---: | ---: |
| 1 | 1.039 | 1.197 | +15.2% |
| 2 | 0.962 | 1.003 | +4.2% |
| 3 | 1.102 | 1.040 | -5.6% |
| 4 | 1.123 | 1.070 | -4.8% |
| 5 | 0.964 | 0.933 | -3.3% |

Delta straddles zero across runs (mean +1.1%) with no consistent direction,
consistent with run-to-run scheduling/GC noise dominating a sub-millisecond
query rather than a real per-branch cost — unlike the #5330 evidence above
(which removed one large combined `EXISTS {...}` subquery branch and showed a
consistent, one-directional 17-30% improvement), this change adds two cheap
single-hop endpoint-labeled `MATCH` branches, and 5 independent runs found no
consistent regression. Row counts matched on both shapes at every run (306
rows each, both capped by the shared `$limit`), confirming neither shape
errored or silently truncated on this dataset.

No-Observability-Change: the query still runs through the existing
`h.Neo4j.Run` adapter and per-query graph telemetry; no new span, metric,
runtime knob, or queue behavior was added. The response's `complete`/
`coverage` fields already existed from #5330; only which edge types they
report as materialized changed (`sqlTableBlastRadiusEdgeTypes`), computed in
Go from the existing `EdgeMaterializationCoverage` registry with no new graph
read.

### SQL Table Bounded View Traversal and FK/Write Branches (#5410)

The SQL-table blast-radius query now follows reverse view `READS_FROM` edges
through `*1..2` and includes `WRITES_TO` and `REFERENCES_TABLE` branches. The
bounded traversal was proven before implementation against an isolated pinned
NornicDB graph with 500 direct-view repositories, 500 second-level-view
repositories, and 100 third-level controls. The old direct branch returned 500;
the bounded branch returned exactly 1,000 and excluded all third-level controls.

Warm Bolt medians were 1.246 ms for the old 500-row shape and 2.194 ms for the
new 1,000-row shape; all new-shape samples stayed below 2.5 ms. The intended
accuracy gain doubles the useful result set while keeping traversal depth and
latency bounded. See
the repository evidence note at
`docs/internal/evidence/5410-sql-relationships-performance.md`
for the graph shape, samples, source-executor PROFILE result, and NornicDB
PROFILE-display limitation.

No-Observability-Change: the query still uses the existing graph adapter and
per-query telemetry. Reducer target-resolution misses reuse the existing SQL
materialization completion log through new unresolved/ambiguous reference and
write counters; no metric, span, queue, or runtime knob was added.

## Related Docs

- [NornicDB Pitfalls](nornicdb-pitfalls.md)
- [NornicDB Tuning](nornicdb-tuning.md)
- [Local Testing](local-testing.md)
- [Telemetry Overview](telemetry/index.md)
- [Graph Backend Operations](graph-backend-operations.md)
