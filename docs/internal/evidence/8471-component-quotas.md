# #8471 Per-Component Quota Evidence

Per-component generation quotas with atomic claim-grant reservation (owner
decisions in the PR body: manifest declares with per-field operator override;
uniform application with override differentiation). This note records the
measured numbers behind the gate markers.

## No-Regression Evidence:

Backend: PostgreSQL 18.6 on aarch64-unknown-linux-musl (Alpine), throwaway
container on the dev host; Go live-test suites on the same host via
`ESHU_POSTGRES_DSN`.

- Unconfigured path is byte-identical: quota-less components run the
  unchanged `claimNextWorkflowWorkItemQuery` constant, so their cost cannot
  regress by construction. The full claim-family live suite
  (`TestClaim*`, `TestFairClaim*`, `TestWorkflowControl*`, DDL parity,
  checksum manifest) passes on the final head, including the #8468/#8469
  depth drains.
- Gated vs plain on the same backlog (1000 pending items, two fairness
  keys, ANALYZE'd tables, `EXPLAIN (ANALYZE, COSTS OFF)`, same host back
  to back): plain 0.39-0.88 ms (first 0.88 cold, steady ~0.40), gated
  0.37-0.59 ms (first 0.59 cold, steady ~0.40). Indistinguishable; the
  quota CTEs add no measurable cost at this scale. Gated full-plan buffers:
  ~120 shared hits, candidate walk off
  `workflow_work_items_fair_claim_idx`, reserve via
  `component_quota_windows_pkey` (conflict arbiter).
- Usage-table size is bounded by construction: at most two hour buckets
  per active (component, scope), pruned in-statement, so the budget
  pre-filter scans a tiny set and no secondary index is added (index
  doctrine: the PK serves the reserve point lookup; a window-range index
  is unjustified without hot-query evidence).
- Terminal counts from the committed proofs: idle throttle grants exactly
  the limit then stops; the 8-replica race grants exactly 1 of 8 with one
  grant of budget left (unconditional-reserve mutation grants 7 and fails);
  restart preserves the count; floor fallback grants exactly the floor.
  Telemetry: `quota_exceeded_total` ticks once per throttled empty claim;
  `quota_unreadable_fallback_total` ticks once per degraded call with a
  WARN line. Safe because the pre-filter is liveness-only (stale reads
  cannot grant) and the conditional upsert is the atomic gate, proven by
  the serialization race.

## Observability Evidence:

- `eshu_dp_workflow_claim_quota_exceeded_total` (`collector_kind`,
  `quota_kind`) and
  `eshu_dp_workflow_claim_quota_unreadable_fallback_total`
  (`collector_kind`) are asserted live with an SDK manual reader in
  `TestClaimQuotaOverBudgetThrottlesOnIdleSystem` (1 tick) and
  `TestClaimQuotaUnreadableFallsBackToFloorRate` (3 ticks); `scope_id`
  never labels a metric (log line only).
- Coverage: `scripts/verify-telemetry-coverage.sh` green with the quota
  gate folded into the `collector.claimed_run` coverage row; operator
  reference gains the `claim-quotas.md` sibling page plus nav entry.
  No operator-dashboard panel yet (follow-up).
