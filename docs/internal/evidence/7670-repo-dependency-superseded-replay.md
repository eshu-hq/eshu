# #7670: a retired generation owes no workload replay

## Problem

After recover-generations retired scope generations, the repo_dependency
shared-projection runner failed every cycle with
`replay workload materialization after repo dependency projection: workload
materialization replay was not scheduled for scope S generation G entity
repo:E`, then quarantined its lease for five to eight minutes, repeatedly. In
production 1,291 repo_dependency intents never drained.

Root-Cause Evidence: the runner replays `workload_materialization` for every
distinct scope, generation and entity among its active rows
(`repoDependencyReplayRequests`). The rows are active when their generation
equals the generation in `shared_projection_acceptance`. recover-generations
retires a scope generation without rewriting that acceptance row, and
`GateAcceptedGenerationOnActive` bypasses the relationship-generation fence for
code-import and package-consumption source runs, so the accepted generation can
name a retired one. `ReducerQueue.ReplayWorkloadMaterialization` revives only
`succeeded` items, so for the retired generation's stable item (status
`superseded`) the UPDATE matches nothing, the enqueue conflicts on the stable
identity, and `workloadMaterializationReplayScheduledQuery` does not count
`superseded` as scheduled. The replay returns false, the runner turns false
into an error, and the cycle quarantines. The production scope had a
superseded generation and a different active generation.

## RED runs

Round 1: with the new types, the runner field and the counter declared and no
behavior change, the new reducer regressions failed with the production message:

```text
processOnce() error = quarantine repo dependency lease for 5m0s: replay workload materialization after repo dependency projection: workload materialization replay was not scheduled for scope "scope-gen-superseded" generation "gen-superseded" entity "repo:r_fixture01", want nil: a superseded generation has nothing to replay
```

Round 2 (arbiter ruling): the round-2 tests and the round-2 telemetry
declarations ran against the unmodified round-1 production code in a throwaway
worktree checked out at the round-1 commit (no stash). Failing assertions:

```text
TestRepoDependencyReplayFailsClosedOnSupersededItemOnActiveGeneration: processOnce() error = <nil>, want the unscheduled-replay error
TestRepoDependencyReplayFailsClosedOnSupersededItemWithoutFreshness: processOnce() error = <nil>, want the unscheduled-replay error
TestRepoDependencyReplaySkipsWhenSupersedeLandsBetweenCheckAndReplay: inactive_generation skip counter = 0, want 1
TestRepoDependencyRunsOnFencedReplaySkipsRetiredGeneration: readiness prefetch ran for a retired generation, want no wait on it
TestRepoDependencyInactiveAcceptedGenerationRowsAreCountedAndLogged: inactive_accepted_generation counter = 0, want 2 rows on the retired generation
```

`TestRepoDependencyReplayRechecksFreshnessEachCycle` passes on the unmodified
code. It guards behavior that must not change, so it is not a regression test.
It does fail when the cache is kept across cycles: with `newGenerationFreshness`
temporarily returning a shared package-level cache it failed with
`freshness lookups across two cycles = 1, want 2: the cache must not outlive a cycle`.

Round 3 (reviewer findings): the new tests ran against the round-2
commit in a throwaway worktree with the new telemetry declarations. Failing assertions:

```text
TestRepoDependencyRunsOnFencedReplaySkipsWhenSupersedeLandsBetweenCheckAndReplay: processOnce() error = quarantine repo dependency lease for 5m0s: replay workload materialization for RUNS_ON readiness: workload materialization fenced replay was not scheduled for scope "scope-gen-a" generation "gen-a" ...
TestRepoDependencyRunsOnFencedReplayStillFailsClosedOnActiveGeneration: unscheduled_fenced_replay_on_active_generation counter = 0, want 1
TestRepoDependencyRunsOnFencedReplaySkipsRetiredGeneration: inactive_generation skip counter = 2, want 1 per request, not per path
```

All of the above pass on the final code.

## What changed

- `RepoDependencyProjectionRunner.GenerationFreshness` is the runtime's own
  `GenerationFreshnessCheck`, wired in `cmd/reducer` from
  `postgres.NewGenerationFreshnessCheck`. The runner builds a
  `repoDependencyGenerationFreshness` for each acceptance-unit cycle, so its
  cache never outlives a cycle and a scope that activates a new generation
  between cycles is checked again. A request whose generation is no longer the
  scope's active generation is skipped. A scope with no active generation or an
  unknown scope reads as current. A newer pending generation
  (`GenerationNotYetActiveError`) is not skipped, because the replay is still
  owed once it activates. Any other lookup error fails the cycle.
