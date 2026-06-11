# Jira Collector

## Purpose

`internal/collector/jira` owns Jira Cloud work-item source evidence for Eshu. It
reads bounded updated windows, fetches each changed issue's changelog and remote
links, reads bounded project/status/workflow/field metadata, and emits
`work_item.record`, `work_item.transition`, `work_item.external_link`, and
metadata `work_item.*` facts.

## Ownership boundary

This package collects Jira source facts only. It does not call PagerDuty,
GitHub, deployment systems, graph backends, or query handlers. Jira facts can
enrich incident context later when another reducer or query can prove a link,
but a PagerDuty incident does not need a Jira ticket to be useful.

Project, status, workflow, issue-type, and field metadata remain source
context only. This package fingerprints private names, descriptions, URLs, and
custom-field identifiers; reducers and query surfaces decide how that context
explains incidents, pull requests, commits, deployments, and services.

## Exported surface

See `doc.go` for the godoc package contract. The main exported API is:

- `ClaimedSource` and `NewClaimedSource` for workflow-claim-driven collection
- `HTTPClient` and `NewHTTPClient` for bounded Jira Cloud REST reads
- `NewWorkItemRecordEnvelope`, `NewWorkItemTransitionEnvelope`,
  `NewWorkItemExternalLinkEnvelope`, and metadata envelope constructors for
  source-fact envelope construction
- `EnvelopeContext`, `Issue`, `Transition`, `ExternalLink`, metadata model types,
  `CollectionWindow`, `CollectionResult`, `SourceConfig`, and `TargetConfig`
  for source and envelope input shape
- `ProviderFailure`, `JiraError`, and failure-class constants for bounded
  workflow retry classification

## Dependencies

- `internal/collector` for `CollectedGeneration` and claimed-service handoff
- `internal/facts` for durable envelopes, fact kinds, schema versions, and
  stable IDs
- `internal/scope` for collector-kind validation
- `internal/telemetry` for Jira spans and metric instruments
- Go `net/http` and `encoding/json` for Jira Cloud REST calls

## Telemetry

Spans:

- `jira.observe`
- `jira.fetch`

Metrics:

- `eshu_dp_jira_provider_requests_total`
- `eshu_dp_jira_facts_emitted_total`
- `eshu_dp_jira_rate_limited_total`
- `eshu_dp_jira_fetch_duration_seconds`

The `jira.fetch` span also carries bounded page and output counters:
`jira.search_pages`, `jira.changelog_pages`, `jira.remote_link_pages`,
`jira.metadata_pages`, `jira.metadata_objects_scanned`,
`jira.metadata_objects_emitted`, `jira.unsupported_metadata`,
`jira.permission_hidden_metadata`, `jira.stale_metadata`,
`jira.metadata_redactions`,
`jira.issues_emitted`, `jira.changelog_events_emitted`,
`jira.remote_links_emitted`, `jira.remote_links_rejected`,
`jira.unsupported_provider_links`, `jira.partial_failures`,
`jira.rate_limits`, `jira.retry_after_seconds`, and `jira.stale_windows`.

Metric labels stay low-cardinality: provider, status class, and fact kind are
allowed. Site IDs, issue keys, summaries, user identities, URLs, and credential
values are not allowed in metric labels or status errors.

## Gotchas / invariants

- Target credentials are resolved from environment variables named by
  `token_env` and optional `email_env`. The token value is never included in
  facts, metric labels, status errors, or requested scope sets.
- Remote-link URLs and Jira self/browse URLs have sensitive query parameters
  removed before normalization and are represented by fingerprints in
  envelopes.
- For a confidently typed GitHub pull-request or GitLab merge-request link, the
  collector resolves the URL to a canonical repository id via
  `repositoryidentity.CanonicalRepositoryID` *before* the raw URL is redacted
  and persists only that id as `linked_repository_id` on the
  `work_item.external_link` fact. The raw URL stays redacted (the existing
  `url`/`url_fingerprint`/`url_redacted` contract is unchanged). The id is the
  same generation-independent identifier Eshu stores for every repository and
  carries no raw URL, query parameter, credential, or user identity. Links that
  do not canonicalize to a known owner/repo (GitHub) or group/repo (GitLab)
  shape, or that are ambiguous, omit the field entirely — never a guessed id.
  This deliberate resolve-before-redact step is a privacy boundary approved
  under security review and is the durable join key for scoped work-item reads.
- Duplicate remote links inside one issue collection are collapsed by provider
  link ID, global ID, or URL.
- Empty Jira projects or updated windows commit a successful empty generation.
- Permission-hidden metadata emits `work_item.metadata_warning` so readers can
  distinguish hidden source context from a genuinely empty site.
- Signed Jira webhooks wake configured collector scopes through the shared
  webhook/workflow path; this package still treats polling as the backfill
  source of truth.
- `TestLiveJiraWorkItemEvidence` is an opt-in maintainer smoke. It requires
  `ESHU_JIRA_LIVE=1`, a Jira Cloud base URL, and private credential variables;
  default package tests skip it.

No-Regression Evidence: focused collector tests cover envelope redaction,
provider failure classification, empty windows, bounded REST endpoints, search
pagination, changelog pagination, metadata endpoint collection, permission-hidden
metadata warnings, malformed remote-link rejection, unsupported provider
classification, duplicate-window stable keys, visibility and archive status
classification, partial-failure stats, and Retry-After handling.

Collector Performance Evidence: `go test ./cmd/collector-jira
./internal/collector/jira ./internal/telemetry ./internal/facts -count=1`
proves the bounded Jira collection path with configured issue, changelog,
remote-link, and metadata limits. The collector still performs one bounded
updated-window search plus per-issue changelog and remote-link reads, and now
adds bounded metadata definition reads without graph writes, reducer work, or
unbounded provider calls.

Observability Evidence: the existing Jira metrics plus bounded `jira.fetch`
span counters diagnose pages scanned, issues emitted, changelog events emitted,
remote links emitted or rejected, metadata pages, metadata objects scanned or
emitted, unsupported metadata, permission-hidden metadata, stale metadata,
metadata redactions, unsupported provider links, rate limits, partial failures,
stale collection windows, and retry guidance without
high-cardinality labels.

Live-Smoke Evidence: `go test ./internal/collector/jira -run
TestLiveJiraWorkItemEvidence -count=1 -v` exercises the claim-backed source
against a configured Jira Cloud site when `ESHU_JIRA_LIVE=1`. It is skipped by
default and fails if credential material appears in emitted envelopes.

Collector Observability Evidence: `jira.observe` and `jira.fetch` spans,
`eshu_dp_jira_provider_requests_total`,
`eshu_dp_jira_facts_emitted_total`, `eshu_dp_jira_rate_limited_total`, and
`eshu_dp_jira_fetch_duration_seconds` expose collection attempts, provider
failures, emitted fact counts, rate limits, fetch duration, page counts,
partial failures, rejected links, stale windows, and retry guidance without
site IDs, issue keys, summaries, users, URLs, or credential values in metric
labels.

Collector Deployment Evidence: the hosted runtime is charted by
`deploy/helm/eshu/templates/deployment-jira-collector.yaml` with a metrics
Service, ServiceMonitor, NetworkPolicy, and PodDisruptionBudget. Runtime
credentials stay in Secret-backed environment variables referenced from
`jiraCollector.extraEnv`.

## Related docs

- `docs/public/reference/jira-evidence.md`
- `docs/public/reference/fact-envelope-reference.md`
- `docs/public/reference/collector-reducer-readiness.md`
- `docs/public/deployment/service-runtimes-collectors.md`
