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
query-plan fixture at `go/internal/queryplan/testdata/hot-cypher.yaml` against
the NornicDB schema statement contract. The fixture names supply-chain,
deployable, service, code-relationship, and readiness paths; fails deliberately
bad query shapes such as unbounded variable-length traversals, unlabeled
anchors, pagination without deterministic ordering, missing schema evidence,
and forbidden plan signatures; and records explicit caveats for SQL/read-model
paths that do not have a Cypher plan. This gate prevents silent fixture drift
and bad static shapes. It does not replace live backend `EXPLAIN`, `PROFILE`,
or before/after runtime measurements for production Cypher changes.

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

## Quick Reference

| Need | Neo4j | NornicDB |
| --- | --- | --- |
| Cypher feature support | Cypher manual for pinned major | `pkg/cypher/*.go` in NornicDB-New |
| Storage/constraint behavior | Operations manual | `pkg/storage/*.go` in NornicDB-New |
| Known traps | Neo4j changelog | [NornicDB Pitfalls](nornicdb-pitfalls.md) |
| Runtime knobs | Neo4j config reference | [NornicDB Tuning](nornicdb-tuning.md) |
| Version pinning | `NEO4J_VERSION` | `NORNICDB_IMAGE` |

## Evidence Notes

### Relationships Verb Catalog Live Scaling Fix

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

No-Observability-Change: the handlers and gate are unchanged in observability
surface; they reuse the existing query-handler envelope and shared
`GraphQuery.Run`/`RunSingle` adapters, add no new metrics, spans, runtime knobs,
queue behavior, or graph writes, and the query-plan gate stays static validation
only.

### Relationships Verb Catalog And Per-Verb Edge Slice

No-Regression Evidence: issue #3397 adds two new bounded read shapes in
`go/internal/query/relationships_catalog_cypher.go`
(`relationshipCountCypher`, `relationshipEdgesCypher`) backing
`POST /api/v0/relationships/catalog` and `POST /api/v0/relationships/edges`.
These are new endpoints, not a change to an existing path, so there is no prior
shape to regress against. Both shapes are source-label-anchored, never the
unanchored `()-[r:VERB]->()` pattern that risks an all-node scan: each verb is
counted with `MATCH (s:<SourceLabel>)-[r:<VERB>]->() RETURN count(r)`, the same
bounded-aggregate class as the sanctioned whole-graph label count
`MATCH (r:Repository) RETURN count(r)` in `infra_ecosystem_overview.go` and the
`QP-READINESS-HOSTED` fixture. The per-verb edge slice anchors on the same
source label, orders deterministically, and is bounded by `LIMIT $limit`
(default 50, max 200) with a `limit+1` over-fetch for the truncation flag. The
verb and source label are taken only from the fixed `relationshipVerbCatalog`,
never from request input. New gate entries `QP-RELATIONSHIPS-CATALOG-COUNT` and
`QP-RELATIONSHIPS-EDGES` in `go/internal/queryplan/testdata/hot-cypher.yaml`
keep both shapes registered; the static gate validates them against
`graph.SchemaStatementsForBackend(graph.SchemaBackendNornicDB)`, requires the
`Function` source-label index evidence, and forbids `AllNodesScan`,
`CartesianProduct`, and `UnboundedExpand`. Catalog cost is one bounded count per
fixed verb at page load; the capability matrix records a 2000 ms local p95 and
3000 ms production p95 budget for `platform_impact.relationships_catalog`.

No-Observability-Change: the two handlers reuse the existing query-handler
envelope (`WriteSuccess` + `BuildTruthEnvelope` with
`TruthBasisAuthoritativeGraph`) and the shared `GraphQuery.Run`/`RunSingle`
adapters. They add no new metrics, spans, runtime knobs, queue behavior, or
graph writes; the query-plan gate that guards them is static validation only and
opens no backend session.

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

