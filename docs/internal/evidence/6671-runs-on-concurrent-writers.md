# #6671 — RUNS_ON under two concurrent writers, and the Ifá guard's blind spot

Issue #6671 reported two problems. The two RUNS_ON writers could both create the
`(WorkloadInstance)-[:RUNS_ON]->(Platform)` edge, and the Ifá gate could not see
the second copy. #6634 has since merged keyed canonical identity:
`MERGE (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)` in both writers.
This note reconciles the issue's three acceptance items against that merge. The
live proof runs on Neo4j (measured 2026-09-25). A short NornicDB note at the
end is labelled secondary.

## Backend under test

- Neo4j `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`.
  This is the pin in `docker-compose.neo4j.yml` and
  `docker-compose.live-backend-neo4j.yml`.
- It ran as a fresh single container on linux/amd64 with `NEO4J_AUTH=none` and
  database `neo4j`, the same shape as `scripts/run-live-backend-tests.sh`.
- Eshu base: `cab11116f`.

## The live tests

`go/cmd/reducer/runs_on_concurrent_writers_live_test.go` reads `ESHU_NEO4J_URI`
and `ESHU_LIVE_GRAPH_DATABASE` (default `neo4j`), the live-backend runner's
convention. Its ledger row is pinned to `backends: neo4j`.

The test seeds fresh Repository → Workload ← WorkloadInstance chains, each with
its own Platform and no RUNS_ON edge. Both arms drive two writers, each through
its production adapter:

- `reducer.WorkloadMaterializer`, through `newReducerCypherExecutor`;
- `edgewriter.EdgeWriter` (the `repo_dependency` lane), through
  `newReducerNeo4jExecutor`.

In the barrier arm, one barrier releases both writers together. The
sequential control runs the same writers one after the other on separate pairs,
alternating the order. Each arm has a floor of 60 trials, and
`ESHU_RUNS_ON_BARRIER_TRIALS` raises it.

- `TestRunsOnConcurrentWritersOneEdgePerPairLive` covers #6671 A1. It
  hard-fails unless each pair has exactly one edge with
  `identity_key = 'canonical'` and a non-empty `evidence_source`.
- `TestRunsOnConcurrentWritersCrossRepoTupleWinsLive` covers the #6634 tuple
  contract. It hard-fails unless each pair ends with the cross-repo tuple:
  `resolver/cross-repo`, `0.97` and `argocd`.

Shape: each writer call carries one row, and the EdgeWriter batch size is 1.
Production batches repo_dependency rows. This proves the single-pair race, not
a multi-row batch colliding with another batch.

## A1 — one RUNS_ON per pair under the race (Neo4j)

Performance Evidence: runs against the Neo4j pin above.

