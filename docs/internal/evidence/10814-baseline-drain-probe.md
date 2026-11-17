# #10814: the NVD baseline planner gates on a keyed drain probe

Refs #10814, #10728, #10746. Part of #9209. Ruling:
[10728-baseline-pace-ruling-2026-11-16.md](../arbiter/10728-baseline-pace-ruling-2026-11-16.md)
decision 8 and the #10814 arbiter comment. Design:
[10728-baseline-pacing.md](../design/10728-baseline-pacing.md) section 4. Environment
identifiers are redacted per #7803.

## What this slice ships

The planner half of the drain-gated pace. A year plans its next chunk only after
the last committed chunk has drained, at `max(floor, 30 min / 2^streak)`, and
only while the trip latch is shut and fresh. The pace is a run-wide rate: a year
holds one `max_in_flight_years` slot while its claim is open, its last chunk is
still draining or could not be probed, or its last plan advanced the checkpoint
and is younger than the pace, so one slot mints at most one chunk per interval
across all 25 years, newest year first. `NVDBaselineDrainProbe` is the Postgres
read that tells the planner how far a chunk got. There is no migration and no
new index. Nothing in production fills the planner's request fields until the
#10746 enable lane, so none of this runs against a real baseline yet.

## Points the ruling left open, settled from code and a real store

- **Which reducer item belongs to a chunk.** The `supply_chain_impact` item the
  production projection enqueues is keyed by the chunk's own scope and
  generation, so the probe's reducer leg is plain equality
  (`TestNVDBaselineDrainProbeWalksTheRealPipelineLive`: exactly one keyed row,
  found through the real commit, projector claim, and ack path).
- **A superseded named generation.** A same-scope successor commit supersedes the
  generation the checkpoint names. The probe then judges the scope's active
  generation, and reads `projecting` while no generation is active yet
  (`TestNVDBaselineDrainProbeFollowsSupersedingGeneration`).
- **What "done" means.** A chunk is done by the scope's active generation and its
  downstream items, not by the workflow row: a completed claim with a pending
  generation is not done, and a done chunk stays done after its workflow row is
  reaped (`TestNVDBaselineDoneIsDerivedFromActivePointer`). The oldest
  not-done chunk reports its age with the phase it is stuck in
  (`TestNVDBaselineOldestNotDoneAgeReportsPhase`), and the planner's
  `NVDBaselineDrainState` carries that age start as `NotDoneSince`. Those two
  R-B31 tests are named in the #10736 test list with no description, and the
  arbiter folded them into the probe's live tests, so what they check here is
  this slice's reading of the names (an open point below).
- **Terminal failure.** A `dead_letter` projector or reducer item reads as
  `failed`, and the production replay puts the chunk back in the phase that
  resumes (`TestNVDBaselineDrainProbeSeesDeadLettersAndReplaysLive`, through the
  real `ProjectorQueue.Fail`, `ReducerQueue.Fail`, and `RecoveryStore`).
- **Unknown statuses.** The open sets name their terminal statuses, so a status
  the query does not know holds the year instead of releasing it
  (`TestNVDBaselineDrainProbeTreatsUnknownStatusesAsOpenLive`).
- **A pending same-scope successor.** The named generation can be active and
  fully drained while a re-commit of the same chunk scope is still pending, so
  the probe has a leg for any `pending` generation of the scope and reads it as
  `projecting`, with the pending generation's commit time as `NotDoneSince`
  (`TestNVDBaselinePendingSuccessorHoldsADrainedChunkLive`).