- `ReducerQueue.ReplayWorkloadMaterializationOutcome` reports the closed outcome
  `scheduled`, `superseded` or `not_scheduled`. `ReplayWorkloadMaterialization`
  keeps its boolean contract and delegates to it: `pending`, `claimed`,
  `running`, `retrying` and `succeeded` stay true, every other state and a
  missing row stay false, which `TestReducerQueueReplayWorkloadMaterializationOutcome`
  asserts per status for the other caller, the platformfam handler.
- A superseded stable item is nothing to replay only for a retired generation.
  When the replay reports `superseded` the runner re-checks freshness without
  the cache. If the generation has retired since (a supersede landed between the
  check and the replay) the request is skipped. If it is still active, or no
  freshness seam is wired, the cycle fails closed with the same error,
  quarantines the lease, keeps the intents pending, and counts
  `eshu_dp_repo_dependency_generation_anomalies_total{reason="superseded_item_on_active_generation"}`
  with a WARN. A dead-lettered item stays `not_scheduled` and fails closed.
- The RUNS_ON fenced replay is reachable for a retired accepted generation.
  `ensureRunsOnWorkloadReadiness` receives the cycle's active rows, which the
  acceptance row selects (`acceptance_cycle.go`, `processAcceptanceUnit`), not
  the scope's active generation. RUNS_ON rows come from the cross-repo resolver,
  whose gate checks the relationship generation, and recover-generations retires
  only `scope_generations`, so a retired scope generation's RUNS_ON rows stay
  authoritative. The fenced UPDATE
  (`scheduleWorkloadMaterializationFencedReplayQuery`) excludes `superseded`,
  so the replay returned false and the cycle quarantined before it wrote.
  Skipping only the replay would leave `ready=false`, and `BlockedReadiness`
  returns without completing the intents. So a fenced request on a retired
  generation is skipped and dropped from the readiness wait, counted as
  `inactive_generation` with `fenced=true` in the WARN. An unscheduled fenced
  replay on the active generation, which includes a superseded item, still
  fails the cycle.
- Active rows whose accepted generation is retired still project. Before
  `writeActiveRows` they are counted per row as
  `eshu_dp_repo_dependency_generation_anomalies_total{reason="inactive_accepted_generation"}`,
  with one WARN per scope generation per cycle. This changes no behavior.
- Each skipped replay request counts on
  `eshu_dp_repo_dependency_replay_skipped_total{reason="inactive_generation"}`
  and logs one WARN.

- The fenced path mirrors the plain path. When a fenced replay is refused the
  runner re-checks freshness without the cache: a generation that retired since
  the first check is skipped, and the next cycle drops it up front. On the active
  generation it counts
  `eshu_dp_repo_dependency_generation_anomalies_total{reason="unscheduled_fenced_replay_on_active_generation"}`
  and fails closed. The fenced path reports only a boolean, so it cannot tell a
  superseded item from a dead-lettered one and uses one reason for both.
  Only the refinement of a fenced outcome API, to split those two causes, is
  left to follow-up #7673.
- A replay request counts once per scope, generation and entity per cycle on
  `eshu_dp_repo_dependency_replay_skipped_total`, even when a RUNS_ON row
  produces both a fenced and a plain request.

RUNS_ON rows on a retired generation now write where the lane used to quarantine
before writing. The writer is MATCH-only on both endpoints
(`canonicalRunsOnUpsertCypher` in
`go/internal/storage/cypher/canonical_relationships.go`: `MATCH (repo)-[:DEFINES]->(w:Workload)`, `MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)`,
`MATCH (p:Platform {id})`, then `MERGE` of the relationship only). A missing
WorkloadInstance therefore writes no edge and fabricates no node, and the intent
completes with zero edges. Completing the rows without writing them would not be
safer: the repo RUNS_ON retract (`RetractRepoRunsOnEdgesCypher` in
`canonical_relationships.go`, described by the doc comment on the RUNS_ON retract
role in `go/internal/storage/cypher/edge/materialized/repo_dependency.go`) deletes
the repo's RUNS_ON edges by evidence source, so skipping the rewrite could leave
none. With instances present, the edge carries the retired generation's platform
assertion, the same class as every other edge type from a retired accepted
generation, which already projected before this change. Whether such rows should
project at all is follow-up #7673. The `inactive_accepted_generation` counter
makes them visible.

## Performance and concurrency

