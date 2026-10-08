# #7661: lease recovery for stale webhook trigger claims

Claimed webhook trigger rows held by a dead ingester stayed `claimed`
forever: the claim took only `queued` rows and `claimed_at` was never
compared with a clock. The selector now reaps before it claims: rows
past the lease window (default 15m) go back to `queued`, rows past the
attempt cap (default 3) fail with `claim_lease_exhausted`. Handoff
completions carry the claim fencing token, so a stale holder finishing
late affects zero rows instead of the new owner's claim.

## Performance Evidence: reap, exhaust, and stuck-count plans

Backend: local Postgres 18 (`postgres:18-alpine`, `eshu-pg18`
container). Input shape: 10,000 `handed_off` history rows plus 300
stale `claimed` rows (the issue's large-table/few-stale shape), 50 of
them at fencing token 5 to exercise the exhaust arm. Baseline: no
sweep existed, so stuck rows accumulated unbounded and every stale
claim was a permanently lost trigger; there is no before-timing to
regress against.

EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) of the production statements
(`TestWebhookTriggerStoreReapPlanUsesClaimedAtIndex`, which fails on
any Seq Scan of `webhook_refresh_triggers` or any plan that skips the
partial index):

- reap (limit 100, cap 3): 1.7 ms execution, Index Scan on
  `webhook_refresh_triggers_claimed_at_idx`
- exhaust (limit 100, cap 3): 1.0 ms execution, same index
- stuck count: 0.06 ms execution, same index

Row counts per instrumented tick: at most 2×limit rows touched across
the two reap statements (limit 100 default), plus one index-only
count. Contention proof on the same backend: two concurrent reclaimers
split 20 stale rows exactly once each (FOR UPDATE SKIP LOCKED, no
double-reap); two concurrent claimers split 40 queued rows exactly
once; a stale holder's late handoff updates zero rows while the live
holder's completes. The change is safe because every sweep statement
is LIMIT-bounded, reads only the partial claimed-rows index no matter
how large the handed-off history grows, never blocks on a live holder
(SKIP LOCKED both sides), and a reap error aborts the tick before any
claim so no partial work escapes.

## Observability Evidence: reap counter, stuck gauge, reap log

Two new signals, both emitted at the selector dispatcher once per tick
(`recordClaimReapOutcome`, covered by a meter-reader test):

- `eshu_dp_webhook_trigger_claim_reaps_total` (counter, label
  `outcome` = `requeued` | `exhausted`): rows recovered per tick.
- `eshu_dp_webhook_trigger_stale_claims` (gauge, no labels): residual
  rows still stuck past the lease window after the sweep; nonzero
  means stale claims arrive faster than one reap limit per tick.
- Structured log `reaped expired webhook trigger claims` with
  `requeued`/`exhausted` counts whenever a sweep recovers rows.

A gauge-query failure logs a warning and never fails selection
(tested). Coverage doc, ingestion-collector metrics reference, and
telemetry README mirror the new instruments; the lease window and
attempt cap are documented env settings
(`ESHU_WEBHOOK_TRIGGER_CLAIM_LEASE_WINDOW`,
`ESHU_WEBHOOK_TRIGGER_MAX_CLAIM_ATTEMPTS`) with selector defaults.
