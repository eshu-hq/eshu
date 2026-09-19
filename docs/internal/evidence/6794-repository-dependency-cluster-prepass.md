# 6794 Repository Dependency-Cluster Pre-Pass

`GET /api/v0/repositories` runs a dependency-cluster pre-pass before it
decorates each repository row. The pre-pass reads every
`(:Repository)-[:DEPENDS_ON]->(:Repository)` edge and groups repositories into
connected components. On a production-scale instance, this stage was the
dominant cost of the route.

## Diagnosis

On a production-scale graph with several hundred `:Repository` nodes and zero
`DEPENDS_ON` relationships, measured directly against NornicDB over its HTTP
transaction endpoint (each statement text made unique so the result cache
could not answer it):

| Statement | Rows | Time |
| --- | --- | --- |
| `MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository) RETURN s.id, t.id ORDER BY ... LIMIT 50000` (the pre-pass) | 0 | 5.3-6.4s |
| `MATCH (s:Repository)-[r:DEPENDS_ON]->(t:Repository) RETURN count(r)` | 1 | 5.4s |
| `MATCH ()-[r:DEPENDS_ON]->() RETURN count(r)` | 1 | 0.09s |
| `MATCH (s)-[r:DEPENDS_ON]->(t) WHERE s:Repository AND t:Repository RETURN ...` | - | over 120s, cancelled |

The Repository-anchored shape expands the full adjacency of every Repository
node to look for `DEPENDS_ON`. Real repositories have very large fan-out
(files, entities, and other edges), so the scan costs seconds even when no
`DEPENDS_ON` edge exists. Only the bare relationship-type count is answered
from the relationship-type index. The earlier measurement recorded in
`dependency_cluster.go` (5.79ms at 900 repositories) came from a synthetic seed
whose Repository nodes had almost no other relationships, which is why it did
not surface this cost.

## Change

For unscoped callers, `loadRepositoryDependencyClusters` first runs
`MATCH ()-[r:DEPENDS_ON]->() RETURN count(r) AS edge_count`. When the count is
zero the edge scan is skipped. That skip is exact: an empty edge set yields an
empty cluster map, so the response is unchanged. Any nonzero count runs the
unchanged scan.

- Scoped callers never issue the unscoped probe; they keep the
  grant-predicated scan, so every statement on the repository-list path still
  carries the caller's grant (`TestRepositoryListGraphAppliesScopedAuthBeforePagination`).
- A probe failure still runs the scan, and a missing or unrecognized probe
  value never counts as zero, so cluster evidence cannot be dropped by an
  unreadable probe.
- Probe and scan failures were previously swallowed; they now emit warning
  events.

When `DEPENDS_ON` edges do exist the scan cost is unchanged. That case is
bounded by the same NornicDB expansion behavior and is not addressed here.

## Performance Evidence

Performance Evidence: `GET /api/v0/repositories` on a production-scale instance
(several hundred repositories, zero `DEPENDS_ON` edges, NornicDB v1.3.3
backend, Postgres at 8 CPU), unscoped caller, local API processes built from
the base commit and from this branch against the same live backends,
interleaved, 6 rounds each, 20s apart so ongoing ingestion invalidated the
NornicDB result cache between rounds:

| Build | Route wall time | `dependency_cluster_edges` stage |
| --- | --- | --- |
| base | 5.73, 10.13, 7.99, 8.61, 6.69, 6.23s | 5.50-10.00s |
| this branch | 0.35, 0.30, 0.40, 0.48, 0.40, 0.28s | 0.030-0.032s (scan skipped) |

Response bodies (the `data` payload) were identical in all 6 rounds.

## Observability Evidence

Observability Evidence: the existing `repository_query.stage_completed` event
for `stage=dependency_cluster_edges` now carries `edge_scan_skipped`
(true/false) next to `cluster_count` and `duration_seconds`. Probe and scan
failures emit `repository_query.dependency_cluster_probe_failed` and
`repository_query.dependency_cluster_scan_failed` warning events with the
error, where the scan failure was previously silent.
