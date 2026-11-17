# #10814: the NVD baseline planner gates on a keyed drain probe

Refs #10814, #10728, #10746. Part of #9209. Ruling:
[10728-baseline-pace-ruling-2026-11-16.md](../arbiter/10728-baseline-pace-ruling-2026-11-16.md)
decision 8 and the #10814 arbiter comment. Design:
[10728-baseline-pacing.md](../design/10728-baseline-pacing.md) section 4. Environment
identifiers are redacted per #7803.

## What this slice ships

The planner half of the drain-gated pace. A year plans its next chunk only after
the last committed chunk has drained, at `max(floor, 30 min / 2^streak)`, and
only while the trip latch is shut and fresh. `NVDBaselineDrainProbe` is the
Postgres read that tells the planner how far a chunk got. There is no migration
and no new index. Nothing in production fills the planner's request fields until
the #10746 enable lane, so none of this runs against a real baseline yet.

## Points the ruling left open, settled from code and a real store

- **Which reducer item belongs to a chunk.** The `supply_chain_impact` item the
  production projection enqueues is keyed by the chunk's own scope and
  generation, so the probe's reducer leg is plain equality
  (`TestNVDBaselineDrainProbeWalksTheRealPipelineLive`: exactly one keyed row,
  found through the real commit, projector claim, and ack path).
- **A superseded named generation.** A same-scope successor commit supersedes the
  generation the checkpoint names. The probe then judges the scope's active
  generation, or holds the year as `projecting` while the successor is pending
  (`TestNVDBaselineDrainProbeFollowsSupersedingGeneration`).
- **What "done" means.** A chunk is done by the scope's active generation and its
  downstream items, not by the workflow row: a completed claim with a pending
  generation is not done, and a done chunk stays done after its workflow row is
  reaped (`TestNVDBaselineDoneIsDerivedFromActivePointer`). The oldest
  not-done chunk reports its age with the phase it is stuck in
  (`TestNVDBaselineOldestNotDoneAgeReportsPhase`). Those two R-B31 tests
  exist in the issue history by name only; their semantics here are read from
  those names and the ruling, which is an open point below.
- **Terminal failure.** A `dead_letter` projector or reducer item reads as
  `failed`, and the production replay puts the chunk back in the phase that
  resumes (`TestNVDBaselineDrainProbeSeesDeadLettersAndReplaysLive`, through the
  real `ProjectorQueue.Fail`, `ReducerQueue.Fail`, and `RecoveryStore`).
- **Unknown statuses.** The open sets name their terminal statuses, so a status
  the query does not know holds the year instead of releasing it
  (`TestNVDBaselineDrainProbeTreatsUnknownStatusesAsOpenLive`).

## The probe adds no index (EXPLAIN proof)

The shipped constant `NVDBaselineDrainProbeQuery` is what the test EXPLAINs, not a
copy. Fixture: `TestNVDBaselineDrainProbePlanUsesKeyedIndexes`, PostgreSQL 18.6
on a local socket, `shared_buffers` 256 MB, ANALYZEd.

| Table | Rows | How it was built |
|---|---|---|
| `fact_work_items` | 52,369 | 31,184 projector items (real `ProjectorQueue.Enqueue`), 11,184 `supply_chain_impact` reducer items (production intent builder, real `ReducerQueue.Enqueue`), 10,000 other-domain reducer items, 1 seeded by bootstrap |
| `workflow_work_items` | 51,184 | 31,184 collector items and 20,000 registry items (real `EnqueueWorkItems`) |
| `scope_generations` | 31,186 | 20,000 git generations, about 11,000 NVD baseline chunks, 184 of them with a same-scope successor |

Scopes and generations are bulk-inserted, and terminal statuses are applied in
bulk afterwards with a production-like skew (mostly terminal, a small live tail,
a few dead letters), because driving tens of thousands of claim and ack cycles
adds nothing to a plan proof. The queue rows themselves come from the real
enqueue paths. Four chunk states, each under a custom plan and a forced generic
plan, three runs:

| Chunk state | Custom: shared buffers, server ms | Generic: shared buffers, server ms |
|---|---|---|
| named generation superseded | 27, 0.086 to 0.097 | 22, 0.063 to 0.111 |
| drained | 23, 0.072 to 0.078 | 18, 0.045 to 0.064 |
| reducer item open | 23, 0.057 to 0.074 | 18, 0.040 to 0.083 |
| scope absent | 9, 0.033 to 0.052 | 9, 0.053 to 0.062 |

Every scan was a keyed index scan, with no Seq Scan and at most one index
search. The test requires each index condition to bind its key columns (a
`Filter` does not count): `fact_work_items_scope_generation_idx` on `scope_id`
and `generation_id`, `workflow_work_items_collector_scope_generation_updated_idx`
with `collector_kind` leading, `scope_generations_active_scope_idx` on `scope_id`.
It also asserts a shared-buffer bound of 64. The whole probe call (begin,
read-only, timeout, query, rollback) measured a 305 microsecond median, 1.0 ms
p99, and 11.6 ms max over 800 calls on that fixture; that figure is logged, never
asserted.

The planner chose two indexes the ruling did not name. It reads the named
generation through the covering `scope_generations_scope_generation_idx` instead
of the primary key, and `fact_work_items` through
`fact_work_items_scope_generation_idx` instead of `_generation_idx`. Both are
keyed on the same columns, so no index is added and the test pins the ones the
planner actually uses.

## Fails closed

