# Security-Alert Empty-Answer Truth (#8803 PR A)

An empty security-alert reconciliation answer used to report
`coverage.state=complete` with an exact, fresh truth envelope even when the
collector had never collected the requested repository. This note records the
read-path proof for the fix: the per-state behavior, the query plans for the two
new statements, and the observability that reaches an operator.

What the fix claims: an empty security-alert list, count, or first inventory
page reports `coverage.state=complete` with exact, fresh truth only when the
security-alert collector has a projected generation and no later terminal
fetch failure for a target that covers the requested repository (or,
unanchored, for every configured target). It does not cover a pending or
dead-lettered reducer reconciliation or a scoped caller's grants on an
unanchored read. Since #8838 a repository added to a target after its latest
projected generation reads `no_committed_generation`: each generation records
the repository set it collected, and the read checks the requested repository
against the latest committed generation's set.
A caller with no grants gets `not_configured` with reason
`no_authorized_repositories`. The `coverage` object on an empty inventory page
and the `collector_readiness` envelope are additive fields. The envelope appears
on a healthy empty answer (`ready_zero_results`), on an answer with no enabled
instance (`not_configured`), and on a failed state read
(`readiness_unavailable`). A still-collecting or terminal-failure
(`target_incomplete`) answer, a repository outside every target, and a grantless
answer carry none.

This claim departs from binding ruling item 6, which said "every target that
covers" the repository; the code says "a target that covers". An anchored read
takes the best covering target, because every covering target reads the same
provider data for that repository. The arbiter's own F5 wording and the code
agree, and a failing redundant covering target is a collector-readiness signal
for PR B, not repository coverage. The second independent review judged this
the honest semantic.

## Per-state behavior

Each row is a case in `TestSecurityAlertEmptyAnswerCoverageInvariant`
(`go/internal/query/supply/chain/security_alert_coverage_test.go`), run against
the list, count, and inventory routes. With the classification disabled
(`securityAlertEmptyAnswerVerdict` forced to return no verdict, which is the
pre-fix behavior), all 42 subtests of the 14 cases on 3 routes fail: every
non-healthy case answers `complete`, `exact`, `fresh`, and the healthy cases
lack the readiness envelope (the inventory route lacks `coverage` entirely).
With the classification on, all 42 subtests pass.

| Case | `coverage.state` | `coverage.reasons` | level | freshness | readiness |
| --- | --- | --- | --- | --- | --- |
| No enabled instance | `not_configured` | `collector_not_configured` | `fallback` | `unavailable` | `not_configured` |
| Repository outside every target | `not_configured` | `repository_outside_target_set` | `fallback` | `unavailable` | absent |
| Org allowlist names the repository under another owner | `not_configured` | `repository_outside_target_set` | `fallback` | `unavailable` | absent |
| Target pending, nothing projected | `target_incomplete` | `no_committed_generation` | `derived` | `building` | absent |
| Work items under a different scope id than the config | `target_incomplete` | `no_committed_generation` | `derived` | `building` | absent |
| Terminal failure, nothing projected | `target_incomplete` | `terminal_fetch_failure:auth_denied` | `fallback` | `unavailable` | absent |
| Terminal failure after a projected generation | `target_incomplete` | `terminal_fetch_failure:not_found` | `derived` | `stale` | absent |
| `failed_terminal` status, terminal flag false, nothing projected | `target_incomplete` | `terminal_fetch_failure:auth_denied` | `fallback` | `unavailable` | absent |
| `failed_terminal` status, terminal flag false, after a projected generation | `target_incomplete` | `terminal_fetch_failure:not_found` | `derived` | `stale` | absent |
| Unknown stored failure class | `target_incomplete` | `terminal_fetch_failure:unclassified` | `fallback` | `unavailable` | absent |
| Terminal failure followed by a later success | `complete` | absent | `exact` | `fresh` | `ready_zero_results` |
| Deactivated org target plus live repo target | `complete` | absent | `exact` | `fresh` | `ready_zero_results` |
| One covering target failed, another healthy | `complete` | absent | `exact` | `fresh` | `ready_zero_results` |
| Healthy zero control | `complete` | absent | `exact` | `fresh` | `ready_zero_results` |

Every non-complete row in this table also carries
`missing_collector_completion` as its freshness cause. A repository outside
every target carries no readiness envelope because an enabled instance exists:
the envelope's `not_configured` means no enabled instance is registered, which
would be false for it. The two `failed_terminal` rows cover work items written
before migration 185, whose `last_failure_terminal` is NULL and reads as
false; removing the status check from `classifySecurityAlertTarget` fails them.

A scoped token with no grants is answered without a collector read
(`TestSecurityAlertCoverageGrantlessScopedToken`, list, count, and inventory,
anchored and unanchored):

| Case | `coverage.state` | `coverage.reasons` | level | freshness | readiness |
| --- | --- | --- | --- | --- | --- |
| Scoped token with no grants | `not_configured` | `no_authorized_repositories` | `fallback` | `unavailable` | absent |

It carries no freshness cause, emits no stage event, and still increments the
counter. Before the fix all six subtests failed: the list and count routes
answered `complete`, `exact`, `fresh`, and the inventory route had no
`coverage` object.

