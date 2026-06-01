# Jira Collector

## Purpose

`internal/collector/jira` owns Jira Cloud work-item source evidence for Eshu. It
reads bounded updated windows, fetches each changed issue's changelog and remote
links, and emits `work_item.record`, `work_item.transition`, and
`work_item.external_link` facts.

## Ownership boundary

This package collects Jira source facts only. It does not call PagerDuty,
GitHub, deployment systems, graph backends, or query handlers. Jira facts can
enrich incident context later when another reducer or query can prove a link,
but a PagerDuty incident does not need a Jira ticket to be useful.

Project, status, workflow, and field metadata expansion is not implemented in
this package yet. That work must add `work_item.*` fact names, schema helpers,
and fixtures before provider calls change.

## Exported surface

See `doc.go` for the godoc package contract. The main exported API is:

- `ClaimedSource` and `NewClaimedSource` for workflow-claim-driven collection
- `HTTPClient` and `NewHTTPClient` for bounded Jira Cloud REST reads
- `NewWorkItemRecordEnvelope`, `NewWorkItemTransitionEnvelope`, and
  `NewWorkItemExternalLinkEnvelope` for source-fact envelope construction
- `EnvelopeContext`, `Issue`, `Transition`, `ExternalLink`,
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

Metric labels stay low-cardinality: provider, status class, and fact kind are
allowed. Site IDs, issue keys, summaries, user identities, URLs, and credential
values are not allowed in metric labels or status errors.

## Gotchas / invariants

- Target credentials are resolved from environment variables named by
  `token_env` and optional `email_env`. The token value is never included in
  facts, metric labels, status errors, or requested scope sets.
- Remote-link URLs and Jira self/browse URLs have sensitive query parameters
  removed before they enter envelopes.
- Duplicate remote links inside one issue collection are collapsed by provider
  link ID, global ID, or URL.
- Empty Jira projects or updated windows commit a successful empty generation.
- Signed Jira webhooks wake configured collector scopes through the shared
  webhook/workflow path; this package still treats polling as the backfill
  source of truth.

No-Regression Evidence: focused collector tests cover envelope redaction,
provider failure classification, empty windows, and bounded REST endpoints.
No-Observability-Change: the evidence contract adds no new runtime stage or
metric; existing collector, workflow, status, and runtime signals diagnose the
current Jira collection path.

## Related docs

- `docs/public/reference/jira-evidence.md`
- `docs/public/reference/fact-envelope-reference.md`
- `docs/public/reference/collector-reducer-readiness.md`
- `docs/public/deployment/service-runtimes-collectors.md`
