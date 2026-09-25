# #6671 — RUNS_ON under two concurrent writers, and the Ifá guard's blind spot

Issue #6671 reported that the two RUNS_ON writers could both create the
`(WorkloadInstance)-[:RUNS_ON]->(Platform)` edge, and that the Ifá gate could
not see the second copy. #6634 has since merged keyed canonical identity:
`MERGE (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)` in both writers.
This note reconciles the issue's three acceptance items against that merge. It
records the live measurements taken on 2026-09-25. The lost-update defect found
along the way is tracked as #7175.

## Backend under test

- Image: `ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`.
  Its tag is `fix-500-e022384c`, the default in `docker-compose.yaml`. Every run
  used a fresh single container on linux/amd64, configured with the Compose
  environment block.
- Upstream provenance: the GitHub compare of `orneryd/NornicDB`
  `145ed4156...e022384c` reports `ahead_by: 98, behind_by: 0`. The pin's
  source commit therefore contains `145ed4156`, "fix(cypher): deduplicate
  concurrent bare relationship merges" (NornicDB#357). This rests on the tag
  having been built from `e022384c`, as its name says. The issue comment
  assumed the pin lacks #357.
- Eshu base: `cab11116f`.

## The live tests

`go/cmd/reducer/runs_on_concurrent_writers_nornicdb_live_test.go` seeds fresh
Repository → Workload ← WorkloadInstance chains. Each chain gets its own
Platform and starts with no RUNS_ON edge. For each pair, a barrier releases
`reducer.WorkloadMaterializer` and `edgewriter.EdgeWriter` together; the
EdgeWriter is the `repo_dependency` lane. Both go through the production
`newReducerCypherExecutor` and `newReducerNeo4jExecutor` adapters. The
sequential control runs the same writers one after the other on separate
pairs, alternating the order. Both tests have a floor of 60 trials per arm, and
`ESHU_RUNS_ON_BARRIER_TRIALS` can raise it.

- `TestRunsOnConcurrentWritersOneEdgePerPairLive` covers #6671 A1. It
  hard-fails unless each pair has exactly one edge with
  `identity_key = 'canonical'` and a non-empty `evidence_source`.
- `TestRunsOnConcurrentWritersCrossRepoTupleWinsLive` covers the #6634 tuple
  contract. It hard-fails unless each pair ends with the cross-repo tuple:
  `resolver/cross-repo`, `0.97`, `argocd`. It is expected to fail
  intermittently until #7175 is fixed.

Shape (F5): each writer call carries one row, and the EdgeWriter batch size is
1. Production batches repo_dependency rows, so this proves the single-pair
race. It does not cover a multi-row batch colliding with another batch.

## A1 — one RUNS_ON per pair under the race

Performance Evidence: barrier runs against the pin above.

| Run | Writer text | Arm | Trials | Pairs with >1 edge | Workload tuple kept | Elapsed |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | keyed | barrier | 60 | 0 | 1 (trial 2) | 2m03s |
| 1 | keyed | sequential | 60 | 0 | 0 | 1.4s |
| 2 | keyed | barrier | 150 | 0 | 0 | 5m25s (per trial p50 2.15s, p95 2.44s, max 2.62s) |
| 2 | keyed | sequential | 150 | 0 | 0 | 6.8s |
| 3 | bare, scratch | barrier | 60 | 0 | 1 (trial 16) | 1m59s |
| 4 | keyed, driver DEBUG log | barrier | 60 | 0 | 0 | 2m04s |
| 5 | keyed, pre-split committed test | barrier | 60 | 0 | 0 | 2m04s |
| 6 | keyed, split multiplicity test | barrier | 60 | 0 | 0 | 2m00s |
| 6 | keyed, split multiplicity test | sequential | 60 | 0 | 0 | 1.3s |
| 7 | keyed, split tuple test | barrier | 60 | 0 | 0 | 2m01s |
| 7 | keyed, split tuple test | sequential | 60 | 0 | 0 | 1.3s |
| 8 | bare, scratch | barrier | 300 | 0 | 0 | 10m21s |

Totals:

- Keyed text: 450 barrier trials, 0 duplicate edges, 1 workload tuple.
- Bare-MERGE scratch text: 360 barrier trials, 0 duplicate edges, 1 workload
  tuple.

The scratch harness was never committed. It wrapped the production writer
adapters with a runner that made two changes before sending each statement.
It replaced `[rel:RUNS_ON {identity_key: 'canonical'}]` with `[rel:RUNS_ON]`
in both writers, which reproduces the pre-#6634 bare
`MERGE (i)-[rel:RUNS_ON]->(p)`. It also dropped the legacy unkeyed-edge cleanup
statement, which would otherwise delete the concurrent writer's bare edge.
Seeding, reads and the writers themselves were unchanged.

Run 3 used the pre-split test, which asserted `identity_key = 'canonical'` in
both arms. Every trial in both arms therefore failed (60 of 60 each), because a
bare MERGE never writes `identity_key`. The table records what those failures
showed: one edge in every trial, and one barrier trial whose edge carried the
workload tuple (`finalization/workloads`, `0.42`, no `source_tool`). Run 8
classifies each trial's rows directly and does not assert anything.

Run 8 measured the conflict behavior on the bare text. All 300 trials hit
exactly one `Neo.TransientError.Transaction.Outdated`: "conflict detected: edge
nornic:merge-<hash> changed after transaction start". The driver's
`ExecuteWrite` then retried. Even a bare relationship MERGE gets a
deterministic `nornic:merge-*` id on this image, which is consistent with #357.
Run 4 measured the same pattern on the keyed text: 60 conflicts and 60 retries
in 60 trials.