- **A failed successor.** The sequence is reachable through the real writers: a
  re-commit supersedes the generation the checkpoint names, then dead-letters in
  the projector, which marks it `failed`. The scope's active pointer was
  already NULL because nothing was active: the failure writer nulls it only when
  the failed generation was the active one. With nothing active or pending, the
  probe finds no effective generation, and its projector leg matches nothing, so
  the chunk read `projecting` for ever and a one-slot run stalled under a wait
  reason with no failure signal. The newest
  leg reads the scope's newest generation, and the chunk reads `failed` when the
  scope has no active generation, no pending generation, and that newest
  generation is `failed`
  (`TestNVDBaselineFailedSuccessorOfASupersededChunkLive`, the sequence through
  the real `Claim`, `Fail`, replay, `Ack`, and reducer). Only the newest
  generation decides, so an older failed generation never does
  (`TestNVDBaselineOnlyTheNewestGenerationDecidesFailedLive`).
  The ruling for this leg said a replay returns the generation to pending. It
  does not: `replayFailedWorkItemsTemplate` resets only the work item, and the
  generation stays `failed` until the replayed item's Ack activates it. Built
  exactly as ruled, a replayed chunk read `failed` until the Ack, which would
  release its slot while its data was in flight, and the older replay live test
  failed. So the leg also requires that the newest generation has no live
  projector item (pending, claimed, running, or retrying), read by its own CTE
  on the same index as the projector leg. The outcomes the ruling lists for a
  replay are unchanged; only the mechanism differs.

## The probe adds no index (EXPLAIN proof)

The shipped constant `NVDBaselineDrainProbeQuery` is what the test EXPLAINs, not a
copy. Fixture: `TestNVDBaselineDrainProbePlanUsesKeyedIndexes`, PostgreSQL 18.6
on a local TCP port, `shared_buffers` 128 MB, `fsync` off, ANALYZEd.

| Table | Rows | How it was built |
|---|---|---|
| `fact_work_items` | 52,905 | 31,452 projector items (real `ProjectorQueue.Enqueue`), 11,452 `supply_chain_impact` reducer items (production intent builder, real `ReducerQueue.Enqueue`), 10,000 other-domain reducer items, 1 seeded by migration 115 |
| `workflow_work_items` | 51,452 | 31,452 collector items and 20,000 registry items (real `EnqueueWorkItems`) |
| `scope_generations` | 31,454 | 20,000 git generations, 11,452 NVD baseline generations (11,000 chunks and 452 same-scope successors: 184 superseded by an active one, 158 pending, 110 failed), 2 genesis rows seeded by migrations 115 and 295 |

Scopes and generations are bulk-inserted, and terminal statuses are applied in
bulk afterwards with a production-like skew (mostly terminal, a small live tail,
a few dead letters), because driving tens of thousands of claim and ack cycles
adds nothing to a plan proof. The queue rows themselves come from the real
enqueue paths. The fixture checks the failed-successor case really has a failed
newest generation before it probes it. Six chunk states, each under a custom plan
and a forced generic plan, three runs:

| Chunk state | Custom: shared buffers, server ms | Generic: shared buffers, server ms |
|---|---|---|
| named generation superseded | 39, 0.282 to 0.467 | 34, 0.116 to 0.256 |
| failed successor | 28, 0.189 to 0.306 | 23, 0.088 to 0.223 |
| pending successor | 43, 0.221 to 0.411 | 35, 0.085 to 0.160 |
| drained | 39, 0.231 to 0.282 | 34, 0.119 to 0.142 |
| reducer item open | 39, 0.328 to 0.528 | 34, 0.102 to 0.115 |
| scope absent | 15, 0.127 to 0.242 | 15, 0.071 to 0.208 |

The buffer counts were identical in all three runs, which were taken at the final
code head. An earlier set of three runs read up to 44 buffers for the
pending-successor case, which is the top of the 15 to 44 range the bound's
comment in the test quotes. The server times are three back-to-back runs on a
shared laptop with other builds running, so they are noisy; read them as an
order of magnitude.

Every scan was a keyed index scan on an allowed index, with no Seq Scan and at
most one index search. The test requires each index condition to bind its key
columns (a `Filter` does not count): `fact_work_items_scope_generation_idx` on `scope_id`
and `generation_id` (three scans: the projector, reducer, and newest-projector
legs), `workflow_work_items_collector_scope_generation_updated_idx` with
`collector_kind` leading, `scope_generations_active_scope_idx` on `scope_id`,
`scope_generations_scope_idx` on `scope_id` and `status` (the pending leg), and
`scope_generations_scope_latest_lookup_idx` on `scope_id` (the newest leg, with
`LIMIT 1`). It also asserts a shared-buffer bound of 96, a little over twice the
worst case measured. The whole probe call (begin, read-only, timeout, query,
rollback) measured a 0.57 to 0.59 ms median, 1.6 to 6.2 ms p99, and 27 to 57
ms max over 1,200 calls per run on that fixture; that figure is logged, never
asserted.

