# #6786: repository dependency marker and relationship repo-anchor fixes

Two NornicDB v1.3.3 correctness defects found during the epic #6788 prod
Cypher audit, fixed with TDD in `fix/6786-nornicdb-defect-exposure`.

## Defect 1: repository dependency marker (`is_dependency`)

`querycontract.RepositoryDependencyMarkerProjection` rendered
`EXISTS { MATCH (r)<-[:DEPENDS_ON]-(dep:Repository)... } as is_dependency`
as a per-row RETURN expression, consumed by `GET /api/v0/repositories`
(unscoped and scoped) and `GET /api/v0/catalog` (unscoped).

- On NornicDB v1.3.3 an `EXISTS` used as a RETURN expression is **always
  false**, so every repository row reported `is_dependency=false`
  regardless of ground truth.
- In scoped mode the caller's grant predicate was spliced directly after the
  `MATCH` pattern with no `WHERE` keyword
  (`MATCH (r)<-[:DEPENDS_ON]-(dep:Repository) AND (dep.id IN
  $allowed_repository_ids OR ...)`), which is invalid Cypher: Neo4j fails the
  whole scoped repository list with `Neo.ClientError.Statement.SyntaxError`,
  while NornicDB silently accepts it and still returns false.

Fix: `is_dependency` is now derived in Go
(`repository.repositoryDependencyTargetSet`) from the same bounded,
already-scoped `(:Repository)-[:DEPENDS_ON]->(:Repository)` edge pre-pass the
handler already ran for dependency-cluster grouping
(`repository.loadRepositoryDependencyEdges` /
`repositoryDependencyClusterEdgeCypher`, issue #3504), computed once and
reused for both signals. The page query's `RETURN` no longer carries any
`EXISTS`, `DEPENDS_ON`, or `is_dependency` text. `GET /api/v0/catalog`, which
never ran the edge pre-pass before, now runs it once (unscoped) to derive the
same marker.

## Defect 2: repo-filtered relationship lookup

`codequery` `relationshipsGraphRow`'s name+repo_id branch rendered
`e.name = $name AND EXISTS { MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
WHERE repo.id = $repo_id }` as a filter on a bare `MATCH (e)` label scan
(`codemodel.RelationshipGraphRowCypher`).

- On NornicDB v1.3.3 that backward multi-hop `EXISTS` is silently ignored: a
  same-named entity in a *different* repository still matched, and
  `RunSingle` returned whichever row came back first, regardless of the
  requested `repo_id`.
- Neo4j evaluates the `EXISTS` correctly.

Fix: `codemodel.RelationshipGraphRowCypherAnchored` lets the caller supply
the entity `MATCH` clause. The `repo_id` branch now anchors on
`MATCH (anchorRepo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(anchorFile:File)-[:CONTAINS]->(e)`
— the same repository-anchored shape `entity.BuildResolveEntityGraphQuery`
already uses — instead of a post-hoc existence check. `RelationshipGraphRowCypher`
(the entity_id and name-only branches) is unchanged, implemented as
`RelationshipGraphRowCypherAnchored("MATCH (e)", predicate)`.

## RED/GREEN

Live tests behind `-tags live_nornicdb_answer_truth`:

- `go/internal/query/repository/nornicdb_dependency_marker_live_test.go`
  (`TestLiveRepositoryDependencyMarkerAnswerTruth`) drives the real
  `GET /api/v0/repositories` (unscoped, scoped) and `GET /api/v0/catalog`
  handlers.
- `go/internal/query/codequery/nornicdb_relationship_repo_anchor_live_test.go`
  (`TestLiveRelationshipRepoAnchorAnswerTruth`) drives the real
  `POST /api/v0/code/relationships` handler.

RED (before the fix), against isolated containers, NornicDB v1.3.3
(`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f`)
and Neo4j (`neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`),
schema applied via `graph.EnsureSchemaWithBackend`:

- NornicDB, defect 1: unscoped and scoped subtests failed — every seeded
  repository reported `is_dependency=false` including the two true
  dependency targets.
- Neo4j, defect 1: unscoped passed (already correct); scoped subtest failed
  with `status = 500 ... Neo.ClientError.Statement.SyntaxError (Invalid
  input '{': ... EXISTS { MATCH (r)<-[:DEPENDS_ON]-(dep:Repository) AND
  (dep.id IN $allowed_repository_ids OR ...) }`.
- NornicDB, defect 2: querying `"Run"` scoped to repo A and repo B resolved
  to the *same* entity id, proving `repo_id` was not applied.
- Neo4j, defect 2: passed (already correct).

GREEN (after the fix), same containers, same seeds:

```
ESHU_NEO4J_URI=bolt://127.0.0.1:27900 ESHU_LIVE_GRAPH_BACKEND=nornicdb go test ./internal/query/repository -tags live_nornicdb_answer_truth -run TestLiveRepositoryDependencyMarkerAnswerTruth -count=1 -v
  => 4 passed (unscoped, scoped, catalog_unscoped)
ESHU_NEO4J_URI=bolt://127.0.0.1:27910 ESHU_LIVE_GRAPH_BACKEND=neo4j   go test ./internal/query/repository -tags live_nornicdb_answer_truth -run TestLiveRepositoryDependencyMarkerAnswerTruth -count=1 -v
  => 4 passed
ESHU_NEO4J_URI=bolt://127.0.0.1:27900 ESHU_LIVE_GRAPH_BACKEND=nornicdb go test ./internal/query/codequery  -tags live_nornicdb_answer_truth -run TestLiveRelationshipRepoAnchorAnswerTruth -count=1 -v
  => 1 passed
ESHU_NEO4J_URI=bolt://127.0.0.1:27910 ESHU_LIVE_GRAPH_BACKEND=neo4j   go test ./internal/query/codequery  -tags live_nornicdb_answer_truth -run TestLiveRelationshipRepoAnchorAnswerTruth -count=1 -v
  => 1 passed
```

Non-live unit coverage pins the new Cypher/Go shapes:
`code_relationships_graph_response_test.go`,
`relationship_repo_anchor_test.go`, `dependency_cluster_test.go` (new
`repositoryDependencyTargetSet`/`loadRepositoryDependencyEdges` cases),
`list_dependency_marker_test.go`, `catalog_dependency_marker_test.go`, and
`authz_test.go` (updated to reflect that `is_dependency` is now
edge-derived, not a fake row field).

`go test ./internal/query/... -count=1` => 4846 passed, 0 failed.
`go vet ./internal/query/...` => clean. `gofumpt -l` on every changed file
=> no output.

## Performance Evidence

No-Regression Evidence: this is a correctness fix, not a performance
optimization, so the acceptance bar is "no regression" on the touched
statements, not a scaled corpus replay. Single-observation-per-shape medians
(n=40 reads each, identical seed reused for before/after, same isolated
NornicDB v1.3.3 container/session pattern `repository/dependency_cluster.go`
already documents its own edge-cypher timing with), 20 repositories / 10
DEPENDS_ON edges for the repository-list shapes, 2 repositories / 1
same-named Function each for the relationship shape:

| Statement | Before | After |
| --- | --- | --- |
| Repository list page query (unscoped, is_dependency column) | 303.8µs median (n=40) | 283.1µs median (n=40) |
| Dependency-edge pre-pass (unchanged Cypher; new extra round trip for `GET /api/v0/catalog` only — `GET /api/v0/repositories` already ran it for clustering) | n/a | 225.2µs median (n=40) |
| Relationship repo-anchored lookup (`POST /api/v0/code/relationships`, name+repo_id) | 255.0µs median (n=40) | 237.5µs median (n=40) |

Both statements got marginally faster (dropping a per-row EXISTS subquery
and replacing a scan-then-filter with an anchored MATCH are both cheaper,
not more expensive) and are within run-to-run noise either way at this
corpus size — well under the skill's 10%/60s regression stop threshold in
either direction. `GET /api/v0/catalog`'s only new cost is the ~225µs
bounded edge pre-pass it previously skipped entirely, which is required for
correctness (defect 1) and is already the exact query
`repository.loadRepositoryDependencyEdges` runs, bounded by
`repositoryDependencyClusterEdgeLimit` (50000) with an `ORDER BY`. No change
to result cardinality, page limits, or timeout behavior on any touched
handler. Absolute figures are resource-qualified to this shared host and
container image pinning; only the relative before/after delta on identical
inputs is the claim.

## Observability Evidence

Observability Evidence: the existing `repository_query.stage_started` /
`repository_query.stage_completed` log events for
`operation=repository_list, stage=dependency_cluster_edges` are unchanged in
shape and now also carry `edge_count` (previously only `cluster_count`),
giving operators direct visibility into how many DEPENDS_ON edges backed
both the cluster grouping and the is_dependency marker in one place. No new
signal was needed for `GET /api/v0/catalog` or the relationship lookup: both
already flow through the shared `WriteGraphReadError` / bounded-read-error
telemetry path for any backend failure, and neither introduces a new failure
mode (the Go-derived marker degrades to `is_dependency=false` on an edge-read
error, matching the existing "degrade rather than fail the whole list"
contract `loadRepositoryDependencyEdges`'s doc comment states).