Disposition: the harness is delivered and multiplicity holds, with 0 duplicates
in 450 keyed barrier trials. A pre-fix RED cannot be produced on the pinned
image. The bare text also produced 0 duplicates in 360 barrier trials. If the
historical 3-in-80 rate (3.75%) still held, the chance of zero duplicates in 360
trials would be about 1e-6, so the pre-#6634 race does not reproduce here. The
test is therefore a forward regression guard against a backend or pin change,
not a demonstrated pre-fix failure. CI does not enforce it until a lane runs
`scheduled` rows (see CI coverage).

## New finding: a rare lost cross-repo tuple (#7175)

In one keyed barrier trial out of 450 (run 1, trial 2), the pair ended with a
single edge carrying the workload tuple: `evidence_source:
finalization/workloads`, `confidence: 0.42`, no `source_tool`. It should have
carried the cross-repo tuple. This contradicts the #6634 claim that cross-repo
wins in every ordering, recorded in
`docs/internal/design/6634-runs-on-atomic-replay.md` and in Defect 5 of
`docs/internal/evidence/6184-cross-repo-calls-readiness-and-resolver-ordering.md`.
Both now carry a pointer to this note.

The bare scratch text showed the same outcome once in 360 barrier trials (run 3,
trial 16). The defect therefore also occurs without the keyed identity. With one
observation per text, the data cannot say whether the keyed identity changes
its rate.

It is an accuracy defect, not cosmetic. `RetractRepoRunsOnEdgesCypher` deletes
only edges whose `evidence_source` is `resolver/cross-repo`. A workload-stamped
edge left behind by a lost update is therefore not reaped when the cross-repo
evidence goes away.

Every barrier trial produces one `Outdated` conflict, and the losing
transaction replays against committed state. In that replay, either ordering
yields the cross-repo tuple. The lost update therefore implies some interleaving
commits the workload writer's ownership-guarded SET without that conflict
firing. That is a theory; it has not been established from NornicDB source.
Serializing the two writers is not an acceptable fix.

At 1 in 450 per trial, a 60-trial run of the tuple test fails about 12% of the
time. That is why it is split from the multiplicity test.

## Cost of an overlap

A pair on which both writers collide pays one driver-level managed-transaction
retry, which is about 2s of backoff. A pair with no overlap takes about 24ms. In
production, an overlap needs both lanes to reach the same instance and platform
at the same moment. This is a per-collision latency, not a throughput floor.
This note records the cost and does not change it.

## A2 — the Ifá assertion must see an unstamped duplicate

Before this change, `assert-edges` compared a multiset. But
`scanMaterializedEdges` in `go/cmd/ifa/assert_edges_scan.go` skipped every
RUNS_ON edge whose `evidence_source` was not `resolver/cross-repo` before
counting. A resolver-stamped edge plus an unstamped or workload-stamped copy on
the same pair therefore matched the expected set exactly. #6634 added
`identity_key` to the comparison key, but the gap remained. A2 was not
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
  relationship. A real duplicate would therefore reach this check. It is proven
  only on synthetic graphs, because no live duplicate occurs on this image.

Test results:

- RED before the fix: `go test ./cmd/ifa -run TestRunsOn -count=1` failed three
  cases (the unstamped duplicate, the workload-stamped duplicate, and the
  duplicated workload-owned pair). Each reported "passed the repo_dependency
  assertion".
- GREEN after: the same command passes.
- Mutation: with the RUNS_ON flag set to false, three tests fail:
  `TestRunsOnDuplicateIsVisibleRegardlessOfEvidenceStamp`,
  `TestRunsOnDuplicateOfAnotherWritersEdgeFails` and
  `TestRunsOnDeclaresOneEdgePerEndpointPair`. With the workload_dependency
  DEPENDS_ON flag set to true, the negative test fails.

## A3 — the empty evidence_source on the racing workload edge

The #6634 record explains it:
`docs/internal/evidence/6184-cross-repo-calls-readiness-and-resolver-ordering.md`
lines 189-213 (Defect 5). There, on pinned v1.3.2, "`ON CREATE` did not persist
the new relationship stamp". The empty-stamp racing copy #6671 saw is
consistent with that: a workload-writer edge whose stamp never landed.

The current shape removes that window. The identity MERGE and an
ownership-guarded MATCH/SET run in one transaction
(`go/internal/reducer/workload_materializer.go:431-455`). No barrier trial here
produced an edge with an empty `evidence_source`, and the multiplicity test now
asserts it. The explanation is empirical, not a NornicDB source-level root
cause.

## Observability

Observability Evidence:

- The live tests log `trials`, `elapsed` and `slowest_trial` for each arm, plus
  `multiplicity_violations` or `tuple_violations`.
- On a collision, the driver logs `Retrying transaction ...
  Neo.TransientError.Transaction.Outdated` for the shared `nornic:merge-*` edge.
- The `assert-edges` failure report has a new "shared-identity duplicate"
  section that lists each pair, its count and its stamps.

No runtime metric, span or writer Cypher changed.

## CI coverage

The live test file is registered as `class: scheduled` in
`specs/live-tests.v1.yaml`, with one row naming both tests.
`.github/workflows/live-backend-tests.yml` runs only `class: ci` rows
(`scripts/lib/live_backend_test_targets.py`), and no workflow runs `scheduled`
rows. No CI lane runs either test today. The multiplicity test could be promoted
to `ci` on its own. The tuple test cannot be promoted until #7175 is
fixed. The `assert-edges` change is unit-tested in `go/cmd/ifa`, which CI's Go
lanes run.
