# Security-Alert Empty-Answer Truth (#8803 PR A)

An empty security-alert reconciliation answer used to report
`coverage.state=complete` with an exact, fresh truth envelope even when the
collector had never collected the requested repository. This note records the
read-path proof for the fix: the per-state behavior, the query plans for the two
new statements, and the observability that reaches an operator.

## Per-state behavior

Each row is a case in `TestSecurityAlertEmptyAnswerCoverageInvariant`
(`go/internal/query/supply/chain/security_alert_coverage_test.go`), run against
the list, count, and inventory routes. With the classification disabled
(`securityAlertEmptyAnswerVerdict` forced to return no verdict, which is the
pre-fix behavior), all 36 subtests (12 cases on 3 routes) fail: every
non-healthy case answers `complete`, `exact`, `fresh`, and the healthy cases
lack the readiness envelope (the inventory route lacks `coverage` entirely).
With the classification on, all 36 pass.

| Case | `coverage.state` | `coverage.reasons` | level | freshness | readiness |
| --- | --- | --- | --- | --- | --- |
| No enabled instance | `not_configured` | `collector_not_configured` | `fallback` | `unavailable` | `not_configured` |
| Repository outside every target | `not_configured` | `repository_outside_target_set` | `fallback` | `unavailable` | `not_configured` |
| Org allowlist names the repository under another owner | `not_configured` | `repository_outside_target_set` | `fallback` | `unavailable` | `not_configured` |
| Target pending, nothing projected | `target_incomplete` | `no_committed_generation` | `derived` | `building` | absent |
| Work items under a different scope id than the config | `target_incomplete` | `no_committed_generation` | `derived` | `building` | absent |
| Terminal failure, nothing projected | `target_incomplete` | `terminal_fetch_failure:auth_denied` | `fallback` | `unavailable` | absent |
| Terminal failure after a projected generation | `target_incomplete` | `terminal_fetch_failure:not_found` | `derived` | `stale` | absent |
| Unknown stored failure class | `target_incomplete` | `terminal_fetch_failure:unclassified` | `fallback` | `unavailable` | absent |
| Terminal failure followed by a later success | `complete` | absent | `exact` | `fresh` | `ready_zero_results` |
| Deactivated org target plus live repo target | `complete` | absent | `exact` | `fresh` | `ready_zero_results` |
| One covering target failed, another healthy | `complete` | absent | `exact` | `fresh` | `ready_zero_results` |
| Healthy zero control | `complete` | absent | `exact` | `fresh` | `ready_zero_results` |

Every non-complete row also carries `missing_collector_completion` as its
freshness cause. Companion tests pin the rest of the contract:

- `TestSecurityAlertCoverageRowsPresentSkipsProbe`: a page with rows never reads
  collector state and keeps its exact, fresh truth.
- `TestSecurityAlertCoverageProbeFailure`: a failed state read still serves the
  page, as `collector_state_unavailable` with `fallback` truth, `unavailable`
  freshness, and `readiness_unavailable`.
- `TestSecurityAlertCoverageProbeDeadlineFailsClosed`: a store that blocks
  until its context ends is cut off by the 1 s probe deadline
  (`securityAlertCollectionStateTimeout`) and answers the same fail-closed
  shape. Before the deadline existed the test hung until the test timeout.
- `TestSecurityAlertCoverageUnanchoredReadsEveryTarget`: a read without a
  repository anchor checks every configured target.
- `TestSecurityAlertCoverageScopedCallers`: an out-of-grant selector answers
  `404` before any state read; an in-grant repository reads only its own
  covering target.
- `TestSecurityAlertCoverageOpenAPIEnum`: failed on the old spec (state enum
  lacked `not_configured`), passes once the published reasons enum equals the
  handler's closed set.

Every test response is checked for target scope ids, provider scope ids,
allowlists, and other repository names; none appear.

## Query plans

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` on PostgreSQL 16 with the
repository migrations for `ingestion_scopes`, `scope_generations`,
`workflow_runs`, `workflow_work_items` (including 184, 185, 201, 202), and
`collector_instances`. Seed: 200,000 work items, 100,000 of them
`security_alert` spread over 50 targets with 2,000 work items each, 198,000
generations, and 42 collector instances, then `VACUUM (ANALYZE)`.

| Statement | Plan | Time | Buffers |
| --- | --- | --- | --- |
| Configuration read | Seq scan of `collector_instances` (42 rows), sort, limit | 0.11 ms | 4 |
| Target state, one target (anchored) | Index-only scan of `workflow_work_items_collector_scope_generation_updated_idx`, top-1 sort, PK fetch; index-only `EXISTS` on `scope_generations_scope_idx` | 1.06 ms | 61 |
| Target state, 50 targets (unanchored) | Same plan per target, memoized PK fetch | 36.1 ms | 2,671 |
| First draft, one target (heap fetch per work item) | Index scan plus heap fetch of 2,000 rows | 1.09 ms | 471 |
| First draft, 50 targets | Same | 41.0 ms | 23,349 |

### Ruling assumption disproved, measured form used

The ruling expected the latest-work-item read to be an indexed point read. The
plans disprove that: security-alert work items have no retention, and
`generation_id` is a hash, so no existing index orders one target's work items
by `updated_at`. The shipped statement selects only index columns in the
lateral subquery, so the scan reads index tuples (Heap Fetches: 0) and fetches
one heap row by primary key.

Cost model: each empty answer costs O(work-item history of each covering
target's scope). On the seed that is about 1 ms and 61 buffers for one target
with 2,000 work items, and about 36 ms and 2.7k buffers for an unanchored read
over 50 targets. Work-item history is unbounded, so the cost keeps growing as
the collector runs. Pages with rows pay nothing. The whole probe runs under a
1 s deadline and fails closed past it. A true point read needs an index on
`(collector_kind, scope_id, updated_at DESC)`; that DDL belongs to a later
change with its own proof, not to this one.

### Unanchored reads keep the per-target sweep

An unanchored count, inventory, or list read checks every configured target.
The alternative, the single latest work item of the security_alert instance,
was measured on the same seed (instance `sa-live`, 100,000 security-alert work
items among 200,000):

| Statement | Plan | Time | Buffers |
| --- | --- | --- | --- |
| Instance-level latest row, full row | Parallel seq scan of `workflow_work_items`, top-N sort | 19.1 ms | 4,369 |
| Instance-level latest row, id only | Parallel seq scan of `workflow_work_items`, top-N sort | 16.1 ms | 4,369 |

No index orders an instance's work items by `updated_at`, so the planner scans
the whole table: more buffers than the 50-target sweep, and its cost grows with
every collector kind's history, not only security alerts. It is also less
truthful. One target's newer success hides another target's terminal failure,
so an empty answer would read `complete` when a target never collected, which
breaks the #8803 invariant. The sweep stays, bounded by the same 1 s deadline
and the 1,024-target read bound.

The same statements ran against the seed for the review probes. The
deactivated organization instance returned no configuration row. A target whose
terminal failure was followed by a later completed work item read `completed`
with a projected generation (healthy). A scope id with no work items returned
an empty status with no generation (still collecting).

## Observability

Observability Evidence: each classified empty answer runs stage
`security_alert_collection_state`. Its `supply chain query stage completed` log
carries `coverage_state`, `coverage_reason`, `collector_state`,
`duration_seconds`, `error`, and `deadline_exceeded`. The counter
`eshu_dp_query_security_alert_empty_answers_total{coverage_state,reason}`
counts the answers; both labels are closed sets. No payload, log attribute, or
label carries a repository name, allowlist, URL, target scope id, or failure
message.
