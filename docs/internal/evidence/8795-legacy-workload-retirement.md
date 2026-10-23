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
check, the dry run, and the pre-delete verification. That shape is a design
choice, not a NornicDB claim: no NornicDB behavior is claimed. The route
answers `501` to dry runs and execute on any backend other than Neo4j, and the
readiness check reports `unknown` there and does not run the detection.

## Concurrent writers (lock-first delete)

`DeleteNeo4jCypher` sets and removes a marker property on each named node before
it evaluates any retirement clause, so it holds the node's write lock while it
checks. A throwaway probe on a fresh Neo4j 2026.08.1 first proved the defect in
the earlier shape (clauses in the first `WHERE`): a writer transaction T1
created a `WorkloadInstance -[:INSTANCE_OF]->` the eligible node and stayed
open; the delete blocked on T1; after T1 committed the delete reported
`nodes=1`, and the instance was left without its target. Same probe with the
lock-first shape: the delete was aborted with
`Neo.TransientError.Transaction.DeadlockDetected` (T1 held a shared
relationship-group lock and needed the node lock at commit; the delete held the
node lock and needed the relationship-group lock), nothing was deleted, and T1's
edge committed.

`TestLiveDeleteNeo4jRechecksAfterTheLockWait` pins both cases: a property write
on the node (the delete waits, re-reads, deletes nothing, no error) and an
`INSTANCE_OF` edge created into the node over three rounds (every round ended
with the delete aborted, the node and the edge intact, and a retry deleting
nothing). Not covered, and stated in the runbook: a writer that touches neither
the node nor its relationships (an instance naming the node only by its
`workload_id` property, or the twin losing its `DEFINES`) while the delete runs.

On the pinned Neo4j, the nine-case fixture (a leftover with an own-repository
twin; a scoped node with a null `generation_id`; a leftover with an instance by
edge; one with an instance by `workload_id` property only; one with no twin; one
whose twin is in another repository; one with a `DOCUMENTS` edge; one with a
`DEPENDS_ON` edge; one whose twin the repository does not define) gives one
eligible leftover from both the Go detection and the delete statement's own
predicate, and `TestLiveDetectionAndDeleteStatementSelectTheSameIDs` pins that
the two select identical ids on the fixture and after each late change a dry run
could miss. The fixture also carries a twin with a null `generation_id`, a
foreign repository `DEFINES` into an otherwise eligible leftover, and a
non-legacy-shaped id passed in `$ids`; dropping the own-repository `DEFINES`
rule, either twin `generation_id IS NOT NULL` check, or the legacy-shape clause
each fails a live test. Plain `DELETE w` on a node that still has relationships is refused by
Neo4j.

## Query plans (Neo4j 2026.08.1, community, constraints `workload_id` and
`repository_id` unique, index `workload_instance_workload_id` present; the plan
pin applies these from the production schema statements itself, so it holds on
a fresh database)

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

Host: a shared development machine. The reads are
page reads of 100 or 500 candidates plus four anchored reads per page.
Re-measured on 2026-10-23 on a fresh `neo4j:2026-community` (2026.08.1)
container with the production `workload_id`, `repository_id`, and
`workload_instance_workload_id` schema statements, seeded with 15,000
`Workload` nodes (10,000 scoped with a `generation_id`, a `DEFINES` edge, and
an instance each; 5,000 name-only leftovers each with an own-repository twin and
a `DEFINES` edge), 10,000 `Repository` nodes, and 10,000 `WorkloadInstance`
nodes. A throwaway `legacyworkload.Detect` test with a read-counting reader
(not committed) ran each setting four times, with
`ESHU_CYPHER_BOLT_DSN=bolt://127.0.0.1:<port> go test -tags
live_nornicdb_answer_truth ./internal/graph/legacy/workload/`:

- Readiness settings (page 500, cap 5,000 examined): 5,000 candidates in 50
  reads, 0.75 s on the cold first run and 0.23 to 0.34 s on the next three.
- Dry-run settings (page 100, cap 1,000 examined, stop at 100 eligible): 100
  candidates in 5 reads (one page reaches the limit), 0.68 s on the cold first
  run and 29 to 44 ms on the next three.

Both are bounded by an examined cap, so a very large leftover set cannot make a
request unbounded; a capped readiness count is reported as `N+`.

No-Regression Evidence: the change adds no worker, queue, lease, claim, or batch
setting, and nothing in the reducer's queue path calls the retirement. The only
new graph statements run when an operator calls the admin route or when hosted
readiness is read, never from a projection or materialization stage. Delete
shape: `MATCH (w:Workload) WHERE w.id IN $ids SET w.__eshu_retire_lock = true
REMOVE w.__eshu_retire_lock WITH w WHERE <clauses> DETACH DELETE w`,
Neo4j 2026.08.1, at most 100 ids per statement, one auto-commit transaction,
anchored on the `workload_id` unique constraint (`NodeUniqueIndexSeek`), about
2,500 db hits per 100 ids and 0.03 s for the live eligible-delete case (measured
on the earlier shape without the lock markers; the lock-first shape adds one
property set and one remove per named node, and its plan pin still finds the two
unique-index seeks and no scan, but its db hits were not re-measured). It does
not UNWIND the delete. Lock scope is the batch's nodes and their incident
relationships; the live race proof
(`TestLiveLegacyWorkloadRetirementC2RacesAMaterializationPass`) runs the
retirement against a workload materialization pass for the same repository under
`-race` for four rounds, six repetitions, and ends with the twin intact, one
`DEFINES` into it, and the leftover deleted. Transient errors (deadlock) retry
through the reducer executor's existing classification.

Observability Evidence: `eshu_dp_workload_legacy_retirements_total{outcome}`
(bounded: `deleted`, `blocked_has_instances`, `blocked_no_twin`,
`blocked_relationships`) increments per node judged by an executed request (every verdict, up to the
1,000 examined, not only the 100 blocked nodes the response lists). An
INFO line `legacy workload retirement executed` carries candidates, eligible,
deleted, blocked, truncated, and the operator's reason; a WARN line
`legacy workload kept` carries the node id, the blocked reason, and the
per-type relationship counts for each node kept. A Neo4j reducer with no graph
writer wired refuses execute with `503` and the audit reason
`legacy_workload_retire_refused_writer_unavailable`. A governance audit event
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
| `legacy_id` on the ambiguity candidates (unit and HTTP `409` through the story route) | none | local, `-race` |
| scenario A2: story `409` marks the leftover, the retirement statement removes it, the same selectors then resolve to the scoped workload | Neo4j | local container; registered for the live-backend CI job |
| golden-corpus gate (B-7, B-12) | Neo4j | not run locally by this change; see the report |
