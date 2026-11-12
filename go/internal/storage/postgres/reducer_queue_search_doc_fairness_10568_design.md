# #10568 design: reserved claim slots for `eshu_search_document`

Status: RED reproduction + mechanism design. No production edits to
`reducer_queue_batch_query.go` in this lane: #10565 owns that file until it
merges, then this design is implemented rebased. Sequencing verified
2026-11-12 (`gh issue view 10565 --json state` = OPEN).

## Problem (re-verified against code, not only the issue text)

`claimReducerWorkBatchQuery` (`reducer_queue_batch_query.go:239`) maps
`eshu_search_document` to `reducer_domain_priority = 1`, every other domain to
0. The `locked` CTE (`:296`) orders by that priority first with `LIMIT $8`
(`:297`, default 16 via `ClaimBatch`). Any claimable priority-0 backlog deeper
than the batch limit fills every batch, so priority-1 rows are never claimed:
660 pending search-doc rows, oldest ~72h, `attempt_count=0`, zero executions.
Coordinator lead re-verified: conflict domains are disjoint
(`reducer_queue_conflict.go:176-188` vs `:224-226`), so this is pure global
ordering starvation, not contention. Pinned by
`TestClaimBatchSearchDocConflictDomainIsDisjointFromAdvisory` (passes).

## Decision: reserved claim slots, not priority aging

Reserve k = 1 slot per batch claim for the oldest ready `eshu_search_document`
candidate; fill the remaining LIMIT - 1 slots with the current ordering
unchanged. When no ready search-doc candidate exists the reserved slot falls
back to the normal ordering (no idle capacity loss).

Rejected alternative, priority aging (e.g. promote rows older than N hours to
priority 0): its advisory impact is unbounded in the worst case (a large old
search-doc backlog inverts the deprioritization entirely and jumps the whole
fair-rank structure), it couples drain behavior to wall-clock age thresholds
that need their own tuning and tests, and it changes the global ORDER BY rather
than one bounded slot. Reserved slots give a structural throughput bound with
no new threshold knob.

## Bound on advisory throughput impact

While ready search-doc candidates exist, advisory-family claims lose at most
k slots per batch: with k = 1 and the default limit 16 that is at most 1/16
(6.25%) of batch capacity, and exactly 0 whenever no search-doc row is ready
(fallback). The bound is per-batch and structural: it holds regardless of
advisory backlog depth, worker count, or search-doc age. Search-doc progress
is likewise structural: at least 1 row per batch whenever any is ready, so the
660-row backlog drains in at most 660 batch claims even under infinite
advisory pressure.

## Lock / claim ordering analysis (concurrency-deadlock-rigor)

- Workflow stages: `base` (gates once per row) -> `reps_ranked`/`reps`
  (representative + fair rank) -> `candidate` (fence) -> `locked`
  (ORDER BY + LIMIT + FOR UPDATE OF lock_target SKIP LOCKED) -> `claimed`
  (single UPDATE ... FROM locked). Entrypoint: `ReducerQueue.ClaimBatch`.
- Shared state: `fact_work_items` rows (per-row lease columns), no new table,
  no new lock target. The reservation is a second ordered arm inside the same
  `locked` CTE selecting from the same `candidate` set, UNION ALL with the
  existing arm, with the two arms' LIMITs summing to $8.
- Conflict domains: `scope` (search-doc) vs `advisory_group` (advisory
  backlog) are disjoint key spaces, so a reserved search-doc claim and an
  advisory claim never fence the same conflict key and never serialize on the
  same representative. No new deadlock edge: still one statement, one lock
  acquisition order (candidate id order into `lock_target`), same
  SKIP LOCKED non-blocking behavior.
- Transaction scope: unchanged (one statement, Read Committed). Retry scope:
  unchanged (claim fencing, lease expiry, dead-letter paths untouched).
  The EvalPlanQual row-self lease/visibility/status recheck on `lock_target`
  must cover both arms identically; the existing
  `TestClaimBatchLockRecheckDropsConcurrentlyClaimedRow` pattern extends to a
  reserved-slot twin (8-claimer storm, duplicate delivery, expired-lease
  reclaim, empty queue, dead-letter replay per the replay matrix).
- Idempotency: claim effect unchanged (status/lease-owner/claim_until write
  keyed by work_item_id); duplicate delivery still collapses on the lease
  fence. Starvation direction is the only behavior change, and it is
  intentionally one-sided and bounded.
- Preserved concurrency: worker count, batch limit default, and all fairness
  lanes keep their current behavior; advisory ordering within its LIMIT - 1
  slots is byte-identical to today.

## Observability for the new path

- Per-claim telemetry from #10565 (candidates in, representatives after fence,
  drops by reason) gains the reserved-slot split: search-doc slots used vs
  fallback-to-advisory counts per batch, so an operator can see the mechanism
  engaging and confirm fallback when the search-doc backlog is empty.
