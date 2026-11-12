# #10568 design: reserved claim slots for `eshu_search_document`

Status: implemented (#10565 merged; this lane carries the production edit
to `reducer_queue_batch_query.go`). GREEN evidence at the end of this note
is the current record; the design sections above it describe the decision
as made.

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
  no new lock target. The reservation is a second locking arm
  (`locked_search_doc_reserved`) beside `locked_rest`, both selecting from
  the same `candidate` set and UNION ALL into `locked`, with the two arms'
  LIMITs summing to $8. Each arm lock-probes its ordered candidates through
  a per-candidate LATERAL primary-key lookup, so the candidate side is
  always the outer loop regardless of planner statistics.
- Conflict domains: `scope` (search-doc) vs `advisory_group` (advisory
  backlog) are disjoint key spaces, so a reserved search-doc claim and an
  advisory claim never fence the same conflict key and never serialize on the
  same representative. No new deadlock edge: still one statement, locks
  taken in each arm's claim order against `lock_target`, same
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

Implemented by #10581 (this lane's sibling; this note's mechanism ships the
slot, #10581 ships the split). The deferred per-claim split is now explicit:

- Shipped by #10581: `eshu_dp_reducer_batch_claim_reserved_slot_total`, an
  `Int64Counter` recorded once per non-empty batch claim by closed
  `outcome` (`reserved_used` when the batch admits at least one
  `eshu_search_document` row, `fallback` when it admits none; empty
  batches record nothing). Emission lives in
  `emitReservedSlotClaim` (`reducer_queue_batch.go`), which reads only the
  admitted intents — the claim statement is untouched. Bounded
  cardinality by construction: one instrument, one label, two values, one
  count per batch. No domain label was invented on the claim
  duration/batch-size instruments.
- What an operator could already read (unchanged): `ReducerExecutions` and
  `ReducerQueueWaitDuration` are recorded per domain
  (`recordReducerResult`), so search-doc executions resuming and its queue
  wait draining show the mechanism's effect per domain. The queue-observer
  oldest-age aggregates group by stage and source system, not by domain, so
  they cannot isolate search-doc age; `QueueClaimDuration` and
  `BatchClaimSize` likewise carry no domain label, so neither can show a
  per-domain drain rate.
- Combined read: the reserved-slot counter shows the mechanism engaging
  per batch (`reserved_used` vs `fallback`), while the per-domain
  execution/queue-wait signals show the outcome effect (search-doc
  executions resuming, queue wait draining). Proof that both move with the
  mechanism is in #10581 (hermetic on/off/fallback/empty-batch contract
  tests plus the live on/off fixture proof).

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

Superseded by the GREEN evidence below. As implemented: `locked` is split
into `locked_search_doc_reserved` (oldest ready search-doc candidate,
LIMIT 1) plus `locked_rest` (current ORDER BY, LIMIT $8 - taken), UNION
ALL, with the identical `lock_target` EvalPlanQual recheck predicates on
both arms; when the reserved arm is empty, `locked_rest` takes the full
$8. Each arm locks through a per-candidate LATERAL primary-key probe so
stale planner statistics cannot flip the join order (measured in the
GREEN evidence).

## GREEN evidence (implemented 2026-11-12, base 1b434b4d7c)

No-Regression Evidence: reserved slot drains search-doc without regressing
the advisory drain rate, and the lock tail adds no per-row gates.

