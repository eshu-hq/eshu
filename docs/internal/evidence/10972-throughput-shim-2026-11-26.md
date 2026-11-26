# 10972 throughput theory shim — 2026-11-26

Scope: read-only theory shim for `eshu-hq/eshu#10972`. No code changed in this lane.
Worktree `/Users/allen/personal-repos/eshu-wt-10972-throughput` at `a15c918c48`
(matches base `origin/main`), tree clean at write time. Skills active:
`diagnostic-rigor`, `performance-rigor`, `postgres-rigor`.

Issue state (carried from prior result 2, issue-reported): `#10972` OPEN.
Four acceptance boxes: (1) shim posted — this file; (2) drain>inflow change with
headroom; (3) canary generation drained; (4) `#10671` resumes minting.

## Stage boundaries

- Stage under test: `fact_work_items` where `stage='reducer'`.
- Metric start event: `created_at` (mint).
- Metric terminal event: `updated_at` at `status='succeeded'`.
- Handler sub-span: `last_attempt_at -> updated_at`.
- Wait sub-span: `created_at -> last_attempt_at` (queue wait proxy).
- Open backlog: `pending + retrying + claimed (+running)`.
- Drain accounting: growth of `status='succeeded'` counted by `updated_at`
  inside the window (delta-based, not per-minute arbiter boundaries).

## Correctness invariant

- Replica access is read-only (`default_transaction_read_only=on`); no status
  mutated; `SELECT` only.
- `succeeded` growth = drain; `open` = backlog. No double-counting across
  `reducer`/`projector` stages (see reconciliation below).

## Worst-case partition

Worst case is `stage='reducer', domain='supply_chain_impact'`
(vuln-intel OSV npm batch conflict keys) — carried live figures:

- 2026-11-26 02:18:46Z..02:19:28Z sample (prior result 1): pending 8264/8487
  (97.4%), retrying 620, open 8895. Second pool `advisory_group_projection`
  retrying 1460 / pending 207 / open 1672.
- 2026-11-26 02:21:28Z sample (prior result 4): open 8888 (pending 8258 +
  retrying 620 + claimed 10); `advisory_group_projection` open 1674
  (pending 203 + retrying 1465). Backlog effectively flat (8895 -> 8888).
- Oldest open (02:18–02:19Z sample): pending created 2026-10-22 (~34d),
  retrying oldest 2026-10-23 (~34d). Claimed items touched within seconds
  (idle 6–30s, `claim_until` ~+60s): leases churn, not stuck.

## Baseline band

- STALE 2026-11-26 ~02:16Z census (commit `fe80635b39`): cross-checked only,
  superseded. Its "all reducer succeeded 254304" reconciles as live
  reducer+projector succeeded 226358+27952=254310 (delta 6 = in-flight).
- Live baseline 02:18:46–02:19:28Z (prior result 1): reducer claimed 17 /
  dead_letter 1183 / pending 8487 / retrying 2160 / succeeded 226358 /
  superseded 8691 (running 0); projector claimed 6 / dead_letter 2415 /
  pending 423 / retrying 522 / running 10 / succeeded 27952 / superseded 914.
- Live baseline 02:21:28Z (prior result 4): reducer claimed 15 / pending 8477
  / retrying 2167 / succeeded 226365 / dead_letter 1181 / superseded 8691;
  projector claimed 6 / pending 440 / retrying 522 / running 10 /
  succeeded 27952.
- Drain vs inflow, all-reducer (carried): 1h inflow 35 vs drain 161 (4.6x);
  6h 884 vs 1814 (2.05x); 24h 3484 vs 6152 (1.77x) — gross drain>inflow WITH
  headroom at whole-stage level.
- Drain vs inflow, worst partition `supply_chain_impact` (carried): 1h inflow
  6 vs drain 151 (25x, small-inflow window); 6h 184 vs 182 (~0.99x parity);
  24h 914 vs 270 (0.30x, net +644/day; prior sample net +651/day).
  `advisory_group_projection`: inflow 13 vs drain 39 per 24h (net −26/day,
  ~64d to clear 1672).

## Minimum worthwhile improvement

Carried from prior result 1 (theory verdict, unchanged): lift
`supply_chain_impact` drain from ~11/h to >= inflow ~38/h with headroom
(2x inflow ~= 76/h). 7-day clearance of ~8895 needs ~= 91/h (~8.3x current
drain). A change counts only if it raises gross drain above inflow at this
worst-case composition with stated headroom plus a bounded clearance time,
without serialization workarounds (no worker-count cuts, no batch-size-1,
no single-threaded drains).

## Stop threshold

Shim only: no schema/index/worker/claim change made or proposed here. Stop
further sampling if the replica refuses connection. Stop threshold for any
follow-up lane: do not build a claim/dispatch change on an unmeasured theory
— prove it first with the cheapest representative shim per Mandatory
Prove-The-Theory-First.

## Diagnostic signal

- Latency, succeeded updated last 24h (02:18–02:19Z sample, carried):
  `supply_chain_impact` end-to-end `created->updated` mean 114.3h / p50 59.3h
  / p95 379.5h; handler proxy `last_attempt_at->updated_at` mean 1.3min /
  p50 0.8min / p95 3.8min. Queue wait dominates; handler cost negligible.
- Retrying cause mix, `supply_chain_impact` (carried):
  `cross_scope_producer_not_ready` 445, `reducer_handler_abandoned_at_shutdown`
  90, `supply_chain_impact_fanout_failed` 85.
- Theory verdict (carried, unchanged): bottleneck is claim/dispatch throughput
  on the `supply_chain_impact` conflict-key space at worst-case composition,
  not handler work and not stuck leases.

## Implementation presence (carried, base a15c918c48)