The planner chose two indexes the ruling did not name. It reads the named
generation through `scope_generations_scope_generation_idx` (on `scope_id`, `generation_id`) instead
of the primary key, and `fact_work_items` through
`fact_work_items_scope_generation_idx` instead of `_generation_idx`. Both are
keyed on the same columns, so no index is added and the test pins the ones the
planner actually uses.

## Fails closed

| Situation | Result |
|---|---|
| Probe error, nil beginner, blank key | error, never a phase; the year holds as `baseline_drain_unloaded` |
| Statement blocked behind an ACCESS EXCLUSIVE lock | stops at the server statement timeout (SQLSTATE 57014), typed `ErrNVDBaselineDrainTimeout`; the probe works again once the lock is released (`TestNVDBaselineDrainProbeTimesOutUnderLockLive`) |
| A positive timeout under a millisecond | clamped up to 1 ms, because PostgreSQL reads `statement_timeout` 0 as no timeout and `Duration.Milliseconds` truncated it to 0ms (`TestNVDBaselineDrainProbeNeverDisablesItsStatementTimeout`; `TestNVDBaselineDrainProbeSubMillisecondTimeoutStillBoundsALockedReadLive` shows it still stops a blocked read) |
| A context deadline or SQLSTATE 57014 at any stage: begin, read-only, timeout setting, or the statement | typed `ErrNVDBaselineDrainTimeout` (`TestNVDBaselineDrainProbeClassifiesEveryStageTimeout`; `TestNVDBaselineDrainProbeExpiredContextIsATypedTimeoutLive`) |
| Key with no generation behind it, and no failed newest generation to judge | typed `ErrNVDBaselineDrainNotFound`, year left unloaded |
| Phase string the planner does not know | year left unloaded |
| Caller context ends mid-load | loader stops probing, keeps the proven years, joins the error |
| Real probe through the coordinator loader | five phases reach the planner as the matching phase (`TestNVDBaselineDrainProbeFeedsTheCoordinatorLoaderLive`) |

## Mutation check

A harness applied 150 hand-made mutants to the production code one at a time at
commit 7c031ba45e. A mutant applies only when its target text occurs exactly
once. For each one it ran the tests of the package and read the result: a mutant
is killed when a test fails and survives when none does. They cover the drain
probe's query and phase derivation, its timeout and typed errors, the slot rule,
the pace, the held-years gauge, the pending-successor leg, and the
failed-successor leg. 144 were killed on the first pass and six were not:

| Mutant | Why it was not killed | Outcome |
|---|---|---|
| The guard that skips a chunk cycle when the drain time is unknown (C5c) | Equivalent. `cycle <= 0` already covers a zero drain time. | Kept, no test can tell them apart |
| The failed case moved above the pending case (PA14) | Equivalent. The failed leg already requires that no generation is pending, so the order cannot change a result. | Kept |
| The "effective generation not active" step turned off (N3) | Its test filter was the three live-walk tests, and the pending leg now answers the pending case before that step. | Killed under the full probe test set (`TestNVDBaselineFailedSuccessorOfASupersededChunkLive`, `TestNVDBaselineDrainProbeSeesDeadLettersAndReplaysLive`, and the phase table) |
| The drain gate's default for an unknown phase released the slot (SL14) | A real gap: nothing reached that return. | `TestNVDBaselineYearLaneGateHoldsASlotForAnUnknownPhase` kills it |
| The retry-wait count read as the drain-unloaded count (HY3) | A real gap: the end-to-end test held one year under each reason. | `TestWorkPlannerRecordsHeldYearsEveryPlan` now holds 1, 2, and 3 years and kills it |
| A held year can still mint (SL15) | Invalid: the edit left a variable unused, so the package did not compile. | Repaired and killed |

