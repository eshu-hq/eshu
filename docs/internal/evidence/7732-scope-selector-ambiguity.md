# Admin Skip/Reopen Scope-Selector Evidence

Issue #7732. Skip and reopen resolved the operator's scope selector
(`scope_id = $1 OR source_key = $1`) silently: skip dead-lettered rows
across every matched scope (no scope bound on the UPDATE), and reopen
picked the lowest scope id (`ORDER BY scope_id LIMIT 1`). Both routes now
share one resolver (`resolveScopeID`) that fails closed with 409, naming
the matched scopes, when the selector matches more than one scope.

## No-Regression Evidence (#7732):

- Baseline: base `01ceb1dd05ca` behavior, observed live — skip with a
  colliding selector returned 200 and dead-lettered rows in both scopes;
  reopen returned 200 and reopened the lowest-id scope's row.
- After: the same colliding fixture returns 409 on both routes and no row
  changes state; the distinct-key reopen path is unchanged
  (`TestAdminHandler_ReopenLive` still green, 2 rows reopened and claimed).
- Backend/version: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2), local Docker,
  full migration set via the disposable-database harness.
- Input shape: operator-issued skip/reopen calls (admin-only, low
  frequency). Skip runs one extra bounded resolve SELECT (`LIMIT 2`)
  before its UPDATE; the UPDATE itself is narrowed from the OR match to
  the exact resolved scope id, so it touches fewer rows, never more.
- Row counts: live collision run — 2 pending rows (one per scope) stay
  pending after the skip 409; 1 succeeded row stays succeeded after the
  reopen 409.
- Why safe: the resolve is a read-only bounded SELECT; every write path
  is either unchanged (distinct selector) or removed (ambiguous selector
  now refuses before any write). The unknown-selector skip contract
  (200 with count 0) is pinned by
  `TestSkipRepositoryWorkItemsUnknownSelectorSkipsNothing`.

## Observability Evidence (#7732):

Both refusals record a denied governance audit event through the existing
`recordRecoveryAction` mechanism (`skip_refused_ambiguous_scope`,
`reopen_refused_ambiguous_scope`), the same signal every sibling reopen
and replay refusal emits; the 409 response names the matched scopes so
the operator can resubmit with the exact scope id. No metric, span, log,
or status output is added or renamed, and no telemetry-coverage row
changes: the audit mechanism and the per-route error counters already
cover these paths.
