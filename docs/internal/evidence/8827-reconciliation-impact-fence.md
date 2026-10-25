# #8827 reconciliation impact fence: root cause, proof, and evidence

`security_alert_reconciliation` and `supply_chain_impact` are enqueued from the
same scope generation. The reconciliation can only match an alert once the
producer has written an impact finding.

## What the issue got wrong, and what is true

The issue says the reconciliation "burns an attempt while supply_chain_impact
holds the same scope fence". The queue does not work that way. A row blocked by
the `(scope, scope id)` conflict key is not claimable, and `attempt_count`
increments only on a claim, so waiting behind a claimed producer costs nothing.

Root-Cause Evidence: the claim query's fence counts only `claimed` or `running`
holders (`reducer_queue_claim_query.go`, the `NOT EXISTS ... inflight`
predicate). A producer that is `pending` or `retrying` holds no fence. The
reconciliation is claimable, runs, defers on the counting class
`security_alert_reconciliation_waiting_for_impact`, and `attempt_count` rises.
The handler stops deferring at attempt 3 and writes `unmatched`. Observed on a
disposable PostgreSQL 18.6 with the real queue and handler
(`TestReducerContentionGateSecurityAlertReconciliationGivesUpVisiblyWithoutAFence`,
case "no fence, producer pending"): attempts 1, 2, 3 are claimed with
`AttemptCount` 1, 2, 3, the third writes one `unmatched` decision, and the
producer row was `pending` throughout.

## The change

The handler asks `readiness/producer.ImpactStore` for the producer's state for
its own `(scope_id, generation_id)`. While any impact row is pending, retrying,
claimed, or running and its newest sign of life is under
`crossscope.ProducerReadinessMaxWait` (30 minutes), it returns the non-counting
class `security_alert_reconciliation_impact_not_ready`, enrolled in
`nonCountingReducerRetryFailureClasses`. No worker count, batch size, claim
ordering, or lease changed, and the reconciliation stays claimable, so no
concurrency is lost. Because the class freezes `attempt_count`, the bound is
elapsed time, not an attempt comparison.

The handler's own give-up at attempt 3 is now visible: a counter outcome, a WARN
log, and `sub_signal_impact_wait_gave_up` on the success line. The cap stays 3
and stays separate from the queue budget; the in-flight wait is outside that
budget and the counting wait is capped by the handler.

## Concurrency and ordering

Contention Evidence: `TestReducerContentionGateSecurityAlertReconciliationImpactFenceUnderContention`
runs six workers, each on its own connection, over twelve scopes that each hold
a pending reconciliation (the older row) and a pending producer. Three runs
under `-race`: 12 of 12 reconciliations ran first and deferred without
spending an attempt, 0 overlaps of a scope's two domains, and all 24 rows ended
`succeeded` at `attempt_count` 1 with 12 `matched` decisions.

Retry and dead-letter matrix, on the real queue:

| Case | Test | Result |
| --- | --- | --- |
| reconciliation first, producer pending, retrying, claimed | `...ImpactFenceKeepsItsAttemptBudget` | five cycles past `MaxAttempts` and the handler cap: `attempt_count` 1, nothing written; fenced out while the producer is claimed |
| producer acks | same | next claim matches, acks at attempt 1 |
| no fence wired, producer pending | `...GivesUpVisiblyWithoutAFence` | pre-fix behavior reproduced: attempts 1, 2, 3, gives up |
| producer dead-lettered | same | counting path, gives up at 3 with `gave_up_unmatched` |
| probe error | handler test | counting error, never the non-counting class |
| producer idle past 30 minutes | handler test | falls to the counting path, `gave_up_bound_exceeded` |

Mutation: removing the class from `nonCountingReducerRetryFailureClasses` fails
the budget test at cycle 2 (`AttemptCount = 2, want 1`).

## Performance Evidence

Disposable PostgreSQL 18.6 (`ci-postgres-bookworm@sha256:afc7e2d4...`), all 229
migrations, `fact_work_items` populated to 350,000 rows (2,000 scopes, 5
generations each, 35 domains per generation; the target generation holds one
pending producer row).

- The probe (`ImpactProducerStateQuery`) plan is an index scan on
  `fact_work_items_scope_generation_idx`, no sequential scan. Three runs of
  `EXPLAIN (ANALYZE, BUFFERS)`: execution 0.054, 0.042, 0.042 ms; planning
  0.151, 0.146, 0.157 ms; 4 buffer touches. The run was on a shared host and
  is not a throughput claim.
- It runs once per reconciliation pass that has a decision waiting on impact
  evidence, never for a pass with nothing to wait for
  (`TestSecurityAlertReconciliationSkipsTheProbeWhenNothingIsPending`).

No-Regression Evidence: the only claim-path change is one more
`work.failure_class = '...'` term in the `attempt_count` CASE of the claim
UPDATE, evaluated for the one claimed row, not in a WHERE clause.
`BenchmarkReducerQueueClaimDeepQueue` at depth 20,000, `-benchtime=300x`,
`-count=3`, same container: before 53.85, 54.01, 54.39 ms/op; after 53.93,
53.97, 53.99 ms/op; allocations 96 before and after.

## Observability

Observability Evidence: `eshu_dp_security_alert_reconciliation_impact_wait_total{domain,outcome}`
with `deferred_in_flight`, `gave_up_unmatched`, `gave_up_bound_exceeded`; the
INFO defer log and WARN give-up log; `sub_signal_impact_wait_gave_up`; the
existing `eshu_dp_reducer_executions_total{status="deferred"}` for the new
class. Reference: `docs/public/reference/telemetry/security-alert-reconciliation-impact-wait.md`
and the 3 AM runbook section.

## Not covered

- The status blockage row named no holder domain when this change landed. #8954
  adds `holder_domain` to each `queue_blockages` row; see
  `docs/internal/evidence/8954-status-blockage-holder-domain.md`.
- Impact evidence written by another scope's `supply_chain_impact` run is not
  waited on; the probe is keyed on the intent's own scope generation.
- A deployed run with a real pending-impact wait.
