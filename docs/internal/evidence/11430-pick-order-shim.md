# #11430 pick-order shim: handler drain convicted (pick order and limit trim exonerated)

Date: 2026-11-25. Lane: drive/11430-drain-bottleneck (leaf child).
Tree: branch `drive/11430-drain-bottleneck`, HEAD b83a27cb77; no production code touched.
Prior: the [drain-funnel shim](11430-drain-funnel-shim.md) proved 8668/8827
supply_chain_impact candidates (98.2%) claimable with all fences exonerated,
yet only ~24/hr complete with ~11 in flight. Per the arbiter flip ruling this
lane reopens ordering/fair-rank as #1 and tests pick order vs limit trim vs
handler drain. Read-only diagnosis: code reads in this tree plus reads via
`sh /tmp/qa-read.sh <file>` (15s statement timeout, read-only txn) and
kubectl log reads from the QA replica. All clocks below are UTC; wall time
during the shim was ~13:55-14:05.

## Verdict

**HANDLER DRAIN convicted.** Effective completions trail admissions ~3-10x
depending on window (stable hourly buckets ~10:1; latest 15-min window
~2.6-3.7x as the drain accelerates). Pick order is exonerated as a starvation
cause and limit trim is exonerated as a binding constraint, both with numbers.
The winners-tick single-flight lock provably does NOT serialize claim
admission. The drain loss decomposes into three measured parts; the third
needs one named follow-up to split.

## Claim loop shape (code, this tree)

- Claimer: `runBatchConcurrent` (`go/internal/reducer/service_batch.go`) uses
  ONE claimer goroutine; `claimLimit = min(batchSize, idleWorkers)` — idle-only
  claiming, no prefetch unless configured. Empty claim waits `pollInterval`
  (`defaultPollInterval = 1s`, `go/internal/reducer/service_loop_config.go`).
  `batchClaimSize = Workers*4` capped to [4, 64].
- Batch claim SQL (`claimReducerWorkBatchQuery`,
  `go/internal/storage/postgres/reducer_queue_batch_query.go` + `..._batch_locked_arms.go`):
  LIMIT ($8 - reserved row), ORDER BY reclaim_rank, gate_boost,
  reducer_domain_priority, reducer_source_inflight_count,
  reducer_source_fair_rank, reducer_domain_fair_rank,
  reducer_domain_inflight_count, updated_at, work_item_id. Single-claim twin
  (`claimReducerWorkQuery`) is LIMIT 1 ORDER BY reclaim_rank, updated_at,
  work_item_id. Within the dominant domain this is FIFO-ish by updated_at with
  expired holds first; fair-rank spreads across conflict keys, not time.
- Winners-tick single-flight (`tryClaimSupplyChainImpactWinnersResweepSQL`,
  `go/internal/storage/postgres/supply_chain_impact_canonical_winners_rebuild.go`):
  `SELECT pg_try_advisory_xact_lock(...)` — transaction-scoped advisory lock
  contended ONLY among resweep/audit claimants. Reducer claim statements never
  take it. By construction it cannot serialize claim admission.

## Commands and observed values

SQL shims are plain SELECTs (files lived in /tmp only, never in the tree).

1. Backlog shape (q1): pending 8370, retrying 479, claimed 9, dead_letter 28,
   succeeded 1600, superseded 1881 (domain total 12367 including terminal
   history). Attempt census (q6): attempt_count=0 in 10212 (includes terminal
   history), =1 in 2062. Never-attempted (q7, last_attempt_at NULL): 1598
   total, of which 831 waiting (pending/retrying; r3) — matches the arbiter's
   ~828-831.
2. Claim rate (q2, last_attempt_at touches/hr): 30 / 243 / 260 / 309 / 243 /
   215 / 307 across the last 7 hourly buckets — sustained 215-309/hr. f2: 72
   touches/10min. f1: 283 touched in the last hour.
3. Completion rate (q3, succeeded by updated_at/hr): 3 / 2 / 0 / 6 / 0 / 25
   (partial hour) — choppy, ~2-25/hr on hourly buckets. d5: 27 succeeded in
   the last 15 min (=108/hr pace). Vuln-engine logs (15 min): 123
   supply_chain_impact handler starts, 122 "reducer execution succeeded"
   (100 distinct intent_ids), 1 failed — handler pace ~400-490/hr with ~99%
   handler-level success.
4. In-flight (q4): 9 claimed, all 9 live lease, 0 expired. Nothing time-parked
   (r2: visible_now 8848, parked_future 0, max_park_min -0.06).
5. Handler service time (q5, recently succeeded): p50 0.21 min, p90 0.81 min —
   fast when the pass proceeds.