No-Regression Evidence: issue #1696 baseline on `main` showed
`go test ./internal/query -run TestTraceDeploymentChainKeepsConfigDerivedCloudResources -count=1`
failing because deployment trace dropped explicit config-derived CloudResource
rows when workload context did not preserve `deployment_evidence`. After the
fix, `go test ./internal/query -run 'TestTraceDeploymentChainKeepsConfigDerivedCloudResources|TestConfigDerivedCloudResourceDependenciesRequireConfigReadEvidence' -count=1`,
`go test ./internal/query -count=1`,
`go test ./cmd/api ./internal/query ./internal/mcp -count=1`, and
`go test ./... -count=1` pass.

The covered backend/version contract is the existing NornicDB/Neo4j
`GraphQuery.Run` deployment-trace read path. The input shape starts from one
resolved workload or service context and issues config-derived CloudResource
reads only for explicit `READS_CONFIG_FROM` deployment artifacts. The negative
guard proves zero graph reads and zero rows when the artifact relationship is
not `READS_CONFIG_FROM`; positive reads are capped by the existing
service-story item limit, with each config anchor consuming only the remaining
result budget.

No-Observability-Change: the helper uses the existing `GraphQuery.Run` adapter,
`neo4j.query` spans, query-duration metrics, and deployment-trace response
fields. It introduces no new runtime stage, queue, worker, or telemetry surface.
The change is safe because uncorrelated CloudResource candidates remain
unpromoted; only explicit config-read deployment evidence can produce
`relationship_basis=deployment_config_read_evidence`, preserving the
missing-relationship contract for name or ARN coincidences.

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
delta generations can therefore retract stale `REFERENCES_TABLE`, `HAS_COLUMN`,
`TRIGGERS`, and `EXECUTES` edges without requiring current SQL entity rows, and
ordinary full refreshes keep the existing repository-wide retract path.

No-Regression Evidence: `go test ./internal/reducer -run
'TestSQLRelationship(MaterializationHandler(ScopesDeltaRetractToFiles|DeletedOnlyDeltaRetractsWithoutWrites)|HandlerUses(KindFilteredFactLoader|PayloadFilteredContentEntities))|TestBuildSQLRelationshipRetractRowsKeepsMalformedDeltaScoped'
-count=1` proves the reducer extracts repo-qualified delta file paths from the
repository fact, carries them into SQL retract rows, handles deleted-only delta
generations without writes, preserves bounded SQL content-entity loading, and
does not silently downgrade malformed delta scope to repo-wide cleanup. `go test
./internal/storage/cypher -run
'TestEdgeWriterRetractEdgesSQLRelationship(DeltaUsesFileScopedGroup|RejectsDeltaWithoutFilePaths|Dispatch|UsesLabelScopedGroup)|TestBuildRetractSQLRelationshipEdgeStatementsUsesSharedParameters'
-count=1` proves valid delta rows dispatch the five label-scoped SQL retract
statements with `source.path IN $file_paths`, malformed delta rows execute no
Cypher, and non-delta SQL retracts keep their existing repo-wide dispatch
behavior for non-group executors. The input cardinality is the delta file-path
count for one repository generation; the changed Cypher keeps static source
labels and relationship tokens, binds only a positive `$file_paths` list, and
does not add a traversal or backend-specific branch.

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
'TestEdgeWriterRetractEdgesInheritance(DeltaUsesFileScope|RejectsDeltaWithoutFilePaths|Dispatch)|TestEdgeWriterRetractEdgesInheritanceIncludesOverrides|TestBuildRetractInheritanceEdgesByFilePath'
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
'Test(BuildRetractRationaleEdgesByFilePath|EdgeWriterRetractEdgesRationale(DeltaUsesFileScope|RejectsDeltaWithoutFilePaths))'
-count=1` proves valid delta rows dispatch the file-scoped rationale retract
statement with `target.path IN $file_paths`, malformed delta rows execute no
Cypher, and non-delta rationale retracts keep the existing repo-wide dispatch.
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
ORDER BY n.name
LIMIT $limit
```

The result set is byte-identical (same predicates, same projection, same
ordering, same bound); only the anchor changed, so candidate truth is preserved.
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

## Related Docs

- [NornicDB Pitfalls](nornicdb-pitfalls.md)
- [NornicDB Tuning](nornicdb-tuning.md)
- [Local Testing](local-testing.md)
- [Telemetry Overview](telemetry/index.md)
- [Graph Backend Operations](graph-backend-operations.md)
