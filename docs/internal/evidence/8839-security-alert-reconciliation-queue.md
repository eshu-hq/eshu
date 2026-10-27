# Reducer reconciliation queue gates the security-alert empty answer (#8839)

**A collection-healthy security-alert empty answer no longer reads
`complete` while the reducer's `security_alert_reconciliation` work for the
scope is still outstanding or dead-lettered. Outstanding work reports
`building` with cause `reducer_backlog`; dead-lettered work reports
`unavailable` with cause `dead_lettered_domain`.**

## Problem

The #8803 read checked collection and projection only. The
`security_alert_reconciliation` domain runs after projection (the generation
becomes `active` when the projector acknowledges it;
`generation_projected_commit.go`), so a scope with a projected generation
and no reconciliation rows yet answered `complete`, exact and fresh. The
reducer queue state for that domain was already visible through
`get_freshness_causality`, but the empty-answer classification did not
consult it. The MCP and OpenAPI text already promised that a zero-row
answer is clean only when coverage is `complete` and the domain has no
pending or dead-lettered work; the implementation now keeps that promise.

## Change

- `SecurityAlertCollectionStateStore.SecurityAlertReconciliationQueueStates`
  reads per-scope queue depth for the domain in one statement: outstanding
  (pending, claimed, running, retrying — the same outstanding set the
  status snapshot's per-domain backlog counts) and dead-lettered counts.
  Completed, failed, and other-domain rows are not read. The domain literal
  is pinned query-side like the terminal failure classes.
- The queue read runs only for a collection-healthy answer; a
  collection-incomplete answer keeps its collector verdict without paying
  for it. A queue read failure fails closed to
  `collector_state_unavailable` like any other probe error.
- Dead-lettered work outranks outstanding work, mirroring the
  freshness-causality ranking (stale beats building). Work queued for a
  scope outside the covering set is ignored.
- Two coverage reasons join the closed set, reusing the freshness-cause
  vocabulary so reason and cause read the same: `reducer_backlog` and
  `dead_lettered_domain` (OpenAPI enum extended in lockstep). The verdict's
  cause is now the proven `FreshnessCause` token instead of a bool, so each
  verdict names its own cause.
- No new readiness envelope: like the unread and outside-target answers,
  coverage already says why the page is not a configured-but-empty result.

## Proof

- `TestSecurityAlertReconciliationQueueCoverage` (8 cases over anchored
  and unanchored list reads): pending, retrying, dead-lettered,
  dead-plus-outstanding, drained, outside-covering-set, and
  collection-incomplete-keeps-collector-verdict (which also proves the
  queue read is skipped there) failed first and pass.
- `TestSecurityAlertReconciliationQueueStatesLive` (PostgreSQL 18.6,
  isolated bootstrapped schema) proves the real query counts each status
  into the right bucket and omits drained scopes.
- `TestSecurityAlertCoverageOpenAPIEnum` and
  `TestSecurityAlertCoverageReasonsPinned` cover the two new reason tokens.

## Queue-read measurement (acceptance criterion)

`TestSecurityAlertReconciliationQueuePlanLive` runs the shipped query over
a 500,000-item `security_alert_reconciliation` backlog (499,500 outstanding,
500 dead-lettered, plus 1,000 succeeded rows on a quiet scope),
`VACUUM ANALYZE`d, on PostgreSQL 18.6:

| Covering scopes | Plan | Buffers | Execution |
|---|---|---|---|
| 1 | Parallel Seq Scan, HashAggregate | 12,236 shared hit | 47.3 ms |
| 1,024 (1 hot + 1,023 absent) | Parallel Seq Scan, HashAggregate | 12,237 shared hit | 48.4 ms |

The seed is adversarial: the hot scope holds 99.8% of the table, so the
planner seq-scans instead of probing `fact_work_items_scope_generation_idx`.
Even so the read fits the 1 s collection-state probe with 20x headroom, so
no new index is warranted. The read returns exactly the hot scope at both
sizes with exact outstanding/dead-letter counts.

Observability Evidence: the existing
`security_alert_collection_state` stage event and the existing
`eshu_dp_query_security_alert_empty_answers_total` counter report the new
outcomes through bounded tokens only — two new `collector_state` values
(`reconciling`, `reconciliation_stuck`) and two new `coverage_reason`
values (`reducer_backlog`, `dead_lettered_domain`), all closed. No new
instrument, span, or log line.
The truth envelope gains the proven cause plus its bounded next-check
(`reducer_backlog` and `dead_lettered_domain` already have next-check
entries), which is the documented behavior of `WithFreshnessCause`.