6. Deferral classes (r1): cross_scope_producer_not_ready 390 (all retrying),
   reducer_handler_abandoned_at_shutdown 50, reducer_handler_timeout 25,
   supply_chain_impact_fanout_failed 13. The 390 wait on declared producers
   container_image_identity + ci_cd_run_correlation
   (`go/internal/reducer/crossscope/readiness.go`,
   `go/internal/reducer/supply_chain_impact/core/evidence_load.go`).
   Producer domains themselves are drained (d2: 1 pending total, rest
   terminal), and the 390 waits spread 1-per-scope over ~390 distinct
   vuln-intel scopes/generations (d1) — not one stuck generation.
7. Staleness of the producer-wait set (d3): p50 cycle age (COALESCE
   reopened_at, created_at) 7.70hr, p50 touch age 7.70hr — far past
   ProducerReadinessMaxWait (30 min, `readiness_floor.go`). Only 5 of the 390
   were touched in the last hour (d4). These are stale deferrals mid-queue,
   not the current churn.
8. Current churn shape (d4, rows touched in last hour, n=323): pending/(none)
   272, succeeded 30, claimed 11, retrying producer_not_ready 5, fanout 4-5.
   The 272 pending/(none) signature (fresh last_attempt_at, cleared failure
   fields) does NOT match failIntent-retry (sets retrying + class,
   `reducer_queue.go:106`) — they recycled via a reopen/wake path (see open
   discriminator D2).
9. FIFO discriminator: oldest waiting updated_at 2026-11-24 06:42:51 (r3) =
   the mass reopen/replay wave vintage (w4 shows the whole head cohort stamped
   06:42-06:43); newest touch 13:55 today. The cursor is AT the oldest wave;
   the 831 never-attempted share that same vintage (min updated_at Nov-24
   06:43, w3) = queue depth (~8849 waiting / ~250/hr ~ 35hr), not a fence.
   (A naive touched-vs-untouched updated_at median comparison is contaminated:
   every claim rewrites updated_at, so touched rows read newer by construction.
   Not used.)
10. Winners stall (adjacent, NOT the convicted cause): materialized_at
    2026-11-25 01:00:57, lag 12h56m (w1); dirty log 52710 + staging 8037,
    oldest dirty exactly the watermark (w2). Ticks are not advancing the
    watermark (consistent with the TEMPORARY #10972 cap squeeze forcing a
    full rebuild per tick). Claims nevertheless flow at 250+/hr across the
    same 7 buckets — the frozen tick does not gate admission. supply_chain_impact's
    deferral names container_image_identity/ci_cd_run_correlation producers,
    not winners.
11. Post-execution attrition (15-min log window): 3 x SQLSTATE 23505
    (live-lease unique-index conflict aborts the whole batch claim,
    `reducer_queue_batch.go` repoll path = 12/hr pace) + 14 "superseded"
    mentions; domain carries 1881 superseded rows. Handler successes (100
    distinct/15min) exceed DB completions (27/15min) ~3.7x; 122 success lines /
    100 distinct intents shows re-execution after lost acks.

## Suspect disposition

- Pick order: EXONERATED as starvation cause. Cursor at the oldest wave,
  representatives = 100% of non-drained candidates (funnel), untouched tail =
  same vintage as the head (depth, not fence). ORDER BY + LIMIT shape read and
  pinned above.
- Limit trim (LIMIT $8 / batchClaimSize / idle-gating): EXONERATED as binding.
  215-309/hr flow through the same LIMIT path for 7 straight hours; in-flight
  9 (worker-gated by design, never backlog-saturated); batches not starved
  (72 touches/10min).
- Winners-tick single-flight serialization of admission: EXONERATED by
  construction (advisory xact lock, disjoint lock domain) AND by observation
  (250+/hr claims while watermark frozen 13h).
- Handler drain: CONVICTED. Completions trail admissions on every window
  (hourly ~10:1; latest 15-min ~2.6-3.7x with the drain accelerating).
  Handlers are fast when they proceed (p50 12s; 122/123 recent executions
  succeed), so the drain loss is (a) producer-wait deferral churn (390 rows,
  stale, 1-per-scope spread), (b) post-execution attrition (handler success
  exceeds DB completion ~3.7x; 23505 batch aborts + active supersession),
  (c) claim-to-pending recycling with cleared failure fields (~272/hr, path
  TBD in D2).

## Open discriminators (not checked, owned follow-ups)

- D1 (winners stall vs consumers): ticks not advancing materialized_at for
  ~13h with 60k deep dirty backlog — which tick path (keyed over-cap full vs
  stuck) and which consumers gate on the watermark. Adjacent risk, not this
  domain's convicted cause.
- D2 (272/hr pending/(none) recycler): split reopen/redrive vs waiting-wake
  by joining the wake/redrive event sources per work_item_id over one hour;
  decides whether the churn is upstream replay or producer-commit wake fan-out.
- D3 (post-execution attrition split): per-hour ack_claim_rejected vs
  superseded-mid-flight counts from DB matched-counts/logs over a full hour;
  decides whether successes die at ack or at supersession.
