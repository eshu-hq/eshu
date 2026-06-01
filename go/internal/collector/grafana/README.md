# Grafana Collector

## Purpose

`internal/collector/grafana` collects bounded live Grafana metadata for
observability evidence. It gives Eshu no-IaC fallback, drift candidates, and
freshness validation without treating provider state as declared GitOps truth.

## Ownership boundary

This package owns Grafana API target validation, metadata normalization, fact
envelope construction, and source-local failure classification. It does not
write graph state, compare declared/applied/observed evidence, or decide service
coverage truth; reducers and query surfaces own those decisions.

## Exported surface

- `ClaimedSource` and `NewClaimedSource` implement the workflow-claim source.
- `HTTPClient` and `NewHTTPClient` read bounded Grafana REST endpoints.
- `NewSourceInstanceEnvelope`, `NewObservedResourceEnvelope`,
  `NewObservedRuleEnvelope`, and `NewCoverageWarningEnvelope` build
  observability fact envelopes.
- `TargetConfig`, `SourceConfig`, `CollectionResult`, `Resource`, `AlertRule`,
  and `Warning` define the collector contract. See `doc.go` for the godoc
  package contract.

## Dependencies

The package depends on `internal/collector` for `CollectedGeneration`,
`internal/facts` for fact envelopes and stable IDs, `internal/scope` for scope
generation identity, `internal/workflow` for claim input, and
`internal/telemetry` for bounded collector metrics and spans.

## Telemetry

- Spans: `grafana.observe`, `grafana.fetch`.
- Metrics: `eshu_dp_grafana_provider_requests_total`,
  `eshu_dp_grafana_fetch_duration_seconds`,
  `eshu_dp_grafana_facts_emitted_total`,
  `eshu_dp_grafana_rate_limited_total`, `eshu_dp_grafana_retries_total`, and
  `eshu_dp_grafana_redactions_total`.

Labels stay bounded to provider, status class, fact kind, and redaction reason.
Instance IDs, dashboard titles, URLs, datasource URLs, query bodies, contact
points, and token values must not appear in metric labels.

## Gotchas / invariants

- Live Grafana facts are `source_class=observed`; they do not replace declared
  IaC evidence when declared evidence is present and current.
- `HTTPClient` redacts or drops datasource URLs, dashboard URLs, alert query
  models, contacts, and notification destinations before returning normalized
  results.
- Search pagination is bounded and duplicate pages converge through
  provider-UID dedupe before fact emission.
- Alert rules older than `TargetConfig.StaleAfter` are emitted as stale
  observed-rule facts plus stale coverage warnings. A zero stale window disables
  stale classification.
- Disabled targets are skipped before client construction, so operators can
  configure the collector without enabling live provider reads.

## Related docs

- `docs/public/reference/observability-evidence.md`
- `docs/public/reference/fact-envelope-reference.md`
- `docs/public/deployment/service-runtimes-collectors.md`
- `docs/public/reference/telemetry/metrics-ingestion-collectors.md`

No-Regression Evidence: this slice adds a source-fact collector package and
bounded telemetry only. It does not add graph writes, reducer phases, query
handlers, Helm wiring, or a long-running runtime command.

Observability Evidence: operators can diagnose live Grafana reads through
`grafana.observe` / `grafana.fetch`, provider request counts, fetch duration,
facts emitted by fact kind, rate-limit counts, retry counts, and metadata
redaction counts.
