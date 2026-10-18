# 8258 no-regression note: per-endpoint operation tokens (Refs #8258)

No-Regression Evidence:

- Baseline: every failed claim persists the claim-stage fallback in
  `failure_operation` (`collect_failure`, `commit_failure`,
  `identity_mismatch`), because no provider call site stamps an endpoint
  token and `ClassifyProviderFailure` never sets `operation`.
- After: failed calls carry a bounded endpoint token
  (`search_issues`, `list_incidents`, `collect_targets`, …); causes with no
  token keep the exact stage fallback as before.
- Backend/version: unit-level (no database); Go 1.26.9 per repo pin.
- Input shape: provider error fixtures per funnel (Jira search/changelog/
  remote-link/metadata, PagerDuty incidents/log-entries/change-events/
  services, Prometheus targets/rules, Tempo echo/tags/values, Grafana
  search/datasources/alert-rules, Loki labels/values/series/rules,
  Dependabot repo-scope and org-scope pages).
- Row counts: 9 new `endpoint_operation_test.go` files, one per provider
  surface plus SDK; TDD RED proven first
  (`TestSearchIssuesFailureCarriesEndpointToken` failed pre-fix with
  `FailureOperation() = ""`).
- Why safe: error-path only — the success path executes zero new
  statements; tokens are a closed identifier set (never raw paths, never
  metric labels); `WithOperation` sanitization bounds length/charset;
  retry/terminal classification untouched; idempotency and ordering of the
  claim path unchanged (same errors, more precise labels).

No-Observability-Change: no new metrics, spans, or logs; the change only
increases the precision of the existing `failure_operation` column values.