No-Regression Evidence: the change adds one `generationFreshnessSQL` read per
distinct scope and generation among an acceptance unit's active rows, cached for
that cycle. It runs on every cycle with active rows, including the visibility
count, even with no replayer. A cycle covers one source repository, so the count
is about one to three lookups, one per distinct scope generation. Measured with
read-only `EXPLAIN (ANALYZE, BUFFERS)` of the statement as a prepared statement
on a production-sized database, for a scope with a retired generation as the
intent:

| Run | Execution time | Planning time | Buffers |
| --- | --- | --- | --- |
| 1 | 0.112 ms | 0.582 ms | shared hit=12 |
| 2 | 0.055 ms | 0.363 ms | shared hit=12 |

The plan is three index probes and no sequential scan: `ingestion_scopes_pkey`
(3 buffers), `scope_generations_scope_generation_idx` for the intent generation
(4), and an index-only scan of the same index for the active generation (5, one
heap fetch). A cycle therefore adds well under 1 ms of read time. This is a
plan and latency measurement of the added read, not an end-to-end timing of the
repo_dependency lane; baseline and after lane timings are NOT_CHECKED. The store
change swaps a boolean projection for `SELECT status` on the same single-row
primary-key lookup, and it only runs after the enqueue conflicted. A skipped
request removes one replay UPDATE and one INSERT that would only have
conflicted. The runner takes no new lock and holds its partition lease as
before. A generation that supersedes between the freshness check and the replay
is re-checked without the cache and skipped, on both the plain and the fenced
path.

Observability Evidence: `eshu_dp_repo_dependency_replay_skipped_total` counts
a skipped replay request by bounded `reason` (`inactive_generation`), with the
WARN `repo dependency workload materialization replay skipped: nothing to
replay` (`scope_id`, `generation_id`, `entity_key`, `fenced`, `reason`).
`eshu_dp_repo_dependency_generation_anomalies_total` counts
`superseded_item_on_active_generation` (plain replay only, a fail-closed state:
any nonzero value needs an operator; the fenced RUNS_ON replay reports only a
boolean and counts `unscheduled_fenced_replay_on_active_generation`) and `inactive_accepted_generation` (rows that still project),
with the WARN `repo dependency generation anomaly`. A fail-closed request also
counts on `eshu_dp_shared_projection_lease_quarantines_total{domain="repo_dependency"}`.
Tests: `TestRepoDependencyReplaySkipsSupersededGenerationRequest` (skip counter
increments), `TestRepoDependencyReplayFailsClosedOnSupersededItemOnActiveGeneration`
(error, quarantine, intents pending, anomaly counter),
`TestRepoDependencyReplayStillFailsClosedOnActiveGeneration` (skip counter stays
zero, intents stay pending),
`TestRepoDependencyInactiveAcceptedGenerationRowsAreCountedAndLogged`,
`TestRepoDependencyRunsOnFencedReplaySkipsRetiredGeneration` (intents complete)
and `TestRepoDependencyRunsOnFencedReplayStillFailsClosedOnActiveGeneration`.

Live proof: `TestWorkloadReplayOutcomeReportsSupersededStableItem`
(`reducer_queue_workload_replay_live_test.go`) enqueues a
`workload_materialization` item, sets it `superseded` and asserts the outcome is
`superseded`, the boolean method is false, and the row stays `superseded`. It
skips without `ESHU_POSTGRES_DSN`; the blocking reducer-contention-gate workflow selects it by
name and passes a DSN.

Live run (2026-10-07, native PostgreSQL 18.6, private cluster, `ESHU_POSTGRES_DSN`
exported before the run): `go test -p 2 -count=1 ./internal/storage/postgres/ -run
'^TestWorkloadReplayOutcomeReportsSupersededStableItem$' -v` exit 0,
`--- PASS: TestWorkloadReplayOutcomeReportsSupersededStableItem (0.68s)` after
180 of 180 migrations applied (not skipped). The rest of the file's live proofs
(`-run '^TestWorkload'`, including the replay-during-claim, concurrent-first-schedule,
fenced-token and ack-contention proofs) also exit 0, so the boolean replay contract
holds on real rows.

Guard proof: `TestReducerContentionPostgresProofsRunInTheReducerContentionGate` and
the sibling enrollment guards exit 0. In a throwaway copy, dropping the test name
from the workflow `-run` filter with the pin kept fails the guard with `does not
select TestWorkloadReplayOutcomeReportsSupersededStableItem` (exit 1); dropping it
from both the filter and the pinned list passes (exit 0), so the pin is what makes a
dropped filter entry visible.
