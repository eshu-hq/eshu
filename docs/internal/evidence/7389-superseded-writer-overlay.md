# #7389: a superseded or refused writer leaves its overlay in the graph

## Problem

A git delta generation is diffed from the scope's active commit. Two deltas
diffed from the same active commit A (G_B = diff(A,B), G_D = diff(A,D)) can be
in flight together. The projector heartbeat superseded G_B after G_B had already
written its overlay to the canonical graph and the content store, because a
newer pending generation (G_D) existed. G_B never activated, so the active
commit stayed A, and G_D passed the #7319 delta-baseline fence and activated.
diff(A,D) never names the paths that changed between A and B but not between A
and D, so the graph kept G_B's overlay: a stale node (x.go, new in B) and a
missing node (w.go, deleted in B, unchanged between A and D). An Ack-phase
#7319 refusal left the same kind of overlay. Nothing healed it before the
24-hour reconciliation sweep.

Root-Cause Evidence: the live reproduction
`TestSupersededDeltaOverlaySurvivesNextDeltaLive` and
`TestDeltaRefusedAtAckLeavesOverlayLive`
(`go/cmd/projector/superseded_delta_overlay_live_test.go`) drove the production
projector Service, the #7319 fence, the ingestion commit path and the Neo4j
canonical writer against PostgreSQL 18.6 and Neo4j community 2026.08.1. Before
this change, 3 of 3 runs of each sub-path ended with `gen-b status=superseded
ever_activated=false`, `gen-d status=active baseline=a`, and graph files
`[keep.go x.go y.go]` where `[keep.go w.go y.go]` was expected; a full
generation at D reached the expected set, so the harness was sound.

## Ruling implemented

The arbiter's final binding ruling for #7389, implemented as written:

- Migration 149 adds nullable `scope_generations.projection_write_started_at`
  (catalog-only, no default, backfill, CHECK or index). Migration 150 sets
  `scope_generations` `fillfactor = 90` (SHARE UPDATE EXCLUSIVE, catalog-only,
  new pages only).
- `ProjectorQueue.MarkProjectionWriteStarted` sets the marker to the latest
  write start (`markProjectionWriteStartedQuery`, `GREATEST(COALESCE(...), $5)`,
  on a pending, active or failed generation), locking only its own generation row,
  lease-fenced by an EXISTS on the work row that takes no lock, under Ack's
  lock_timeout (`ackLockTimeoutSetting`, 2 s default). A lock timeout returns
  `failure.ErrWorkWriteMarkerDeferred` and is re-run; no row returns
  `failure.ErrWorkSuperseded` (generation superseded, completed or gone, or
  work row superseded) or `ErrProjectorClaimRejected` (claim lost), decided by
  `writeMarkerRefusal`.