- Baseline: `git merge-base HEAD origin/main` = 1b434b4d7c. RED re-derived
  on that base before the fix: `ClaimBatch()` claimed 8 items, none for
  `eshu_search_document` behind a 24-row advisory backlog (batch 8), and
  0/4 search-doc rows in 6 rounds of 4 behind a 40-row backlog.
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
- Plan shape (paired, identical fixture, no manual ANALYZE anywhere):
  `TestClaimBatchGatePrecomputeStaleHeavyExplain` -count=3 per side on
  postgres:18-alpine, candidate SELECT steady at 119.96-120.83 ms on
  both sides. Full claim: base (origin/main archive) 122.405 / 122.490 /
  122.650 ms (lock tail ~1.9 ms over candidate); first split-arm shape
  161.189 / 160.889 / 161.208 ms (tail ~37.5 ms — two references forced
  candidate materialization and stale statistics flipped both arms to an
  outer scan over all 1,350 lock_target rows); implemented LATERAL shape
  124.782-125.379 ms across two runs (tail ~4.8 ms: a consistent
  +2.3-2.9 ms over base at ~0.2 ms within-side spread — the bounded
  second-arm cost, far under the 10% stop threshold). Full-claim
  SubPlans (15) == candidate SubPlans (15) with 0 in the base section on
  the implemented shape — the `LIMIT ($8 - ...)` scalar subquery plans as
  an InitPlan and the LEFT JOIN anti-join adds no per-row SubPlan; the
  funnel plan carries no Update/LockRows nodes. Implemented plan excerpt:
  both arms are `Limit -> Nested Loop` over a Sort of `CTE Scan on
  candidate` with the inner side `LockRows -> Index Scan on
  fact_work_items lock_target` at loops=8 (= LIMIT, so locks stop once
  the arm fills) — lock_target is never the outer scan. Rejected shape,
  measured: a single locked arm floating the oldest priority-1 row via a
  window rank over candidate ran 196.456-196.652 ms (the window CTE
  inlined to the inner side of the lock join and re-evaluated per
  lock_target row, loops=1350). Production-scale cost stays NOT_CHECKED
  here (no live backend); it rides on the structural argument: both arms
  probe the identical candidate set by primary key with byte-identical
  ORDER BY keys, and the statement still locks at most $8 rows.
- Why safe: both arms carry the identical `lock_target` EvalPlanQual
  row-self recheck, pinned by exact count (each predicate exactly twice,
  `TestClaimBatchLockRecheckPredicatesPinnedByCount`; mutation-checked)
  and proven live through the shipped LATERAL lock shape by the
  lateral-shape case of `TestClaimBatchLockRecheckDropsConcurrentlyClaimedRow`
  (id-join-only still steals; lease theft/duplicate work dropped); SKIP
  LOCKED never blocks, so the second
  locking arm adds no deadlock edge; conflict domains stay disjoint
  (scope vs advisory_group); the funnel census composes the untouched
  candidate chain, so admission accounting is unchanged.
- NOT re-proven in this lane (no live backend): the #10567 truth-sample
  agreement re-run and the 39 orphaned `building` projection-state rows
  stay deferred to the implementation PR live read, per the Cleanup
  disposition above.

No-Observability-Change: this diff adds no instruments and no metric
label cardinality, and it does not implement the Observability section
above. The existing per-domain outcome signals are the acceptance read:
`ReducerExecutions` and `ReducerQueueWaitDuration` (both recorded with
the domain attribute in `recordReducerResult`) show search-doc executions
resuming and its queue wait draining once the reserved slot engages. The
queue-observer oldest-age aggregates group by stage and source system,
not domain, and `QueueClaimDuration` / `BatchClaimSize` carry no domain
label, so none of those can attribute age or drain rate to
`eshu_search_document` and this note does not claim them. The per-claim
reserved-slot split telemetry is deferred to #10581 ("Per-domain
execution telemetry for the search-doc reserved claim slot"), category:
genuine missing coverage. Owner agreement for that deferral:
PENDING-COORDINATOR — quote the owner's agreement in the PR beside the
#10581 link before merge.

## Addendum: source-fairness ORDER BY spelling restore (2026-11-12)