| Situation | Result |
|---|---|
| Probe error, nil beginner, blank key | error, never a phase; the year holds as `baseline_drain_unloaded` |
| Statement blocked behind an ACCESS EXCLUSIVE lock | stops at the server statement timeout (SQLSTATE 57014), typed `ErrNVDBaselineDrainTimeout`; the probe works again once the lock is released (`TestNVDBaselineDrainProbeTimesOutUnderLockLive`) |
| Key with no generation behind it | typed `ErrNVDBaselineDrainNotFound`, year left unloaded |
| Phase string the planner does not know | year left unloaded |
| Caller context ends mid-load | loader stops probing, keeps the proven years, joins the error |
| Real probe through the coordinator loader | five phases reach the planner as the matching phase (`TestNVDBaselineDrainProbeFeedsTheCoordinatorLoaderLive`) |

## Mutation check

52 distinct mutants of the production code, each applied alone, run against the
focused tests, and restored. All 52 failed a test. Two rounds: the first gave 44
killed and four not killed (two invalid mutants and two that survived), and each
of the four was fixed in the tests or the mutant before the second round.

| Group | Mutants |
|---|---|
| Pace floor, blank or zero as fast, sub-minimum clamp, exponent saturation, negative streak, streak ignored | 7 |
| Stale latch: check removed, `>` as `>=` at 90 minutes, future-dated sample, zero sample time | 4 |
| Drain gate: ignored, unloaded as drained, failed plans, failed not counted, no-checkpoint year, cycle recorded for an undrained chunk, hold precedence, claim_open as projecting, loader ignores a dead context, loader accepts an unknown phase | 10 |
| Telemetry: plan outcome, hold reason, pace, chunk cycle not recorded; unknown reason label; zero pace; default buckets; non-positive cycle | 8 |
| Probe, hermetic: `collector_kind` dropped, function on a key, `OR`, row lock, non-local timeout, no read-only, no rollback, blank key accepted, SQLSTATE and deadline not classified, failure not outranking an open claim | 11 |
| Probe, live: superseding generation ignored, dead letter not failed, unknown reducer or workflow status as closed, no timeout, `collector_kind` dropped, function on three different key columns, wrong generation on the reducer leg, claim leg missing | 11 |
| Phase vocabulary drifting from the coordinator's | 1 |

The first round's survivors were a function wrapped on a key column that the
EXPLAIN test let through (another index kept the read cheap, so the test now
requires the key columns in the index condition), and a chunk-cycle guard
nothing exercised (now `TestPlanNVDBaselineChunkCycleIgnoresAYearWithNoCommittedChunk`).

## Limits and open points

- At `max_in_flight_years` 1, a second year can mint a chunk while the first
  year's chunk is projecting or paced, because a draining or paced year takes no
  slot (the C1 semantics) and the interval is per year. Only open collector
  claims are capped. If the ruling meant one chunk in the whole downstream
  pipeline, that needs a ruling.
- A year whose last chunk failed downstream shows on the plan span and as a
  hold only when nothing else plans; other years keep planning.
- A checkpoint with no last scope or generation (written before migration 301)
  cannot be keyed, so its year holds as `baseline_drain_unloaded`. No such row
  exists unless a baseline ran on a build older than 301.
- A named generation superseded with no active successor, or a generation
  status of `completed`, reads `projecting`. Storage never writes `completed`
  today.
- A pending same-scope successor does not hold the active named generation; the
  year reads drained until the successor activates.
- A latch sample dated more than 2 minutes ahead of the planner's `now` reads
  stale. That goes past the ruling's wording; it is the same bound the writer
  refuses at, so a stepped clock cannot read fresh for ever.
- The soft-inflow and ETA gauges the ruling names are not built.
- The probe's timeout is set with a read-only transaction and `set_config`, not a
  pool-level setting, so it follows the statement and never a pooled connection.
- Whole-fixture timings are one machine, `fsync` off; they are a shape
  check, not a production latency claim. Nothing here ran against a QA
  environment.

## Evidence markers

No-Regression Evidence: the probe is a new read-only path and changes no worker,
lease, claim, batch, or queue write. The conflict domain is none: it reads
`scope_generations`, `workflow_work_items`, and `fact_work_items` by key in a
read-only transaction with no row locks, so it cannot hold up the claim, ack, or
reducer-lease writers, and only an ACCESS EXCLUSIVE table lock can hold it up,
which it survives by failing closed at its 3 second statement timeout. Load is
at most one probe per incomplete year with a checkpoint per planner tick (25 at
most), and none until #10746 fills the request fields. PostgreSQL 18.6, 52,369
`fact_work_items`, 51,184 `workflow_work_items`, and 31,186 `scope_generations`,
ANALYZEd: every scan is a keyed index scan, 9 to 27 shared buffers and 0.03 to
0.11 ms of server time per execution under custom and forced generic plans,
over three runs, against tables that span thousands of pages.

Observability Evidence: `eshu_dp_nvd_baseline_plan_holds_total{reason}` counts every
held baseline plan under the closed reason set, so `baseline_drain_unloaded`
(the probe could not answer), `baseline_downstream_failed` (a dead-lettered item),
and `baseline_trip_stale` (the hourly sampler stopped) page differently from the
waits. `eshu_dp_nvd_baseline_pace_interval_seconds` shows the interval the
planner last applied, and `eshu_dp_nvd_baseline_chunk_cycle_seconds` the
plan-to-drained time of each drained chunk. The plan span carries
`nvd_baseline_pace_interval_seconds`, `nvd_baseline_downstream_failed_years`, and
`nvd_baseline_drain_unloaded_years` with the closed hold reason, counts only, never
a scope id. At 3 AM an operator reads the hold counter by `reason` and then the
plan span of the held tick; a probe timeout comes back as the typed
`ErrNVDBaselineDrainTimeout` for the #10746 service to log when it loads the drain
map. The telemetry-coverage row for #10728 names all of it.