With the two tests added after the pass (the commit that follows 7c031ba45e),
148 of 150 are killed and the other two are equivalent. The mutants the
failed-successor ruling asked for all fail tests: the no-pending condition
dropped (`TestNVDBaselineDrainProbeDerivesThePhaseOfTheLastChunk`), the
no-active condition dropped (same), the status-only leg without "newest"
(`TestNVDBaselineOnlyTheNewestGenerationDecidesFailedLive`), the liveness
conjunct dropped (`TestNVDBaselineFailedSuccessorOfASupersededChunkLive` and the
replay live test), and the liveness conjunct flipped (those and the phase
table). The older of two pending generations winning, and the pending leg
reading the newest one, are killed by the phase table and by
`TestNVDBaselinePendingSuccessorHoldsADrainedChunkLive`.

## Limits and open points

- The pace is a run-wide rate (the slot rule), so one slot mints at most one
  chunk per interval (`TestPlanNVDBaselineRateIsOneChunkPerPaceIntervalPerSlot`,
  `TestPlanNVDBaselineRateFollowsTheQuietStreak`). It corrects the walk the
  first slice shipped, which paced each year on its own and could mint up to 25
  chunks per interval at one slot. A chunk that commits facts but does not
  advance its checkpoint releases its slot (the year reads as retry wait), and
  the probe, which reads the checkpoint's last scope, cannot see that chunk's
  downstream work. A systemic fault of that kind can still mint one chunk per
  year per 30 minutes (`TestPlanNVDBaselineNonAdvancingFaultMintsOnePerYearPerRetryWait`);
  the trip latch is the backstop.
- Where the stored state overlaps, the drain gate reads before retry wait: a
  year in retry wait whose last chunk is still draining keeps its slot and does
  not mint. The ruling's rule does not say which wins; this is this slice's
  reading, and it holds in the safe direction.
- An open claim on a complete year still holds its slot, and an open-claim year
  is counted once. A last plan dated ahead of the tick clock reads as younger
  than the pace and holds a slot.
- A stored position that cannot mint a scope id on any incomplete, unclaimed
  year returns an error once the plan passes its trip gates, because the planner
  cannot tell whether that year's last plan advanced.
- A year whose last chunk failed downstream releases its slot, so other years
  keep planning. It shows on `eshu_dp_nvd_baseline_held_years{reason}` (every
  plan, zero included), on the plan span, and as a hold only when nothing else
  plans. The gauge is skipped for a disabled or config-error plan, which compute
  no year counts, and recorded for trip-held and complete plans. Its `reason`
  values are the hold reasons without the `baseline_` prefix; a parity test ties
  the two spellings together.
- A checkpoint with no last scope or generation (written before migration 301)
  cannot be keyed, so its year holds as `baseline_drain_unloaded` and keeps its
  slot; at one slot that stalls the run, which is intended and loud (the hold
  reason and the gauge show it). No service has planned a baseline chunk yet, so
  no such row should exist, but that has not been checked in any environment.
  There is no in-place repair in this goal: disable the lane and start a new
  `baseline_run_id`. The ruling adds a count of such rows to the #10817 enable
  preflight reads:

  ```sql
  SELECT count(*) AS blank_key_rows,
         count(*) FILTER (WHERE NOT year_complete) AS blank_key_incomplete_rows
  FROM vulnerability_nvd_baseline_checkpoints
  WHERE btrim(last_scope_id) = '' OR btrim(last_generation_id) = '';
  ```

  Only the incomplete rows stall a run, because a complete year is not probed.
  The query was run on a scratch PostgreSQL database built from migrations 297
  and 301, over seven rows (blank, whitespace-only, one-sided, and keyed; complete
  and incomplete), and returned 5 and 4.
- A generation status of `completed` reads `projecting`, and so does a superseded
  named generation whose scope has a newest generation of any status other than
  `failed`. The ruling found no writer of `completed` to `scope_generations`
  (unverified here); the direction is a hold.
