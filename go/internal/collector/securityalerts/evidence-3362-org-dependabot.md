# Evidence: org-wide Dependabot alert collection (#3362)

Scope: add an organization-scoped target kind to the Dependabot security-alert
collector (`GET /orgs/{org}/dependabot/alerts`) that fans out into
per-repository facts, alongside the existing per-repository path.

## Performance impact declaration

- Stage: hosted collector request + fact-envelope construction
  (`go/internal/collector/securityalerts` and `.../alertruntime`).
- Hot path: `go/internal/collector/**` (provider HTTP request loop).
- Cardinality: one paginated org request per org target per collection versus
  one paginated request per repository target. For an org of N repositories the
  org scope issues 1 paginated request stream instead of N.
- Baseline / known-normal: each request stream is bounded by `per_page<=100`,
  `max_pages` (default 1, capped at 100), and `repository_alert_limit`. The org
  loop reuses the exact same bounded cursor loop as the per-repository path.
- Stop threshold: org collection stays within the per-call page/limit bounds;
  truncation past `max_pages` is surfaced as `source_freshness=partial` exactly
  like the per-repository path.

## No-Regression Evidence

The org path reuses one shared `paginateAlerts` loop and `listAlertsPage`
helper for both endpoints, so per-page clamping, `rel="next"` cursor traversal,
cross-host next-link rejection (token never forwarded off-host), `state=open`
filtering, truncation marking, and rate-limit metadata are byte-for-byte the
same code as the unchanged per-repository path. The per-repository public
surface (`ListRepositoryAlertsPages`, allowlist enforcement, fact shape) is
unchanged; all pre-existing per-repository tests still pass.

API-call reduction: one org target replaces N per-repository targets and
collapses N independent paginated request streams into one paginated org
request stream per collection, while emitting the same number of
per-repository `security_alert.repository_alert` facts (one per repository that
owns an alert).

Fan-out fact shape is unchanged: org alerts are converted with the same
`NewGitHubDependabotAlertEnvelope` constructor, the same
`facts.SecurityAlertRepositoryAlertFactKind`, the same
`source_confidence=reported`, and a `repository_id` derived to the same
canonical `security-alert:github:<owner>/<repo>` scope the per-repository path
and the `security_alert_reconciliation` reducer already key on. Reducer
reconciliation is therefore unchanged.

Verified by:

- `go test ./internal/collector/securityalerts/... -count=1` — 29 passed,
  including new `TestGitHubDependabotClientListsOrganizationAlertsAcrossCursorPages`,
  `TestGitHubDependabotClientMarksOrganizationAlertsTruncatedAtMaxPages`,
  `TestGitHubDependabotClientDoesNotForwardTokenToCrossHostOrganizationNextLink`,
  `TestGitHubDependabotClientReturnsRateLimitFailureForOrganizationAlerts`,
  `TestClaimedSourceFansOutOrganizationAlertsIntoPerRepositoryFacts`,
  `TestClaimedSourceSkipsOrganizationAlertsWithUnusableRepository`, and the
  org-scope validation tests.
- `go test ./internal/workflow ./cmd/collector-security-alerts ./internal/reducer
  ./internal/coordinator ./internal/telemetry -count=1` — all passed (the one
  unrelated `TestSCIPLanguageSubtreesRunWithBoundedWorkers` flake passes 3/3 in
  isolation and touches no security-alert code).
- `go vet` and `golangci-lint run` on the changed packages — no issues.

## No-Observability-Change

Existing runtime telemetry already diagnoses the org path: the org fetch flows
through the same `security_alert.observe` / `security_alert.fetch` spans and the
same `SecurityAlertProviderRequests`, `SecurityAlertFetchDuration`,
`SecurityAlertRateLimited`, and `SecurityAlertFactsEmitted` instruments, all
labeled by the existing bounded `provider`, `status_class`, and `fact_kind`
dimensions. The only telemetry addition is a bounded
`eshu.security_alert.target_scope` span attribute (`repository` or `org`) so an
operator can tell org fan-out work from per-repo polls; it is a span attribute,
not a metric label, so metric cardinality is unchanged. No new metric, no new
metric label, and no new worker, queue, lease, or graph write are introduced.
