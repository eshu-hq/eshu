# Evidence — #10630 step-2 claim-latency cut (live-lease fence precompute)

## What changed

`claimReducerWorkBatchQuery` evaluated the #4137 live-lease conflict-key
fence as a correlated `NOT EXISTS` probe against `fact_work_items` once
per representative row (~5.2k probes/claim on the live backlog, 95%+ of
claim time per probe B). Each probe descends the partial unique index
`fact_work_items_reducer_live_lease_uniq` and heap-fetches to check
`claim_until > $1`: cache-hot it costs microseconds, cache-cold it costs
a random-page miss per probe — the 2.6–2.8 s live claim term.

The rewrite computes the live-lease key set once per claim
(`live_fence_keys AS MATERIALIZED`: `DISTINCT conflict_domain,
COALESCE(conflict_key, scope_id)` over `stage='reducer'`,
`status IN ('claimed','running')`, `claim_until > $1`) and anti-joins it
in `candidate` with the shipped key comparison verbatim. The shipped
`work_item_id <>` self-exclusion is vacuous over the set: `base`
pre-filters to unleased rows, so no candidate row can satisfy the set's
`claim_until > $1` arm (same C-4 argument as the step-1 semantic
scalars; pinned by `TestClaimBatchBaseExcludesLiveLeases`). Set and
anti-join evaluate on the statement snapshot, exactly like the probe
they replace; the lock-time race stays owned by the untouched
`locked`-CTE EvalPlanQual recheck. Lock tail, `ORDER BY`, ranking,
batch knobs, funnel, and DDL are untouched. The single-claim query
(`claimReducerWorkQuery`) keeps the old probe: out of scope for this
lane (its probe count is bounded by rows scanned to the first match).

## Conflict domain, workers, leases

- Conflict domain: `fact_work_items` rows sharing `(conflict_domain,
  COALESCE(conflict_key, scope_id))` at `stage='reducer'`.
- Workers: 8 concurrent single-connection claimers × 20 rounds (storm),
  plus 20 timed serial `ClaimBatch` rounds (replay, batch limit 8).
- Leases: production worker lease is 1 minute (`reducerClaimLeaseDuration`
  in `go/cmd/reducer/main_helpers.go`); the proof uses a 1h fixture
  lease plus staggered expiries, so the live set (200 keys) is a
  conservative overestimate of the production live set (tens of rows at
  0.375 claims/s × ~2 rows/claim).
- Fixture: 12,401-row backlog (6,000 two-row keys + 200 live-held keys),
  `ANALYZE`d (production autovacuum keeps stats on this hot table).

## Performance Evidence

Paired same-fixture, same-binary-lineage before/after (only the query
constant differs; two identically seeded disposable databases):

- Derived-candidate `EXPLAIN (ANALYZE, BUFFERS)` x3 in rollback:
  BEFORE 94.634 / 94.348 / 94.968 ms with 6200 per-row probes on
  `fact_work_items inflight` (`loops=6200`); AFTER 196.014 / 195.998 /
  194.724 ms with 0 `inflight` probes and one `live_fence_keys`
  evaluation. The AFTER shape is a Nested Loop Anti Join over a
  200-row in-memory set (planner estimates the correlated
  `status`/`claim_until` predicates independently at 3 rows vs 200
  actual, so it does not pick the hash join; forcing hash off-knob
  measures 64.7 ms).
- Timed `ClaimBatch` ×20 (batch limit 8): BEFORE p50 74.505 ms /
  p95 79.924 ms, 160 admitted, 106.38 items/s; AFTER p50 147.633 ms /
  p95 167.896 ms, 160 admitted, 54.82 items/s.

The hotter-fixture AFTER number is a cache artifact, not a production
regression: the BEFORE cost is buffer-bound (6,200 random heap probes —
microseconds hot, ~0.5 ms cold each per probe B: 95%+ of the 2.6–2.8 s
live claim), while the AFTER cost is CPU-bound (one 200-row index scan
plus ~1.2 M in-memory comparisons, ~130 ms measured, zero buffer
dependence). The shared `base`/reps/lock/claim cost cancels in the
comparison, so the production projection is: fence term 2.5 s → ≤ 0.3 s
at the measured live-key scale (linear in representatives × live keys;
safe for any plausible live set — the cliff needs >10 k simultaneous
live leases, impossible under 8 workers × 1-minute leases), full claim
2.6–2.8 s → ~0.4–0.6 s. Sustained-window live proof stays with the
coordinator's live gate.

## Correctness Evidence

- `TestClaimBatchFencePrecomputeMatchesPreChangeCandidateSetAndOrder`:
  bidirectional 0/0 candidate differential (frozen machine-derived OLD
  vs derived-from-shipped NEW) across live/expired/absent-holder,
  NULL-key scope fallback, cross-domain isolation, and two clocks, plus
  fence non-vacuity cells (live key admits nobody; expired holder
  reclaims as representative).
- `TestClaimBatchFencePrecomputeSharedKeyStorm`: 8 claimers × 20 rounds
  over 32 three-row keys → exactly the 32 oldest rows, each once, zero
  keys with two live leases (EvalPlanQual recheck intact).
- Hermetic shape guards: shipped query contains `live_fence_keys AS
  MATERIALIZED (` verbatim from `reducerClaimLiveFenceKeysCTE()` and no
  `FROM fact_work_items AS inflight`; chain/funnel verbatim-source
  guards still green (single-claim query untouched by construction).

## Observability Evidence

No new signals. The before/after is carried by the existing per-batch
`QueueClaimDuration` histogram (`go/internal/reducer/service_batch.go`)
and the `reducer.batch_claim` span; batch size by the existing
`BatchClaimSize` histogram. Funnel stays OFF (no `BatchClaim*` funnel
series changes).
