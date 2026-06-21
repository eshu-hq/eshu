# Security Intelligence Provider Alert Parity

## Provider Alert Parity Gate

Provider-hosted alert parity is a validation gate, not a source of public test
data. For supported hosts, private validation may compare Eshu findings against
provider alerts for the same repositories and package ecosystems.

`eshu-collector-security-alerts` is the hosted provider security-alert runtime.
It is claim-driven and supports GitHub Dependabot alerts in two scopes selected
by the target `scope` field (default `repository`):

- `repository`-scoped targets poll one repository
  (`GET /repos/{owner}/{repo}/dependabot/alerts`) behind explicit credentials
  and a `allowed_repositories` allowlist. Every repository must appear in the
  target's `allowed_repositories` list before the runtime issues an HTTP request.
- `org`-scoped targets poll one organization
  (`GET /orgs/{org}/dependabot/alerts`) and fan the response out into
  per-repository `security_alert.repository_alert` facts keyed on each alert's
  source repository. One org target replaces N per-repository targets and
  collapses N per-repository requests into one paginated org request per
  collection, while producing the same per-repository fact shape so reducer
  reconciliation is unchanged.

Targets name a `token_env` rather than a token value. Any `api_base_url`
override must use HTTPS because the runtime sends the bearer token to that
endpoint. Both scopes share the same bounded cursor pagination, cross-host
next-link rejection, and rate-limit handling. Collection is bounded by
`repository_alert_limit` and `max_pages`, requests GitHub's open-alert view
directly, and surfaces `source_freshness=partial` plus a coverage summary when
the open-alert provider page cap is reached. Provider rate-limit responses are
surfaced as retryable workflow failures.

`security_alert.repository_alert` facts preserve repository-scoped provider
alert state from GitHub Dependabot. The runtime emits only that source fact
kind; provider alerts do not become canonical Eshu impact findings by
themselves. The `security_alert_reconciliation` reducer writes comparison rows
with provider state and Eshu impact state as separate fields:

- `matched` when the alert joins to owned dependency evidence and an Eshu
  impact finding for the same package/advisory.
- `unmatched` when the dependency is owned but no Eshu impact finding exists.
- `stale` when newer owned dependency evidence no longer matches the alert's
  manifest path.
- `dismissed` or `fixed` when the provider reports that state.
- `provider_only` when Eshu has no owned dependency evidence for the alert.
- `unsupported` when the provider alert names an ecosystem Eshu cannot match
  with the current impact matcher.
- `ambiguous` when multiple owned dependency evidence rows could match and
  Eshu refuses to guess.

Provider alert reconciliation reads require a repository, provider, package,
CVE, or GHSA anchor. Provider state and reconciliation status only filter an
anchored page; they are not standalone scopes.
List and count responses include a `coverage` object. `state=target_incomplete`
means at least one returned or counted reconciliation came from a truncated
open-alert provider read, so callers must not treat the count as complete.

Eshu should match provider alert counts when it has equivalent owned target
evidence and advisory data. Eshu may exceed provider alert output when it can
add code-to-cloud context, image/runtime impact, or additional advisory sources.
Any mismatch must classify whether the cause is missing target collection,
missing advisory ingestion, version-range matching, unsupported ecosystem,
provider-only behavior, or an Eshu reducer bug.

Public-safe readback for scoped npm alerts: synthetic `@scope/provider-owned`
fixtures prove a provider-only alert stays `provider_only` when no owned
dependency evidence exists, then promotes to `matched` reconciliation and a
reducer-owned `affected_exact` impact finding when Eshu has lockfile-proven
transitive dependency evidence for the exact scoped package name.

Operator-local `eshu vuln-scan provider-parity` runs this comparison across a
local repository allowlist and emits only aggregate-safe output. The public
summary includes repository count, provider alert count, Eshu finding count,
approved mismatch class counts, truncation, readiness state, and freshness
state. Row-level provider alerts, repository names, package names, advisory ids,
alert URLs, and raw Eshu finding rows stay in the operator-local evidence set.
If a row lands in `unclassified`, open a follow-up issue with private evidence
kept outside the public repo before claiming parity for that validation set.

No-Regression Evidence: `go test ./internal/collector/securityalerts ./internal/collector/securityalerts/alertruntime -count=1`
proves the provider client requests the `state=open` view, preserves cursor
pagination bounds, and marks truncated open-alert reads as partial source
freshness. `go test ./internal/reducer -run
'TestBuildSecurityAlertReconciliations|TestSecurityAlertReconciliationWriterUsesProviderAlertScopeForPackageTriggeredRepair'
-count=1` proves source freshness and collection coverage survive reducer
reconciliation payload publication. `go test ./internal/query -run
'TestSupplyChainListSecurityAlertReconciliations|TestDecodeSecurityAlertReconciliationRowPreservesProviderCoverage|TestSecurityAlertReconciliationAggregate'
-count=1` proves API/MCP-backed list and count responses expose partial
coverage without unbounded page reads.

No-Observability-Change: the fix reuses existing security-alert provider
request counters, fact-emitted counters, fetch-duration histograms,
`security_alert.observe`, `security_alert.fetch`, and the API/MCP response
envelope. No repository name, package name, alert URL, token environment name,
or token value is added to metric labels, status errors, or public docs.

Validation logs may record aggregate counts and mismatch classes. They must not
commit private repository names, package names, alert URLs, or copied provider
payloads to the public repo. Runtime metrics and spans use bounded provider,
status-class, and fact-kind labels; credentials are resolved from environment
variables and must not appear in facts, logs, metric labels, status errors, or
checked-in examples.