- Oldest search-doc age and pending count already flow through the existing
  queue-observer oldest-age aggregates (`CycleStartedAt` per #9292); the
  acceptance read is oldest-age draining under a live-like backlog while the
  advisory drain rate (claims/s per #10565 claim-duration/batch-size signals)
  does not regress beyond the 1/16 structural bound.
- No new metric label cardinality: domain is already a label dimension on the
  claim path; the slot split reuses it.

## Cleanup disposition: 39 orphaned `building` projection-state rows (#10567)

This lane has no database access (no DSN in this environment) and the claim
change does not touch `eshu_search_document_projection_state`, so the orphans
are unaffected either way. Disposition: documented leave pending a live read
in the implementation PR, which must census the 39 rows (stale builder vs
live generation) and either reap or keep them with that evidence. No code in
this lane depends on their state.

## Truth-sample agreement (#10567)

No production behavior changes in this lane, so the #10567 truth-sample
agreement state is unchanged. The implementation PR must re-run the
representative search-results vs reducer/graph truth sample after the change
and keep it positive.

## Implementation sketch (post-#10565 rebase)

1. Rebase onto merged #10565; keep its C-step-1 gate CTEs byte-identical.
2. Split `locked` into `locked_reserved` (oldest ready search-doc candidate,
   LIMIT 1... via `LEAST`-style split of $8, or a window partition take-1)
   plus `locked_rest` (current ORDER BY, LIMIT $8 - taken), UNION ALL.
   Apply the identical `lock_target` EvalPlanQual recheck predicates on both
   arms. Fallback: when the reserved arm is empty, `locked_rest` takes the
   full $8.
3. Turn `TestClaimBatchReservesSearchDocClaimSlots` green; keep
   `TestClaimBatchOrdersSearchDocumentCatchupAfterGraphTruth` truthful about
   the new shape (it asserts the old ORDER BY string and must be updated as
   part of the behavior change, not weakened).
4. Run the live fixture (`TestClaimBatchSearchDocStarvationLiveReproduction`)
   with a DSN: RED before the fix, green after; then the 8-claimer storm and
   the replay-matrix proofs, then the oldest-age/advisory-rate reads.

## GREEN evidence (implemented 2026-11-12, rebased onto 029798f125)

No-Regression Evidence: reserved slot drains search-doc without regressing
the advisory drain rate, and the lock tail adds no per-row gates.

- Baseline: branch rebased onto origin/main 029798f125 (merged C leaf
  #10572). RED re-derived on that base before the fix: `ClaimBatch()`
  claimed 8 items, none for `eshu_search_document` behind a 24-row
  advisory backlog (batch 8), and 0/4 search-doc rows in 6 rounds of 4
  behind a 40-row backlog.
- Backend: postgres:18-alpine disposable databases (template-clone per
  test, pgx), lane-owned container; input shapes mirror the 2026-11-12
  live census at small scale (search-doc rows seeded older, -72 h).
- After, same fixtures: `TestClaimBatchSearchDocumentStarvesBehindAdvisoryBacklog`
  PASS (search-doc claimed in the batch); `TestClaimBatchSearchDocStarvationLiveReproduction`
  PASS with 4/4 search-doc rows drained in 4 rounds, every round a full
  batch of 4 (terminal: 16 rows claimed — 4 search-doc + 12 advisory —
  in the 4 rounds run, 28 advisory rows still pending — the backlog never
  drains within the proof, so the fixture keeps proving ordering).
  Advisory slots while search-doc rows are ready: 3 of 4 per batch;
  after the 4 search-doc rows drain the rest arm takes the full batch.
  The structural bound holds regardless of backlog depth: at most 1
  slot per batch (1/16 = 6.25% at the default limit 16), 0 when no
  search-doc row is ready (fallback to full $8, covered by the empty
  and fallback assertions in the pin tests).
- Concurrency: new `TestClaimBatchSearchDocSlotHoldsUnderConcurrentClaimers`
  (4 single-connection claimers x batch 2 against 24 advisory + 4
  search-doc rows) takes 8 distinct rows with no duplicates, every batch
  full, at least one search-doc row engaged, and at most one search-doc
  row per batch (k = 1 holds per claim under contention). Sensitivity:
  mutating the reserved arm filter to priority 0 fails the test with 0
  search-doc rows. Storm, duplicate-delivery, empty-queue, dead-letter
  replay, and EvalPlanQual lock-recheck proofs all pass against the new
  shape, as does the #3624 rank-once 0/0 differential.
- Plan shape: `TestClaimBatchGatePrecomputeStaleHeavyExplain` on the
  1200-stale-row fixture holds full-claim SubPlans (15) == candidate
  SubPlans (15) with 0 in the base section — the `LIMIT ($8 - ...)`
  scalar subquery plans as an InitPlan and the LEFT JOIN anti-join adds
  no per-row SubPlan. After-only timings on that fixture (not production
  scale): full claim 161.165 ms, funnel census 98.145 ms; the funnel
  plan carries no Update/LockRows nodes. Production-scale net-of-funnel
  cost stays NOT_CHECKED here (no live backend); it rides on the
  structural argument: the rest arm scans the identical candidate set
  with byte-identical ORDER BY, the reserved arm sorts at most the ready
  priority-1 subset, and the statement still locks at most $8 rows.
- Why safe: both arms carry the identical `lock_target` EvalPlanQual
  row-self recheck (lease theft/duplicate work dropped in either arm);
  SKIP LOCKED never blocks, so the second locking arm adds no deadlock
  edge; conflict domains stay disjoint (scope vs advisory_group); the
  funnel census composes the untouched candidate chain, so admission
  accounting is unchanged.
- NOT re-proven in this lane (no live backend): the #10567 truth-sample
  agreement re-run and the 39 orphaned `building` projection-state rows
  stay deferred to the implementation PR live read, per the Cleanup
  disposition above.

No-Observability-Change: this diff adds no instruments and no metric
label cardinality. The acceptance reads ride existing signals: oldest
search-doc age via the queue-observer oldest-age aggregates, advisory
drain rate via the claim-duration/batch-size signals from #10565. The
per-claim reserved-slot split telemetry sketched in the Observability
section above is NOT in this change; it is follow-up work for the
implementation PR, which is also where the oldest-age/advisory-rate live
reads land.
