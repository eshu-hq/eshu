# #8795: retiring pre-#5385 name-only Workload nodes (performance, backend and observability note)

The change adds a shared detection (`go/internal/graph/legacy/workload`), a
dry-run-first admin route on the reducer (`POST /admin/retire-legacy-workloads`,
`go/internal/reducer/workload/legacy/retire`), and a hosted-readiness check
(`workload_identity_leftovers`). Retirement never runs unless an operator calls
the route with `dry_run: false`.

## Backend scope: Neo4j only

Neo4j 2026.08.1 is the supported canonical graph backend and the only one this
change was tested on. NornicDB was intentionally not tested, by owner direction.

The detection is still written so it does not depend on the Cypher shapes
`docs/public/reference/nornicdb-pitfalls.md` records as unreliable
(relationship-existence predicates, `EXISTS {}`, `OPTIONAL MATCH` plus an
aggregate): the negative clauses (no instance, a scoped twin, only safe
relationships) are decided in Go over single-clause, identity-anchored reads
(`legacyworkload.Detect` and `Evaluate`), and one code path serves the readiness
check, the dry run, and the pre-delete verification. The delete statement
`DeleteNeo4jCypher` re-evaluates every clause itself and is offered on Neo4j
only; the route answers `501` to an execute on any other backend. No NornicDB
behavior is claimed.

On the pinned Neo4j, the nine-case fixture (a leftover with an own-repository
twin; a scoped node with a null `generation_id`; a leftover with an instance by
edge; one with an instance by `workload_id` property only; one with no twin; one
whose twin is in another repository; one with a `DOCUMENTS` edge; one with a
`DEPENDS_ON` edge; one whose twin the repository does not define) gives one
eligible leftover from both the Go detection and the delete statement's own
predicate, and `TestLiveDetectionAndDeleteStatementSelectTheSameIDs` pins that
the two select identical ids on the fixture and after each late change a dry run
could miss. Plain `DELETE w` on a node that still has relationships is refused by
Neo4j.

## Query plans (Neo4j 2026.08.1, community, constraints `workload_id` and
`repository_id` unique, index `workload_instance_workload_id` present)

Seeded graph: 15,017 `Workload` nodes (10,000 scoped with a `generation_id` and
an instance each, 5,000 name-only leftovers each with an own-repository twin, a
`DEFINES` edge from its repository, and no instance), 10,000 `Repository`
nodes, 10,000 `WorkloadInstance` nodes.

- `DeleteNeo4jCypher` read side, `PROFILE` over 100 ids: `NodeUniqueIndexSeek`
  on `Workload(id) WHERE id IN [...]` (batch anchor, 200 db hits),
  `NodeIndexSeek` on `WorkloadInstance(workload_id)` (100), `NodeUniqueIndexSeek`
  on the twin id (400), `Expand(All)` of `DEFINES` into the twin (300), and one
  `Expand(All)` of the leftover's own relationships (200). About 2,500 db hits per
  100 ids. No `AllNodesScan`, `NodeByLabelScan`, or `NodeIndexScan`; the live
  test `TestLiveDeleteNeo4jPlanSeeksTheIDConstraint` pins the two unique-index
  seeks and the absence of scans.
- The shape that would scan: a single-statement count over the whole label
  (`MATCH (w:Workload) WHERE ... NOT EXISTS ...`) planned a `NodeIndexScan` of
  15,000 rows and about 140,000 db hits. That is why readiness pages and
  evaluates in Go instead of using it.

## Measured cost of the shared detection (same graph, Neo4j)

Host: a shared development machine, quiet at measurement. The reads are page
reads of 100 or 500 candidates plus four anchored reads per page.

- Readiness settings (page 500, cap 5,000 examined): 5,000 candidates in 50
  reads, 0.29 to 0.43 s over three runs.
- Dry-run settings (page 100, cap 1,000 examined, stop at 100 eligible): 200
  candidates in 10 reads, 21 to 31 ms over three runs.

Both are bounded by an examined cap, so a very large leftover set cannot make a
request unbounded; a capped readiness count is reported as `N+`.

No-Regression Evidence: the change adds no worker, queue, lease, claim, or batch
setting, and nothing in the reducer's queue path calls the retirement. The only
new graph statements run when an operator calls the admin route or when hosted
readiness is read, never from a projection or materialization stage. Delete
shape: `MATCH (w:Workload) WHERE w.id IN $ids AND <clauses> DETACH DELETE w`,
Neo4j 2026.08.1, at most 100 ids per statement, one auto-commit transaction,
anchored on the `workload_id` unique constraint (`NodeUniqueIndexSeek`), about
2,500 db hits per 100 ids and 0.03 s for the live eligible-delete case. It does
not UNWIND the delete. Lock scope is the batch's nodes and their incident
relationships; the live race proof
(`TestLiveLegacyWorkloadRetirementC2RacesAMaterializationPass`) runs the
retirement against a workload materialization pass for the same repository under
`-race` for four rounds, six repetitions, and ends with the twin intact, one
`DEFINES` into it, and the leftover deleted. Transient errors (deadlock) retry
through the reducer executor's existing classification.

Observability Evidence: `eshu_dp_workload_legacy_retirements_total{outcome}`
(bounded: `deleted`, `blocked_has_instances`, `blocked_no_twin`,
`blocked_relationships`) increments per node judged by an executed request. An
INFO line `legacy workload retirement executed` carries candidates, eligible,
deleted, blocked, truncated, and the operator's reason; a WARN line
`legacy workload kept` carries the node id, the blocked reason, and the
per-type relationship counts for each node kept. A governance audit event
(`admin_recovery_action`, reason `legacy_workload_retire_executed`, `_failed`, or a
`_refused_*` code) records each decision. The standing condition is visible at 3
AM as the failed hosted-readiness check `workload_identity_leftovers` (failure
class `legacy_workload_nodes`) with a `next_diagnostic` that names the dry-run
route. Dry runs record no counter and no audit event.

## What ran where

| proof | backend | where |
| --- | --- | --- |
| unit tests for `Evaluate`, `Detect`, the runner, the route, the readiness check, the counting executor | none | local, `-race` |
| detection live proof (every case) and the Go-vs-statement equivalence | Neo4j | local container; registered for the live-backend CI job |
| delete gate and plan pin | Neo4j | local container; registered for the live-backend CI job |
| retire, idempotency, race with a materialization pass | Neo4j | local container; registered for the live-backend CI job |
| golden-corpus gate (B-7, B-12) | Neo4j | not run locally by this change; see the report |