| Run | Writer text | Arm | Trials | Pairs with >1 edge | Workload tuple kept | Elapsed | Slowest trial |
| --- | --- | --- | --- | --- | --- | --- | --- |
| N1 multiplicity test | keyed | barrier | 300 | 0 | not checked | 43.5s | 7.24s |
| N1 multiplicity test | keyed | sequential | 300 | 0 | not checked | 24.9s | 1.26s |
| N2 tuple test | keyed | barrier | 300 | 0 | 0 | 10.4s | 0.99s |
| N2 tuple test | keyed | sequential | 300 | 0 | 0 | 33.5s | 1.24s |
| N3 scratch, driver DEBUG log | keyed | barrier | 300 | 0 | 0 | 27.9s | 3.43s |
| N4 scratch, driver DEBUG log | bare (pre-#6634) | barrier | 300 | 0 | 0 | 1m45s | 9.31s |
| N5 both tests, default floor | keyed | barrier | 60 + 60 | 0 | 0 (tuple test's 60) | 3.4s, 5.0s | 1.53s |
| N5 both tests, default floor | keyed | sequential | 60 + 60 | 0 | 0 (tuple test's 60) | 5.3s, 2.3s | 1.73s |

Totals on Neo4j:

- Keyed text, multiplicity: 1,020 barrier trials and 720 sequential trials,
  with 0 duplicate edges.
- Keyed text, tuple: 660 barrier trials (N2, N3, N5) and 360 sequential
  trials (N2, N5) checked which tuple won, with 0 lost tuples. The
  multiplicity test does not check the tuple.
- Bare-MERGE text: 300 barrier trials, with 0 duplicate edges and 0 lost
  tuples.

The scratch harness was never committed. It wrapped the production writer
adapters with a runner that made two changes before sending each statement:

- It replaced `[rel:RUNS_ON {identity_key: 'canonical'}]` with `[rel:RUNS_ON]`
  in both writers, which reproduces the pre-#6634 bare
  `MERGE (i)-[rel:RUNS_ON]->(p)`.
- It dropped the legacy unkeyed-edge cleanup statement, which would otherwise
  delete the concurrent writer's bare edge.

Seeding, reads and the writers themselves were unchanged. The scratch runs
classify every trial's rows instead of asserting. Their driver log (8,423 DEBUG
lines per run) recorded no errors, no deadlocks and no `Retrying transaction`
lines. On Neo4j the overlapping writers waited rather than conflicted; the
multi-second slowest trials are consistent with that. This observation is
consistent with Neo4j locking the bound endpoint nodes while it MERGEs a
relationship between them. That was not verified against Neo4j source here.

Disposition: the harness is delivered and multiplicity holds on Neo4j, with 0
duplicates in 1,020 keyed barrier trials. The historical RED does not reproduce on
Neo4j: the pre-#6634 bare MERGE also gave one edge per pair in 300 of 300
barrier trials. The original duplicate (3 in 80 on NornicDB `3722b483c02c`) was
a NornicDB behavior. The test is therefore a forward regression guard, not a
demonstrated pre-fix failure. CI does not enforce it until a lane runs
`scheduled` rows (see CI coverage).

## The cross-repo tuple wins on Neo4j

No Neo4j barrier trial that checked the tuple ended with the workload tuple:
0 of 660 keyed and 0 of 300 bare. The #6634 claim that cross-repo wins in every
ordering therefore stands on Neo4j. The tuple test hard-fails if that ever
changes. A lost cross-repo tuple would be an accuracy defect:
`RetractRepoRunsOnEdgesCypher` deletes only edges whose `evidence_source` is
`resolver/cross-repo`, so a workload-stamped edge would not be reaped when the
cross-repo evidence goes away.

## A2 — the Ifá assertion must see an unstamped duplicate

Before this change, `assert-edges` compared a multiset. But
`scanMaterializedEdges` in `go/cmd/ifa/assert_edges_scan.go` skipped every
RUNS_ON edge whose `evidence_source` was not `resolver/cross-repo` before
counting. A resolver-stamped edge plus an unstamped or workload-stamped copy on
the same pair therefore matched the expected set exactly. #6634 added
`identity_key` to the comparison key, which left that gap open. A2 was not
satisfied on `cab11116f`.

The fix is `MaterializedEdgeEndpoint.OneEdgePerEndpointPair`
(`go/internal/storage/cypher/edge/materialized/endpoints.go:53-68`), set for
RUNS_ON at line 98.

- `countEndpointPair` (`go/cmd/ifa/assert_edges_scan.go:59-60`, defined at line
  110) counts every label-matching edge on a pair before the provenance filter.
- `assertMaterializedEdges` (`go/cmd/ifa/assert_edges.go:263`) fails on any pair
  holding more than one edge. The report names the pair, the count and every
  stamp seen.
- DEPENDS_ON and TARGETS_ENVIRONMENT keep the flag false, and
  `TestOneEdgePerEndpointPairStaysOffWhereStampsPartition` pins that.
- The live reader (`MATCH (a)-[r]->(b)`, no DISTINCT) returns one row per
  relationship, so a real duplicate would reach this check. This is proven on
  synthetic graphs only, because neither backend produced a live duplicate.

Test results:

- RED before the fix: `go test ./cmd/ifa -run TestRunsOn -count=1` failed three
  cases (the unstamped duplicate, the workload-stamped duplicate, and the
  duplicated workload-owned pair), each with "passed the repo_dependency
  assertion".
- GREEN after the fix: the same command passes.
- Mutation: with the RUNS_ON flag set to false, three tests fail:
  `TestRunsOnDuplicateIsVisibleRegardlessOfEvidenceStamp`,
  `TestRunsOnDuplicateOfAnotherWritersEdgeFails` and
  `TestRunsOnDeclaresOneEdgePerEndpointPair`. With the workload_dependency
  DEPENDS_ON flag set to true, the negative test fails.

## A3 — the empty evidence_source on the racing workload edge

The #6634 record explains it:
`docs/internal/evidence/6184-cross-repo-calls-readiness-and-resolver-ordering.md`
lines 189-213 (Defect 5). There, on pinned NornicDB v1.3.2, "`ON CREATE` did
not persist the new relationship stamp". The empty-stamp racing copy seen in
#6671 is consistent with that: a workload-writer edge whose stamp never landed.

The current shape removes that window. The identity MERGE and an
ownership-guarded MATCH/SET run in one transaction
(`go/internal/reducer/workload_materializer.go:431-455`). No Neo4j or NornicDB
barrier trial produced an edge with an empty `evidence_source`, and the
multiplicity test asserts that. The explanation rests on measured behavior; no
one traced a root cause in NornicDB source.

## Secondary note: NornicDB

Before the owner directive to prove graph behavior on Neo4j, these tests also
ran on `ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`.

- Duplicates: 0 in 450 keyed and 360 bare barrier trials.
- Conflicts: every barrier trial paid one
  `Neo.TransientError.Transaction.Outdated` retry, about 2s each.
- Lost tuple: 2 barrier trials kept the workload tuple. That was observed on
  NornicDB and not reproduced on Neo4j, and #7175 is closed.

## Observability

Observability Evidence:

- The live tests log, per arm: `trials`, `elapsed`, `slowest_trial`, and
  `multiplicity_violations` or `tuple_violations`.
- The `assert-edges` failure report has a new "shared-identity duplicate"
  section that lists each pair, its count and its stamps.

No runtime metric, span or writer Cypher changed.

## CI coverage

The live test file is registered as `class: scheduled`, `backends: neo4j` in
`specs/live-tests.v1.yaml`, in one row that names both tests.
`.github/workflows/live-backend-tests.yml` runs only `class: ci` rows
(`scripts/lib/live_backend_test_targets.py`), and no workflow runs `scheduled`
rows, so no CI lane runs either test today. Both are green on Neo4j and take
under 90s at 300 trials per arm, so both are candidates for `ci` promotion at the
60-trial default. The `assert-edges` change is unit-tested in `go/cmd/ifa`,
which CI's Go lanes run.
