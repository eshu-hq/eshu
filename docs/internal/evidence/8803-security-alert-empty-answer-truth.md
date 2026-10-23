# Security-Alert Empty-Answer Truth (#8803 PR A)

An empty security-alert reconciliation answer used to report
`coverage.state=complete` with an exact, fresh truth envelope even when the
collector had never collected the requested repository. This note records the
read-path proof for the fix: the per-state behavior, the query plans for the two
new statements, and the observability that reaches an operator.

## Per-state behavior

Each row is a case in `TestSecurityAlertEmptyAnswerCoverageInvariant`
(`go/internal/query/supply/chain/security_alert_coverage_test.go`), run against
the list, count, and inventory routes. Before the fix every non-healthy row
answered `complete`, `exact`, `fresh` (34 failing subtests on the unmodified
handlers). After the fix all cases pass.

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
  page, as `collection_state_unavailable` with `readiness_unavailable`.
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

The ruling expected a point read for the latest work item. It is not one:
security-alert work items have no retention, and `generation_id` is a hash, so
no existing index orders one target's work items by `updated_at`. The shipped
statement selects only index columns in the lateral subquery, so the scan reads
index tuples (Heap Fetches: 0) and fetches one heap row by primary key. Cost
grows with each target's work-item history, about 0.4 ms per 2,000 items per
target, and runs only for empty answers. A true point read needs an index on
`(collector_kind, scope_id, updated_at DESC)`, which is schema work outside this
change.

The same statements ran against the seed for the review probes. The
deactivated organization instance returned no configuration row. A target whose
terminal failure was followed by a later completed work item read `completed`
with a projected generation (healthy). A scope id with no work items returned
an empty status with no generation (still collecting).

## Observability

Observability Evidence: each classified empty answer runs stage
`security_alert_collection_state`. Its `supply chain query stage completed` log
carries `coverage_state`, `coverage_reason`, `collector_state`,
`duration_seconds`, and `error`. The counter
`eshu_dp_query_security_alert_empty_answers_total{coverage_state,reason}`
counts the answers; both labels are closed sets. No payload, log attribute, or
label carries a repository name, allowlist, URL, target scope id, or failure
message.