- A `pending` generation that nothing activates or fails holds its chunk until it
  leaves `pending`, including one that never activated.
- The checkpoint moves its last generation only when a commit reaches or passes
  the stored position, so a late re-commit that ends earlier is what leaves it
  naming a superseded generation. That is the case the failed-successor leg
  covers.
- A latch sample dated more than 2 minutes ahead of the planner's `now` reads
  stale. That goes past the ruling's wording; it is the same bound the writer
  refuses at, so a stepped clock cannot read fresh for ever.
- Not built here, and where: the soft-inflow gauge (the existing
  `eshu_dp_nvd_baseline_dirty_proxy_rows_per_hour`, recorded by the hourly
  sampler), the ETA gauge (it needs the sampler's six-hour history), and the
  not-done age gauge that reads `NotDoneSince` all belong to #10746. The probe
  emits no signal of its own (no span, metric, or log) for the same reason, and
  the WARN on a change of the held-years gauge goes with it. Whether the
  dirty-proxy gauge is the CVE-keyed inflow the streak folds is unverified.
- The two R-B31 tests are named in #10736 with no description; what they check
  here is this slice's reading of the names.
- The probe's timeout is set with a read-only transaction and `set_config`, not a
  pool-level setting, so it follows the statement and never a pooled connection.
  For #10746: run the loader under the tick's deadline, because 25 probes at the
  3 second timeout is 75 seconds worst case.
- Whole-fixture timings are one machine, `fsync` off; they are a shape
  check, not a production latency claim. Nothing here ran against a QA
  environment, and the read-only EXPLAIN on QA stays owed to the #10817 preflight.

## Evidence markers

No-Regression Evidence: the probe is a new read-only path and changes no worker,
lease, claim, batch, or queue write. The conflict domain is none: it reads
`scope_generations`, `workflow_work_items`, and `fact_work_items` by key in a
read-only transaction with no row locks, so it cannot hold up the claim, ack, or
reducer-lease writers, and only an ACCESS EXCLUSIVE table lock can hold it up,
which it survives by failing closed at its 3 second statement timeout. Load is
at most one probe per incomplete year with a checkpoint per planner tick (the
baseline's year count, 25 for a 2026 run), and none until #10746 fills the
request fields. PostgreSQL 18.6, 52,905
`fact_work_items`, 51,452 `workflow_work_items`, and 31,454 `scope_generations`,
ANALYZEd: every scan is a keyed index scan, 15 to 43 shared buffers and 0.07 to
0.53 ms of server time per execution under custom and forced generic plans,
over three runs, against tables that span thousands of pages.

Observability Evidence: `eshu_dp_nvd_baseline_plan_holds_total{reason}` counts every
held baseline plan under the closed reason set, so `baseline_drain_unloaded`
(the probe could not answer), `baseline_downstream_failed` (a dead-lettered item,
or a failed newest generation of the chunk's scope), and `baseline_trip_stale`
(the hourly sampler stopped) page differently from the waits. The counter fires
only when nothing mints, so `eshu_dp_nvd_baseline_held_years{reason}` carries the
years the stored state holds while other years keep planning: it records
`downstream_failed`, `drain_unloaded`, and `retry_wait` on every plan, zero
included, so a recovered year clears. `eshu_dp_nvd_baseline_pace_interval_seconds`
shows the interval the planner last applied, and
`eshu_dp_nvd_baseline_chunk_cycle_seconds` the plan-to-drained time of each
drained chunk. The plan span carries `nvd_baseline_pace_interval_seconds`,
`nvd_baseline_downstream_failed_years`, `nvd_baseline_drain_unloaded_years`, and
`nvd_baseline_retry_wait_years` with the closed hold reason, counts only, never a
scope id. At 3 AM an operator reads the held-years gauge and the hold counter by
`reason` and then the plan span of the held tick; a probe timeout comes back as
the typed `ErrNVDBaselineDrainTimeout` for the #10746 service to log when it loads
the drain map. The telemetry-coverage row for #10728 names all of it.