### Terminal failure classes

`coverage.reasons` names the class the claim runner persisted on the terminal
row. Any other stored value reads `unclassified`.
`TestSecurityAlertCoverageTerminalClasses` covers each class and
`TestSecurityAlertCoverageReasonsPinned` pins the full reason set by value.

| Class | Persisted when |
| --- | --- |
| `auth_denied`, `not_found`, `rate_limited`, `terminal` | The provider failure carried that class. |
| `retryable` | The retry budget ran out on a transient provider failure: the terminal row keeps the cause's class. |
| `attempt_budget_exhausted` | The retry budget ran out on a cause that carried no class. |
| `identity_mismatch` | The collected generation's identity did not match the claim. |
| `collect_failure`, `commit_failure` | A non-retryable collect or commit error carried no class. |

Before the fix only the first four were named, so the most common terminal
cause, an exhausted retry budget, read `unclassified`.

Companion tests pin the rest of the contract:

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
  lacked `not_configured`, and later the reasons enum lacked the new reason
  and classes), passes once the published reasons enum equals the handler's
  closed set.
- `TestSecurityAlertCoverageProbeErrorClass`: a failed probe names its
  `error_class` on the stage event (see Observability) and only a caller
  cancel skips the counter.

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
target's scope), not a point read. On the vacuumed seed that is about 1 ms and
61 buffers for one target with 2,000 work items, and about 36 ms and 2.7k
buffers for an unanchored read over 50 targets. The cost also depends on the
table's visibility map: rows not yet vacuumed are read from the heap (see the
live proof below). Work-item history is not pruned, so the cost keeps growing
as the collector runs. Pages with rows pay nothing. The whole probe runs under
a 1 s deadline and fails closed past it to `collector_state_unavailable`, and
the stage event names a deadline hit as `error_class=deadline_exceeded` at
WARN, so a read that outgrows the deadline is visible rather than silent. A
true point read needs an index on
`(collector_kind, scope_id, updated_at DESC, work_item_id)`, built
concurrently in the shape of migrations 156 and 210; that DDL is deferred to
its own issue with its own proof.

### Live PostgreSQL proof

`go/internal/query/security_alert_collection_state_postgres_live_test.go` runs
the shipped statements on a schema bootstrapped from every repository
migration (227 on this branch), enrolled in the `live-postgres-readiness`
runner (`ESHU_POSTGRES_DSN`). Local run: PostgreSQL 16.15, all three tests
pass.

- `TestSecurityAlertCollectionStateSemanticsLive`: only the enabled,
  non-deactivated `security_alert` instance contributes configuration
  (disabled, deactivated, and `git` instances do not); the latest work item is
  chosen by `updated_at`, ignoring a newer item of another collector kind; on an
  equal `updated_at` the lowest `work_item_id` wins; a `failed_terminal` row
  with NULL `last_failure_terminal` reads as `failed_terminal` with the flag
  false; `active`, `superseded`, and `completed` generations count as committed
  and `pending` and `failed` do not; a target with no work items still returns
  a row.
- `TestSecurityAlertCollectionStateBoundsLive`: 64 instances and 1,024 targets
  read; 65 instances and 1,025 targets fail with
  `ErrSecurityAlertCollectionStateBound`.
- `TestSecurityAlertCollectionStatePlanLive`: 50 targets with 2,000
  `security_alert` work items each plus 100,000 `git` work items. Both plans use
  `workflow_work_items_collector_scope_generation_updated_idx` for the lateral
  read, which the test asserts.

| Table state | Lateral plan | Execution | Shared buffers |
| --- | --- | --- | --- |
| After `ANALYZE` only (no visibility-map bits) | Bitmap index scan, bitmap heap scan of 100,000 heap blocks, top-1 sort | 102.7 ms (523 ms on a first run, about 60 ms of it JIT) | 102,396 |
| After `VACUUM (ANALYZE)` | Index-only scan, Heap Fetches 0, top-1 sort | 18.3 ms | 2,397 |

A busy work-item table sits between the two, so the deferred index is the fix
for both the history scan and the heap reads.

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
`duration_seconds`, and `error`. A failed probe adds `error_class`:

| `error_class` | Meaning | Level | Counted |
| --- | --- | --- | --- |
| `deadline_exceeded` | The 1 s probe deadline expired while the request was live. | WARN | yes |
| `bound_exceeded` | A read stopped at the 64-instance or 1,024-target bound (`ErrSecurityAlertCollectionStateBound`). | WARN | yes |
| `query_failed` | Any other store or SQL error. | WARN | yes |
| `canceled` | The caller's request ended during the probe. | INFO | no |

Every failed probe answers `collector_state_unavailable`. A successful probe
logs at INFO with no `error_class`. The counter
`eshu_dp_query_security_alert_empty_answers_total{coverage_state,reason}`
counts the answers, including a grantless token's
`not_configured`/`no_authorized_repositories`, which emits no stage event; both
labels are closed sets. No payload, log attribute, or label carries a
repository name, allowlist, URL, target scope id, or failure message.