`TestReducerQueueBatchClaimOrdersBySourceFairnessWithinDomain` failed on
the LATERAL tail: the reshaped claim no longer contained the pinned
fragment `ORDER BY reducer_domain_priority ASC,
reducer_source_inflight_count ASC, reducer_source_fair_rank ASC,
reducer_domain_fair_rank ASC, reducer_domain_inflight_count ASC,
updated_at ASC, work_item_id ASC`. Root cause: relocated, not dropped —
commit `6c60a2eaef` kept that literal in `locked_rest`, and the
stats-robust reshape (`d250093f38`) moved the identical seven keys into
the ordered-candidate subquery (`candidate.`-qualified, kept qualified
beside the reserved anti-join, which outputs the same column names) and
the outer locking select (`ordered_candidate.`-qualified). The reserved
arm's age-only order over the priority-1 subset predates the reshape
and is the §Decision contract. Fix: spell the outer `locked_rest`
ORDER BY unqualified — ORDER BY binds simple names to the SELECT's
output columns (`ordered_candidate.*`), so the sort keys, plan, probe
order, LIMIT admission, and EvalPlanQual recheck quals are unchanged,
and the literal again matches the read-only differential tail
(`readOnlyCandidateTail`) verbatim. A toy two-level shim on
postgres:18-alpine confirmed identical admitted rows and byte-identical
EXPLAIN output for both spellings before the edit.

No-Regression Evidence: paired `TestClaimBatchGatePrecomputeStaleHeavyExplain`
-count=3 per side on the same disposable postgres:18-alpine fixture
(1,200 stale rows, no manual ANALYZE; before side at branch head
950cac8812, after side with this fix): full claim before 124.601 /
124.574 / 124.165 ms (lock tail +4.2–4.7 ms over candidate), after
124.231 / 124.469 / 124.501 ms (tail +4.4–4.5 ms) — overlapping ranges,
within noise; SubPlan parity held (full 15 == candidate 15, base
section 0 on both sides). Fairness test, reserved-slot starvation and
concurrent-claimer proofs, the #3624 rank-once 0/0 differential, and
the EvalPlanQual lock-recheck proof all pass against the edited query.
Production-scale cost stays NOT_CHECKED, as above.

No-Observability-Change: the spelling restore adds no instrument,
label, span, log, worker, lease, or batch knob; claim admission and
ordering are unchanged, so the per-domain signals named above remain
the acceptance read.

Observability Evidence (#10581, implemented 2026-11-12 on this lane):
`eshu_dp_reducer_batch_claim_reserved_slot_total` by closed `outcome`
(`reserved_used`, `fallback`) is registered in
`go/internal/telemetry/instruments.go`, recorded once per non-empty batch
in `emitReservedSlotClaim` (`reducer_queue_batch.go`), and covered by a
telemetry-coverage registry row. Hermetic contract tests prove each
outcome and the empty-batch skip
(`TestClaimBatchEmitsReservedUsedWhenSearchDocClaimed`,
`TestClaimBatchEmitsFallbackWhenNoSearchDocClaimed`,
`TestClaimBatchSkipsReservedSlotOnEmptyBatch`); the live fixture proof
(`TestClaimBatchReservedSlotMovesLive`, postgres:18-alpine, 40-row
advisory backlog + 4 older search-doc rows, batch 4) records
`reserved_used` x4 while a search-doc row is ready and `fallback` on the
post-drain batches. The claim statement is untouched, so the S2
lock-tail and EvalPlanQual evidence above stands unchanged.

No-Regression Evidence (#10581, 2026-11-12, this lane): paired runs of
`TestClaimBatchSearchDocStarvationLiveReproduction` plus
`TestClaimBatchReservesSearchDocClaimSlots` on the same fixture and
backend — baseline `bc150cc0c6` 4.027s (live proof 3.91s) vs lane
`0617039cf7` 4.024s (live proof 3.90s), postgres:18.6 disposable
container, 40-row advisory backlog + 4 older search-doc rows with the
#10568 reserved slot engaged: identical within noise. Expected: the
lane touches no claim SQL (statement byte-identical to S2), adding per
non-empty batch only one in-memory scan of the admitted intents
(bounded by LIMIT, early exit) plus one
`eshu_dp_reducer_batch_claim_reserved_slot_total` count. Production-scale
cost stays NOT_CHECKED, as in S2.
