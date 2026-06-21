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
`source_confidence=reported`, and a `repository_id` in `payload` derived to the
same canonical `security-alert:github:<owner>/<repo>` scope the per-repository
path and the `security_alert_reconciliation` reducer already key on. Reducer
reconciliation is therefore unchanged.

**P1 scope fix (PR review #3447903339):** org fan-out envelopes now carry the
org generation scope in `envelope.ScopeID` (e.g.
`security-alert:github-org:example-org`) so the Postgres streaming writer's
per-envelope scope check (`envelope.ScopeID == committed_scope_id`) passes. The
per-repository scope is threaded via the new `EnvelopeContext.RepositoryID`
field into `payload["repository_id"]` and the `stableFactKey` for idempotent
dedup and reducer keying. The per-repository code path is unaffected:
`ctx.RepositoryID` is empty for repo targets, so `repositoryID` falls back to
`ctx.ScopeID` (identical to the previous behaviour).

**P1 allowlist fix (PR review #3447903341):** org targets now require a
non-empty `allowed_repositories` at construction time (validated in both
`alertruntime.validateTarget` and `workflow.validateSecurityAlertTargetConfiguration`).
Fan-out skips alerts for repositories absent from the allowlist, enforcing the
same private-data boundary that per-repository targets enforce. Operator-declared
allowlist bounds the blast radius of a token with org visibility.

Verified by:

- `go test ./internal/collector/securityalerts/... -count=1` — 32 passed,
  including new regression tests
  `TestClaimedSourceOrgAlertEnvelopeScopeIDMatchesCommittedGenerationScope`
  (P1 #1), `TestValidateOrganizationTargetRequiresAllowedRepositories` (P1 #2),
  and `TestClaimedSourceFiltersOrgAlertsByAllowlist` (P1 #2 filtering).
- `go test ./internal/workflow/... ./cmd/collector-security-alerts/... -count=1`
  — 185 passed, including new
  `TestValidateSecurityAlertCollectorConfigurationRejectsInvalidOrganizationTargets/missing_allowed_repositories_for_org_scope`.
- `go test ./internal/reducer/... -run SecurityAlert -count=1` — 24 passed.
- `go vet` on all changed packages — no issues.

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