- Every projection loop takes the marker as a required dependency
  (`projector.Service.WriteMarker`, `drainProjector`'s `writeMarker`) and runs
  `projector.MarkProjectionWriteStarted` after `LoadFacts` and before
  `Runner.Project`. A per-binary wiring test fails when it is not wired
  (projector, ingester, bootstrap-index).
- `supersedeRunningProjectorWorkQuery` locks the scope row, then the work row
  (`locked_work`, the full lease fence), then the generation row
  (`locked_generation`), each `FOR NO KEY UPDATE ... SKIP LOCKED`. The
  generation CTE carries the full supersede predicate: own generation
  superseded (#7130, unconditional), or pending/active with
  `projection_write_started_at IS NULL` and a newer pending/active generation.
  The work UPDATE joins both locked rows. The claim this makes, and no more:
  the heartbeat never abandons a started projection write to a newer
  generation. Every other retirement of a started write (the claim path's
  stale-generation supersede, Ack's obsolete supersede, a dead letter, an Ack
  refusal) is healed by a forced full snapshot.
- `IngestionStore.UncoveredProjectionWriters` (`uncoveredProjectionWritersQuery`)
  returns superseded or failed generations that wrote and never activated,
  whose write started after the newest activated full generation's write
  start (fail closed to `-infinity` when that full has no write start). The git
  collector's reconciliation decision consults it (see "Review round" below):
  a non-empty result is the `graph_dirty` reconcile reason, which forces a
  reconciliation snapshot.
- The heartbeat ERROR log (`projector lease heartbeat failed`, `bootstrap
  projector lease heartbeat failed`) no longer fires for
  `failure.ErrWorkSuperseded`, which the loop logs at INFO.

Why the gate is on a locked row: under Read Committed, EvalPlanQual rechecks
only the rows a statement locks or updates, against their newest version, and
reads every other row from the statement snapshot. The first design put the
marker check on the unlocked joined `current_generation` row, and a marker
that committed after the heartbeat's snapshot was invisible to it, so both won.

Lock order (documented on `supersedeRunningProjectorWorkQuery` and in
`ProjectorQueue.Ack`): the heartbeat takes no lock it can wait on (scope, work,
generation, all SKIP LOCKED); every blocking path takes the scope row first;
the delta-baseline refusal and Ack share work-then-generation. The marker
locks only its own generation row. The first shipped heartbeat locked the
generation before the work row, and that deadlocked with the refusal (see
"Review round").

## Interleave proof

Harness shim (arbiter's Shim A, `harness/interleave.go`, 1,000 iterations,
marker vs heartbeat supersede on one generation, simultaneous or either side
delayed 0-400 us, PostgreSQL 18.6):

```text
one-predicate gate (refuted):
pg_stat_database.deadlocks before=0 after=0 delta=0
class marker=1 supersede=0 : 431
class marker=1 supersede=1 : 341
class marker=0 supersede=1 : 228

locked-generation gate (shipped):
pg_stat_database.deadlocks before=0 after=0 delta=0
class marker=1 supersede=0 : 677
class marker=0 supersede=1 : 323
final marker=1 supersede=0 -> gen_status,pws_set,work_status=pending,true,running : 677
final marker=0 supersede=1 -> gen_status,pws_set,work_status=superseded,false,superseded : 323
ungranted-lock-sample marker | transactionid | ShareLock |  : 139
safety violations=0
```

The ported test, `TestProjectorHeartbeatWriteMarkerInterleave`
(`go/internal/storage/postgres/projector_heartbeat_write_marker_interleave_live_test.go`),
drives the production `ProjectorQueue.MarkProjectionWriteStarted` and
`supersedeRunningWork` from two sessions, N = 1,000:

```text
RED, pre-change heartbeat (no gate):       class marker=0 supersede=1 : 798
                                           class marker=1 supersede=1 : 202
RED, scratch one-predicate gate:           class marker=0 supersede=1 : 812
                                           class marker=1 supersede=0 : 60
                                           class marker=1 supersede=1 : 128
GREEN, locked gate:                        class marker=0 supersede=1 : 812
                                           class marker=1 supersede=0 : 188
GREEN, locked gate, -race:                 class marker=0 supersede=1 : 839
                                           class marker=1 supersede=0 : 161
every run: deadlocks delta=0; GREEN final states only
(pending,true,running) and (superseded,false,superseded)
```

`TestProjectorWriteMarkerHeartbeatOrderingLive` pins each ordering: a marker
held uncommitted makes the heartbeat a no-op without waiting (50 ms
lock_timeout, SKIP LOCKED) and the committed marker keeps it a no-op; a
supersede committed first makes the marker match no row; a stale attempt marks
nothing; the latest write start is kept and never moves back; a lock wait past the timeout is a
deferral. `TestProjectorHeartbeatWriteGateRowsLive` runs gate cases T1-T4 and
the active re-projection case, and checks from a second session that the
steady-state heartbeat locks no generation row.

## Measurements

Local, PostgreSQL 18.6 (postgres:18-alpine, aarch64), 1,002,001
`scope_generations` rows (2,000 scopes x 500 generations plus one hot scope with
2,000 generations, 1,500 newer than its last activated full), 202,002
`fact_work_items` rows. Heartbeat supersede, `EXPLAIN (ANALYZE, BUFFERS)`
generic-plan execution time, 501 interleaved rounds, BEGIN/ROLLBACK each.

One-predicate gate (`sup_mod`, refuted for correctness):

```text
case   stmt      median_ms     min_ms     p90_ms  med_buf   rows
T1hot  sup_cur      0.0890     0.0730     0.1080       28      0
T1hot  sup_mod      0.0890     0.0720     0.1040       28      0
T1hot  delta_median=+0.0%
T1typ  sup_cur      0.0810     0.0660     0.0980       24      0
T1typ  sup_mod      0.0820     0.0660     0.0990       24      0
T1typ  delta_median=+1.2%
T2     sup_cur      0.1800     0.1480     0.2140       71      1
T2     sup_mod      0.1810     0.1470     0.2170       71      1
T2     delta_median=+0.6%
T3     sup_cur      0.1900     0.1470     0.2210       72      1
T3     sup_mod      0.0730     0.0590     0.0850       21      0
T3     delta_median=-61.6%
T4     sup_cur      0.1680     0.1320     0.2020       65      1
T4     sup_mod      0.1690     0.1380     0.2110       65      1
T4     delta_median=+0.6%
T1hot client-wall 1000x interleaved: cur p50=0.179ms p99=0.315ms min=0.141ms | mod p50=0.180ms p99=0.360ms min=0.140ms | delta_p50=+0.6%
T1typ client-wall 1000x interleaved: cur p50=0.172ms p99=0.316ms min=0.137ms | mod p50=0.172ms p99=0.362ms min=0.133ms | delta_p50=+0.0%
```

Locked-generation gate (`sup_lck`, shipped):

```text
case   stmt      median_ms     min_ms     p90_ms  med_buf   rows
T1hot  sup_cur      0.0870     0.0760     0.1040       28      0
T1hot  sup_lck      0.0990     0.0870     0.1160       28      0
T1hot  delta_median=+13.8%
T1typ  sup_cur      0.0780     0.0690     0.0930       24      0
T1typ  sup_lck      0.0920     0.0820     0.1100       24      0
T1typ  delta_median=+17.9%
T2     sup_cur      0.1740     0.1500     0.2070       71      1
T2     sup_lck      0.2090     0.1810     0.2470       82      1
T2     delta_median=+20.1%
T3     sup_cur      0.1840     0.1470     0.2180       73      1
T3     sup_lck      0.0850     0.0750     0.1030       21      0
T3     delta_median=-53.8%
T4     sup_cur      0.1640     0.1390     0.2010       65      1
T4     sup_lck      0.1840     0.1630     0.2140       70      1
T4     delta_median=+12.2%
T1hot client-wall 1000x interleaved: cur p50=0.184ms p99=0.341ms min=0.144ms | mod p50=0.192ms p99=0.344ms min=0.153ms | delta_p50=+4.3%
T1typ client-wall 1000x interleaved: cur p50=0.168ms p99=0.298ms min=0.136ms | mod p50=0.175ms p99=0.308ms min=0.145ms | delta_p50=+4.2%
```

T1 is the steady-state heartbeat (newest generation, nothing newer), T2 fires
the supersede, T3 is a marked generation with a newer pending one (the old
statement superseded it; the new one does not), T4 is an already-superseded
own generation.

Write-start marker, first marker on 300 freshly inserted pending rows, no
index on the column:

```text
without fillfactor: exec median=0.0620ms p99=0.3650ms max=0.8970ms | buffers median=29 p99=32 max=98 | dirtied median=1 | n_tup_upd,n_tup_hot_upd before=[656 | 201] after=[956 | 201]
fillfactor 90:      exec median=0.0320ms p99=0.0950ms max=0.2560ms | buffers median=7 p99=7 max=76 | dirtied median=0 | n_tup_upd,n_tup_hot_upd before=[956 | 201] after=[1256 | 501]
```

With fillfactor 90 all 300 marker updates were HOT (0 of 300 without it).
Existing pages are not rewritten; the heap grows by at most about 10% as old
pages turn over.

Uncovered-writer probe (`probe_exact`, no index):

```text
hot scope rows=205 | server p50=0.4420ms p99=0.5870ms max=0.6250ms | client-wall p50=0.641ms p99=0.846ms | buffers p50=2119
typical scope rows=0 | server p50=0.0950ms p99=0.1840ms max=0.2290ms | client-wall p50=0.183ms p99=0.324ms | buffers p50=486
```

The shipped probe adds a `LEFT JOIN LATERAL` for the projector work row's
`failure_class`, which runs only for returned rows (zero in steady state).

## +5% bar waiver

Arbiter ruling (#7389, round 2, re-affirmed after the remote runs): the +5%
bar stays waived for the heartbeat supersede at the shipped
work-then-generation SKIP LOCKED shape. The cost is +28% to +37% of the
pre-#7389 statement cost on the T1 cases:

- **Local:** +23 to +29 us.
- **Remote validation host:** +90 to +99 us, and up to +148 us on T2. Every
  call there costs about 3.5 times the local cost. These figures come from
  the shipped constants at both SHAs, with a same-host A/A control at or
  below 4 us (see
  [7389-remote-no-regression.md](7389-remote-no-regression.md)).

The statement runs only on a heartbeat tick (lease/3: 20 s for the projector,
60 s for the ingester), at most once per tick per running item. This closes
the "not re-measured on the shipped constant" item.

The cost comes from locking the work row on every heartbeat, and that lock is
required. The generation-first order deadlocked (40P01) with the
delta-baseline refusal, and the one-predicate shape lost 341 of 1,000
interleaved races. The heartbeat takes only SKIP LOCKED locks, so it cannot
join a wait cycle, and contention with Ack, Fail and retry is
sub-millisecond.

The waiver covers this statement's cost only. It does not waive the
end-to-end no-regression run on the built binary; that run is now done and
recorded in the remote file. The round-1 ruling waived +10 to +35 us for the
earlier generation-then-work shape.

## Follow-up ruling: replayed failed generations and the latest write start

Finding: the first ruled marker accepted only `pending` and `active`. A
dead-lettered projector row leaves its generation `failed`, and replay (the
operator replay or the poison auto-retry) makes the row claimable again; Ack
activates a `failed` generation (`status <> 'superseded'`). The marker refused
it, the worker dropped the item without touching the work row, and the lease
expired and was reclaimed forever. Observed on a live scratch run:

```text
marker on replayed failed generation: err=mark projection write started: generation gen-f: projector work superseded (failure_class=projector_write_marker_generation_retired) work_status_after=running generation_status=failed
```

The arbiter's follow-up ruling, implemented:

- The marker accepts `status IN ('pending', 'active', 'failed')`.
  `writeMarkerRefusal` treats exactly superseded, completed and a missing row
  as retired (ErrWorkSuperseded with the work row's class, or
  `projector_write_marker_generation_retired`), a superseded work row as
  ErrWorkSuperseded with its class, and every other state (pending, active or
  failed and not owned) as a lost claim.
- The marker records the latest write start,
  `GREATEST(COALESCE(projection_write_started_at, $5), $5)`, instead of
  keep-first. Counterexample to keep-first: G_F writes at t1 and dead-letters;
  full G_X writes at t2 > t1 and activates; an operator replays G_F at t3 > t2.
  With keep-first the marker stays t1, G_F writes over G_X's tree and fails
  again, and the probe compares t1 < t2 and reports the scope clean. This
  sequence is unreachable today: the newer full's Ack retires the
  dead-lettered generation (Ack's obsolete supersede), and the replay fence
  skips superseded rows. `GREATEST` is kept as the invariant-preserving form
  (the marker is the latest write start whatever the replay path), and
  `TestReplayAfterFullRecordsLatestWriteStartLive` seeds the state directly.
  The heartbeat gate (`IS NULL`) and the interleave proof depend only on
  whether the marker is set, so they are unaffected.
- Accepted residual: markers come from `q.now()` on the writing host, so
  comparing write starts across generations assumes the clock skew between
  projector hosts is smaller than one projection's duration (not measured).
  `clock_timestamp()` was rejected.

RED then GREEN (live Postgres 18.6):

```text
pre-ruling marker SQL:
  TestReplayedFailedGenerationWritesAndActivatesLive/operator_replay     FAIL MarkProjectionWriteStarted(replayed failed generation) = ... claim lost
  TestReplayedFailedGenerationWritesAndActivatesLive/poison_auto_retry   FAIL (same)
  TestReplayAfterFullRecordsLatestWriteStartLive                          FAIL (marker refused)
  TestWriteMarkerStatusAndSetDerivedFromShippedQuery                      FAIL status list ('pending', 'active')
failed accepted, keep-first SET:
  TestReplayAfterFullRecordsLatestWriteStartLive  FAIL UncoveredProjectionWriters() = []; want [gen-f]
                                                  FAIL marker stayed at t1 (want t3)
old classifier logic:
  TestWriteMarkerRefusalClassification            FAIL superseded_owned_keeps_work_class, missing_row
shipped:
  all of the above PASS, also under -race
```

## Consequences recorded by the ruling

- A marker that is set: a re-projection of the active generation whose marker
  is already set is no longer supersedable by a newer pending generation; it
  runs to Ack. `DeltaBaselineAlreadyActive` proceeds and activation is
  idempotent. This is intended.
- A preflight refusal (`eshu_dp_projector_delta_baseline_fence_total{phase=
  "preflight",outcome="refused_active_differs"}`) is now expected when a newer
  generation arrives while an older one is writing: the older one activates
  and the stale delta is refused before it writes. An Ack refusal is still a
  breach.
- Residual, pre-migration only: for an active generation with a NULL marker, a
  supersede that commits between the marker's snapshot and its row lock still
  passes the marker's snapshot-read EXISTS. The worker then rewrites the active
  generation's own tree and its Ack returns `ErrProjectorClaimRejected`. That
  wastes the attempt and leaves no dirty graph.
- Follow-up, not implemented (ruling D): retention can prune an uncovered
  writer. Retention deletes superseded generations older than 168 h, keeping
  the 24 newest per scope, and never deletes failed ones. The heal is a forced
  full, and the reconcile throttle bounds how fast it can repeat: a pending
  heal full holds the scope for up to Interval (24 h by default), and a heal
  full that is superseded or fails before activation backs the scope off
  Interval/4 (6 h). The claim path supersedes a pending full when a newer
  delta arrives before it is claimed, so a hot repo under projector backlog
  can repeat the heal only in 6 h steps, not "at the next sync". Losing the
  marker therefore needs either the ingester down for more than 7 days or
  every heal full in that window being superseded or failing (about 28
  consecutive 6 h attempts at the defaults; a hot repo exhausts the 24-newest
  keep quickly, so the 168 h age gate is the only protection). The path that
  supersedes the pending full is branch 1 of `supersedable_projector_generations`
  in `projector_queue_claim_sql.go`. The dirty state is visible through the
  `git_delta_baseline_graph_dirty` WARN and the `reconcile_in_flight` and
  `reconcile_retry_backoff` suppression counters. Letting a
  reconcile or heal full survive a newer delta in claim maintenance is the
  tracked design follow-up (#7447 item 7; retention is item 4).
- The content store keeps a superseded writer's `content_files` rows (for
  example x.go at commit b) after a full sweep until retention reaps them; the
  graph is correct.
- A marked writer is released only by its own completion, its own failure, or
  a lease expiry after its process dies: the heartbeat no longer supersedes it.
  On Neo4j `ESHU_CANONICAL_WRITE_TIMEOUT` defaults to unbounded, so a hung
  canonical write with a live heartbeat freezes that scope's freshness until
  the process is restarted (bounded by follow-up #7447 item 3). Runbook: a scope whose
  projector work stays `running` with a set marker and no progress logs is
  cleared by restarting the worker; the lease then expires and a later attempt
  re-projects.

## Verification

- `TestSupersededDeltaOverlaySurvivesNextDeltaLive` (both sub-paths) and
  `TestDeltaRefusedAtAckLeavesOverlayLive`, rewritten to the new contract with
  the same gated interleavings: G_B is not superseded and activates at B (graph
  `[keep.go x.go]`), G_D is refused at preflight with
  `projector_delta_baseline_mismatch`, and G_E = diff(B,D) reaches `[keep.go
  w.go y.go]`; after an Ack refusal `UncoveredProjectionWriters` returns G_B and
  a full generation at C converges and clears it. 3 of 3 local runs green on
  Neo4j community 2026.08.1; RED (all three) with the marker disabled and the
  pre-change heartbeat statement restored.
- `TestUncoveredProjectionWritersLive`: dirty writer, covered by a later full,
  pending excluded, failed included, activated writer excluded, pre-migration
  or missing full fails closed.
- `TestReplayedFailedGenerationWritesAndActivatesLive` (operator replay and
  poison auto-retry) and `TestReplayAfterFullRecordsLatestWriteStartLive`.
- `TestGraphDirtyForcesReconcileAtActiveHead`,
  `TestGraphDirtyRespectsReconcileThrottle`,
  `TestGraphDirtyCountsAgainstTheReconcileBudget`,
  `TestGraphDirtyLookupErrorKeepsTheNormalPath`,
  `TestReconcilePolicyDecideGraphDirty` (collector), and
  `TestProjectorHeartbeatNeverDeadlocksWithBaselineRefusal` (lock order).
- Hermetic: `TestServiceWriteMarkerOutcomes`,
  `TestServiceMarksWriteStartAfterLoadFactsBeforeProject`,
  `TestServiceRequiresWriteMarker`, `TestServiceHeartbeatSupersedeIsNotAnError`,
  `TestWriteMarkerStatusAndSetDerivedFromShippedQuery`,
  `TestWriteMarkerRefusalClassification`,
  `TestDrainProjectorWriteMarkerRefusalWritesNothing`,
  `TestBootstrapHeartbeatSupersedeIsNotAnError`, the prefix guard
  `TestSupersedeRunningGateProbeIsPrefixOfShippedQuery` and
  `TestSupersedeRunningGateLocksGenerationWithFullPredicate`.

No-Regression Evidence: local shim measurements above on the 1,002,001-row
fixture. The heartbeat supersede adds +23 to +53 us per heartbeat per running
item (waived bar, re-affirmed). The marker costs one statement per attempt
(median 0.032 ms, 7 buffers, HOT with fillfactor 90), and the probe one read
per git sync per scope (p99 0.587 ms on the hot scope). No worker count,
batch size, lease duration or claim statement changed.

The bounded remote runs on the built binary are done and recorded in
[7389-remote-no-regression.md](7389-remote-no-regression.md), with audits in
[7389-remote-no-regression-audits.md](7389-remote-no-regression-audits.md):
- Pair 1 (steady workload): no regression, and the fix was not exercised.
- Pair 2 (deterministic burst workload): PASS under the arbiter's fallback
  gates, with 5 race-refused bursts on HEAD. It measures race-path timing,
  rates and cost under the change; it does not demonstrate the fix's
  differential at runtime.
- A first-slot BASE control shows that the ingester difference is the run
  slot.

Observability Evidence: `eshu_dp_collector_reconciliation_full_snapshots_total{reason="graph_dirty"}`
with the `git_reconcile_forced` log, and the `git_delta_baseline_graph_dirty`
WARN log (scope_id, generation ids, statuses, failure classes,
projection_write_started_at, decision reason); the
`projector write marker waiting for busy generation row` WARN (first deferral
and every 30th);
`eshu_dp_projector_delta_baseline_fence_total{phase,outcome}`;
`eshu_dp_superseded_generation_fence_total{failure_class}`; the projector
`projector work superseded by newer generation` INFO log with `fact_count` and
`failure_class`; the `projector lease heartbeat failed` ERROR log, which no
longer fires for a routine supersede.

Rollback and decision signals:

- The `refused_active_differs` rate (phase preflight) must not exceed a scope's
  generation commit rate.
- `graph_dirty` reconciliations stay near zero unless matched by an Ack
  refusal (`phase=ack`), a dead letter, or a lease expiry.
- `graph_dirty` with a writer `failure_class =
  projector_superseded_by_newer_generation` is the retry-then-newer path (a
  marked pending generation whose work was retrying is retired by the claim
  path or Ack's obsolete supersede when a newer generation arrives). It is the
  expected most common residual; a rate that tracks the retry rate is normal,
  one well above it is not.
- The supersede INFO log with `fact_count > 0` stays near zero after the
  change; a marker refusal logs `fact_count = 0`.
- The heartbeat ERROR count is zero for routine supersede.

## Review round (#7389 F1-F7)

The review-round record moved to
[7389-superseded-writer-overlay-review-rounds.md](7389-superseded-writer-overlay-review-rounds.md)
to keep this file under the Markdown line cap.

## Remote runs and follow-ups

The remote runs, their manifests and the declarations the arbiter required
are in [7389-remote-no-regression.md](7389-remote-no-regression.md):
- the race-path cost on either side;
- the base drift record for #7455;
- the first-slot harness caveat;
- the reducer follow-up #7458: a search-document item never re-checks its
  generation after claim. It is out of scope, because this change touches no
  reducer or search file.

Tracked follow-ups: #7447 items 1, 3 and 7, plus the retention item 4; #7458.

## Not checked

- NOT_CHECKED: a heartbeat tick meeting a started write with a newer
  generation pending, at runtime on the remote host. It rests on the local
  live Neo4j proofs above; see the remote file for what the runs did cover.
- NOT_CHECKED: NornicDB. The live tests and the remote runs used Neo4j only.
- NOT_CHECKED: the golden corpus (B-7).
- NOT_CHECKED: clock skew between projector hosts (see the accepted residual
  above).
- NOT_CHECKED: the per-service resource sampler in remote pair 1; pair 2 and
  the slot control have it.
- NOT_CHECKED: the marker's HOT measurement (300 of 300 HOT, buffers median 7)
  was taken with the keep-first `COALESCE` SET and a `pending`-only status
  list. The shipped one-column `GREATEST` SET on the same fresh row should
  behave the same; the shim's `fresh.go` was not re-run.
