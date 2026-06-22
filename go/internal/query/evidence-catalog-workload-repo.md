# Catalog Workload-Repository Read Performance

This note records the query-shape contract and measured proof for the
workload->repository enrichment behind `GET /api/v0/catalog` (issue #3466).

## Query shape

`catalogWorkloadRepoCypher` resolves each bounded workload's defining
repository. It MUST be a single connected path anchored on the limited workload
id set:

```cypher
MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)
WHERE w.id IN $ids
RETURN w.id AS id, repo.id AS repo_id, repo.name AS repo_name
ORDER BY id
```

The previous shape used two separate MATCH clauses -- `MATCH (w:Workload) WHERE
w.id IN $ids` followed by `MATCH (repo:Repository)-[:DEFINES]->(w)`. On NornicDB
the second MATCH cold-plans as a full `:Repository` label scan with per-repository
`:DEFINES` fanout re-joined to `w`, so the catalog endpoint timed out even though
the bounded id list (`#3389`) already capped the workload set. This is the same
re-anchor anti-pattern fixed for the deployment-evidence query in `#1731`
(`catalogWorkloadEvidenceEnvironmentCypher`); the repository enrichment was left
on the slow double-MATCH form and reintroduced the timeout once `#3399` made the
catalog the console's primary entity surface.

The bounded-id anchor (`WHERE w.id IN $ids`) is preserved, so the query never
aggregates over the whole `Workload`/`Repository` population. The Postgres
service-catalog correlation enrichment added in `#3399`
(`enrichCatalogWorkloadsFromCorrelations`) was profiled and is NOT the
bottleneck (see below).

## Performance Evidence

Performance Evidence: NornicDB, `nornic` database, live ~900-repo stack
(951 `Repository`, 94 `Workload`). Measured over the Bolt-HTTP `tx/commit`
endpoint with the live workload id set (94 ids) bound as `$ids`.

Before (two MATCH clauses, `MATCH (repo:Repository)-[:DEFINES]->(w)`):
- 36.35s wall, 98 rows.

After (single workload-anchored path, `(w:Workload)<-[:DEFINES]-(repo:Repository)`):
- 0.018s wall, 98 rows. Result rows are byte-identical to the before shape
  (verified by sorted-tuple comparison of both result sets).

Query-shape improvement (load-independent): the operator change is from a
re-anchored Repository label scan + per-repo DEFINES fanout to a single
workload-id-anchored expansion, eliminating the per-repository cross-join. The
other two catalog enrichments were already single-pass and fast on the same
corpus: instance-environment 0.25s, evidence-environment 0.11s.

Postgres correlation enrichment (`#3399`) profiled on the same stack
(7.3M `fact_records`, 2 active `reducer_service_catalog_correlation` facts, 900
repository ids in `AllowedRepositoryIDs`): `EXPLAIN (ANALYZE)` shows the planner
anchors on `fact_records` via
`fact_records_service_catalog_correlations_workload_idx` joined to the active
scope/generation rows, executing in 2.4ms. The catalog timeout was never the
Postgres enrichment; it was the graph workload-repo re-anchor.

No-Observability-Change: this change only narrows an existing Cypher read shape
to its single-pass equivalent; the catalog handler's span, truth envelope, and
counts are unchanged and continue to report the same byte-identical rows.

## Focused proof

`go test ./internal/query -run Catalog -count=1` -- includes
`TestCatalogWorkloadRepoCypherIsSingleChain`, which pins the single-chain,
bounded-id shape so a regression to the double-MATCH form fails the build.
