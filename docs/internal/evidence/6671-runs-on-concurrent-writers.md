# #6671 — RUNS_ON under two concurrent writers, and the Ifá guard's blind spot

Issue #6671 reported that the two RUNS_ON writers could both create the
`(WorkloadInstance)-[:RUNS_ON]->(Platform)` edge, and that the Ifá gate could
not see the second copy. #6634 has since merged keyed canonical identity
(`MERGE (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)` in both writers).
This note reconciles the issue's three acceptance items against that merge and
records the live measurements taken on 2026-09-25.

## Backend under test

- Image: `ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`
  (tag `fix-500-e022384c`, the `docker-compose.yaml` default). It ran as a
  fresh single container on linux/amd64 with the Compose environment block, on
  an empty data directory.
- Upstream provenance: GitHub's compare `orneryd/NornicDB` `145ed4156...e022384c`
  reports `ahead_by: 98, behind_by: 0`. The pin's source commit therefore
  already contains `145ed4156` ("fix(cypher): deduplicate concurrent bare
  relationship merges", NornicDB#357). The issue comment assumed the pin lacks
  #357. That does not hold for this pin.
- Eshu base: `cab11116f`.

## A1 — live barrier through both production writers

`go/cmd/reducer/runs_on_concurrent_writers_nornicdb_live_test.go` seeds fresh
Repository → Workload ← WorkloadInstance chains, each with its own Platform and
no RUNS_ON edge. For each pair, one barrier releases both
`reducer.WorkloadMaterializer` and `edgewriter.EdgeWriter` (the
`repo_dependency` lane). They run through the production
`newReducerCypherExecutor` / `newReducerNeo4jExecutor` adapters. The test then
asserts exactly one RUNS_ON edge carrying the cross-repo tuple. The sequential
control runs the same writers one after the other on separate pairs, alternating
the order.

Performance Evidence: barrier runs against the pin above. The default is 60
trials; `ESHU_RUNS_ON_BARRIER_TRIALS` raises it.

| Run | Writer text | Arm | Trials | Pairs with >1 edge | Wrong tuple | Elapsed |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | keyed (#6634) | barrier | 60 | 0 | 1 (trial 2) | 2m03s |
| 1 | keyed | sequential | 60 | 0 | 0 | 1.4s |
| 2 | keyed | barrier | 150 | 0 | 0 | 5m25s (per trial p50 2.15s, p95 2.44s, max 2.62s) |
| 2 | keyed | sequential | 150 | 0 | 0 | 6.8s |
| 3 | pre-#6634 bare MERGE (scratch mutation, not committed) | barrier | 60 | 0 | 1 | 1m59s |
| 4 | keyed, driver DEBUG logging (scratch) | barrier | 60 | 0 | 0 | 2m04s |
| 4 | keyed, driver DEBUG logging (scratch) | sequential | 60 | 0 | 0 | 1.4s |
| 5 | keyed, final committed test | barrier | 60 | 0 | 0 | 2m04s (slowest trial 2.26s) |
| 5 | keyed, final committed test | sequential | 60 | 0 | 0 | 1.4s (slowest trial 34ms) |

Totals: 0 duplicate edges across 330 keyed barrier trials and across 60
bare-MERGE barrier trials.

Disposition: satisfied for multiplicity. On this image the test cannot be shown
failing before the fix. The scratch mutation removed `{identity_key:
'canonical'}` from both writers' RUNS_ON patterns and dropped the legacy
cleanup statement, reproducing the pre-#6634 bare `MERGE (i)-[rel:RUNS_ON]->(p)`.
It still produced exactly one edge per pair in 60 of 60 barrier trials. The
backend now dedups bare relationship MERGE, which is consistent with #357 being
in the pin. The historical 3-in-80 duplicate rate on `3722b483c02c` does not
reproduce here. The test's sensitivity to the tuple contract is shown by runs
1 and 3. Its sensitivity to multiplicity is unproven on this image. (In run 3
the sequential arm also failed, as expected: a bare MERGE never writes
`identity_key`. The barrier-arm failures were all tuple mismatches on single
edges.)

### New finding: a rare lost update on the shared edge

Trial 2 of run 1 ended with a single edge that carried the workload tuple
(`evidence_source: finalization/workloads`, `confidence: 0.42`, no
`source_tool`) instead of the cross-repo tuple. That contradicts the #6634
contract that cross-repo wins in every ordering
(`docs/internal/evidence/6184-cross-repo-calls-readiness-and-resolver-ordering.md`,
Defect 5). The bare-MERGE run showed the same outcome once, so the keyed
identity is not the cause. Across both texts it happened in 2 of 390 barrier
trials. The committed test fails when it happens, so the test's ledger row is
`scheduled`, not `ci`.

Mechanism measured in run 4: every barrier trial (60 of 60) produced exactly one
`Neo.TransientError.Transaction.Outdated` error ("failed to update edge
property: conflict detected: edge nornic:merge-<hash> changed after transaction
start"). The driver's managed `ExecuteWrite` retried it after a backoff of about
1.8 to 1.9s. So the backend usually detects the write-write conflict on the
deterministic merge edge id. The losing transaction then replays against
committed state, and either ordering yields the cross-repo tuple. The lost
update means some interleaving commits the workload writer's ownership-guarded
SET without that conflict firing. That cause is still a theory: nothing in the
NornicDB source has confirmed it. It needs its own issue before anyone changes
code. Serializing the two writers is not an acceptable fix.

### Cost of an overlap

A pair on which both writers collide pays one driver-level managed-transaction
retry: about 2s of backoff, against about 24ms for a pair with no overlap. In
production, an overlap needs both lanes to reach the same instance and platform
at the same moment. That makes this a per-collision latency, not a throughput
floor. This change records the cost and does not change it.

## A2 — the Ifá assertion must see an unstamped duplicate

Before this change, `assert-edges` compared a multiset, but
`scanMaterializedEdges` (`go/cmd/ifa/assert_edges_scan.go`) skipped every
RUNS_ON edge whose `evidence_source` was not `resolver/cross-repo` before it
counted anything. A resolver-stamped edge plus an unstamped or workload-stamped
copy on the same pair therefore matched the expected set exactly. #6634 added
`identity_key` to the comparison key, but that left the gap open. A2 was not
satisfied on `cab11116f`.

The fix adds `MaterializedEdgeEndpoint.OneEdgePerEndpointPair`
(`go/internal/storage/cypher/edge/materialized/endpoints.go:53-68`), set for
RUNS_ON at line 98. With the flag set, `countEndpointPair`
(`go/cmd/ifa/assert_edges_scan.go:59-60`, defined at line 110) counts every
label-matching edge on a pair before the provenance filter runs.
`assertMaterializedEdges` (`go/cmd/ifa/assert_edges.go:263`) then fails on any
pair holding more than one edge. The failure message names the pair, the count
and each stamp seen. DEPENDS_ON and TARGETS_ENVIRONMENT keep the flag false.

- RED before the fix: `go test ./cmd/ifa -run TestRunsOn -count=1` failed
  three cases: the unstamped duplicate, the workload-stamped duplicate, and
  the duplicated workload-owned pair. Each reported "passed the
  repo_dependency assertion".
- GREEN after the fix: the same command passes. The resolver-stamped duplicate
  case was already caught before the fix and stays green, and so does the
  one-edge-per-pair partition control.
- Mutation: with the RUNS_ON flag set to false,
  `TestRunsOnDuplicateIsVisibleRegardlessOfEvidenceStamp`,
  `TestRunsOnDuplicateOfAnotherWritersEdgeFails` and
  `TestRunsOnDeclaresOneEdgePerEndpointPair` all fail.

## A3 — the empty evidence_source on the racing workload edge

#6634's measured record explains it:
`docs/internal/evidence/6184-cross-repo-calls-readiness-and-resolver-ordering.md`
lines 189-213 (Defect 5). On pinned v1.3.2, "`ON CREATE` did not persist the
new relationship stamp". The racing copy was the workload writer's
bare-MERGE-created edge, and its stamp never landed. The current write shape
closes that window: the identity MERGE and an ownership-guarded MATCH/SET run in
one transaction (`go/internal/reducer/workload_materializer.go:431-455`). No
barrier trial in this note produced an edge with an empty `evidence_source`.
The explanation rests on that measured behavior. Nobody has traced it to a
root cause in the NornicDB source.

## Observability

Observability Evidence: each arm of the barrier test logs `trials`,
`violations`, `elapsed` and `slowest_trial`. On a collision, the driver logs
`Retrying transaction ... Neo.TransientError.Transaction.Outdated` for the
shared `nornic:merge-*` edge at debug level. The `assert-edges` failure report
has a new "shared-identity duplicate" section that lists each pair, its count
and its stamps. No runtime metric, span or writer Cypher changed.

## CI coverage

The new live test is registered as `class: scheduled` in
`specs/live-tests.v1.yaml`. `.github/workflows/live-backend-tests.yml` runs only
`class: ci` rows (`scripts/lib/live_backend_test_targets.py`). No workflow runs
`scheduled` rows, so no CI lane runs this test today. The `assert-edges` change
is unit-tested in `go/cmd/ifa`, which CI's Go lanes run.
