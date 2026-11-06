# Expired-Lease Requeue Cap (#10113)

No-Regression Evidence: the reaper dead-letters past a bounded cap instead
of requeueing forever. Baseline: `reapExpiredWorkflowClaimsQuery` set every
reaped item to `pending` and every reaped claim to `expired` with no cap, so
a run that never returned a failure verdict looped without bound. After: the
reap partitions candidates by `attempt_count` against
`workflow.DefaultExpiredLeaseMaxRequeues()` (10). Below the cap both
branches issue the byte-identical statements they issued before, so normal
churn of one or two expiries requeues exactly as today. At or past the cap
the item and its claim go `failed_terminal` with class
`expired_lease_budget_exhausted`. Backend: PostgreSQL 18.6 disposable
database (local socket). Input shape: one `pending` work item driven through
repeated claim, forced-expire, reap laps with no heartbeat, completion, or
failure verdict. Terminal counts: the 10th claim raises `attempt_count` to
10, the 10th reap dead-letters the item and claim, and a further
`ClaimNextEligible` finds no work. Lock ordering is unchanged: the candidate
still locks claim and item together under `SKIP LOCKED`, the partitions are
disjoint so no row is updated twice, and the fenced rechecks stay on the
locked rows. No new index, no worker or batch change, and the reap adds no
second attempt increment, so one expiry still costs exactly one attempt and
drain and contention behavior are preserved. Full proof:
`TestDefaultExpiredLeaseMaxRequeuesPinsValue`,
`TestExpiredLeaseBudgetExhaustedClassPinsValue`,
`TestWorkflowControlStoreReapExpiredClaimsDeadLettersPastCap` (RED as
undefined symbols / missing cap branch before the fix, GREEN after),
`TestWorkflowControlStoreIntegrationExpiredLeaseRequeueCapDeadLetters`
(live RED/GREEN), plus the unchanged
`TestWorkflowControlStoreIntegrationHeartbeatReclaimAndSplitBrain`,
`TestWorkflowControlStoreIntegrationReapExpiredClaimsLeavesActiveClaimsUntouched`,
and `TestWorkflowControlStoreReapExpiredClaimsUsesSkipLockedAndBackoff`,
whose pinned literals (`status = 'expired'`, `status = 'claimed'`,
`SKIP LOCKED`) the partitioned statements preserve.

No-Observability-Change: the change adds no metric, span, log line, status
field, or pprof surface. Reaped rows still flow through the existing
coordinator reap observations, and a dead-lettered item surfaces through the
existing row truth (`status`, `last_failure_class`,
`last_failure_operation`) and the existing instrumented Postgres spans. The
new class is deliberately not added to the status bounded set in this slice;
it reads as unknown there while the database row stays the source of truth
(status-surface registration is follow-up work).