Code facts below were independently re-inspected in prior result 4 (bodies
read at HEAD `a15c918c48`); repeated here as carried, not freshly re-read in
this lane:

- Batch rest-arm `ORDER BY`
  `go/internal/storage/postgres/reducer_queue_batch_locked_arms.go:137`
  (inner) and `:182` (outer): `reclaim_rank, gate_boost,
  reducer_domain_priority, reducer_source_inflight_count,
  reducer_source_fair_rank, reducer_domain_fair_rank,
  reducer_domain_inflight_count, updated_at, work_item_id`. Reserved
  search-doc arm `:57/:81`. Pinned by `reducer_queue_batch_test.go:293`,
  `reducer_queue_batch_expired_hold_test.go:370-373`,
  `reducer_queue_batch_search_doc_starvation_test.go:143`. Single-claim query
  `reducer_queue_claim_query.go:113` intentionally narrower (`reclaim_rank,
  updated_at, work_item_id`; no fair-rank/gate machinery).
- Gate boost: `gate_waited_generations` CTE
  `reducer_queue_batch_query.go:316-326`; arm 1 checkpoint
  `last_generation_id` (`#11012` merge `f41c8af7e7`, ancestor confirmed);
  arm 2 older undrained windows via
  `NVDBaselineUndrainedGenerationRowsSQL`
  (`vulnerability/nvd_baseline_undrained_generations.go:29-37`; `#11366`
  `ae8cc457a1` + fence pin `28691a6095`, ancestors confirmed). Cap-one `CASE`
  `locked_arms.go:107-108`, ordered behind `reclaim_rank`.
- Batch-vs-single `go/internal/reducer/service.go:327-330` (`Workers<=1` →
  sequential single `Claim`), `:379-386` (concurrent batch iff
  `BatchWorkSource+BatchWorkSink`). `ReducerQueue` implements `Claim`
  (`reducer_queue.go:369`) and `ClaimBatch` (`reducer_queue_batch.go:20`,
  default limit 16; batchSize `Workers*4` max 64 at `service.go:228`).
- Phase-1: `#10863` stale fence read-only base (query `:136-140`) + lock
  recheck (`locked_arms:160-167`); `#10864` no code by design (closed
  refuted/descoped); `#10865` `reclaim_rank` in candidate (`:232-247`) and
  single (`:18`); `#10866` per-domain handler timeout
  (`service_handler_timeout.go`, merge `#10945`); `#10867` prefetch buffer
  (`service_batch_prefetch.go`); `#10868` split decision + `$2` domain filter;
  `#10869-72` idle work committed with idle-domain key in `ORDER BY`.

## Box verdicts in this lane

- Box 2 (drain>inflow with headroom): implementation present CONFIRMED
  (carried); live effect NOT MET at binding 24h worst-partition window
  (0.30x, backlog flat ~8888). 1h/6h confirm the issue-reported improving
  trend but 24h headroom and bounded clearance are not demonstrated.
  Per-minute arbiter figures (~45/min vs 62–65/min) use different boundaries
  and were not reproduced here. Verdict: planned, not proven.
- Box 3 (canary drained): NOT_CHECKED here; carried as issue-reported
  (arbiter 2026-11-25: reducer row succeeded 19:23:21Z at item level; formal
  close carried by `#10671`).
- Box 4 (`#10671` minting): NOT_CHECKED here; carried as issue-reported
  (checkpoints incomplete per issue).
- No tests executed (read-only lane; pre-push/pre-pr gates coordinator-owned
  and never run). Test bodies cited above were opened in prior results, not
  run here; green status is not claimed.
- Canary generation phase (`vulnerability_intelligence:ed9d4d573`, 1546 NVD
  baseline facts): NOT_CHECKED here (comes from `scope_generations`, outside
  the `fact_work_items` shim queries).

## Ranked plan (no code change in this lane)

The shim does not identify a change provable by a failing-first test within
this lane, so nothing was implemented (TDD not triggered).

1. Next measurement: fresh 24h worst-partition drain-vs-inflow on the QA
   replica with identical boundaries (`created_at` vs succeeded
   `updated_at`), plus `pg_stat_activity`/claim telemetry to separate
   selection, claim, handler, graph-write, and ack time.
2. If claim/dispatch is confirmed as the bottleneck at the same composition,
   prove the candidate (e.g. conflict-key-aware dispatch or fair-rank tuning)
   with the cheapest shim (EXPLAIN ANALYZE / bounded replay) before any lane
   builds it — never by shape inference.
3. Then, and only then, a TDD lane: failing-first regression on the touched
   claim path, focused `go test` on touched packages + `go vet`, integration
   or fixture-plan evidence for any plan/index change.
4. Candidate issue filings (reported, not filed — never file issues from this
   lane): bounded-clearance tracking for `supply_chain_impact` backlog;
   `advisory_group_projection` retry-drain (`cross_scope_producer_not_ready`
   445-copy signal) if it becomes the next bottleneck.

## Exact shim queries (carried from prior result 1)

1. `SELECT stage,status,count(*) FROM fact_work_items GROUP BY 1,2;`
2. Same grouped by domain with `min(created_at),max(updated_at)` where
   `stage='reducer'`.
3. Oldest open `ORDER BY created_at LIMIT` with `left(work_item_id,50)`.
4. Inflow/drain via `count(*) FILTER (created_at>=now()-interval)` vs
   `status='succeeded' AND updated_at>=now()-interval` for 1h/6h/24h.
5. Per-domain `inflow_24h/drained_24h/open_now`.
6. `percentile_cont(0.5/0.95)` of `extract(epoch FROM updated_at-created_at)`
   and `updated_at-last_attempt_at` for succeeded last 24h.
7. Retrying `failure_class` counts.
8. Claimed idle `now()-updated_at` with `claim_until`.
