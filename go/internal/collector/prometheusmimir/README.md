# Prometheus/Mimir Collector

## Purpose

`internal/collector/prometheusmimir` collects bounded live Prometheus and
Grafana Mimir metadata for observability evidence. It gives Eshu no-IaC
fallback, drift candidates, and freshness validation without treating provider
state as declared GitOps truth.

## Ownership boundary

This package owns Prometheus-compatible API target validation, metadata
normalization, fact envelope construction, and source-local failure
classification. It does not write graph state, compare
declared/applied/observed evidence, or decide service coverage truth; reducers
and query surfaces own those decisions.

## Exported surface

- `ClaimedSource` and `NewClaimedSource` implement the workflow-claim source.
- `HTTPClient` and `NewHTTPClient` read bounded Prometheus-compatible REST
  endpoints.
- `NewSourceInstanceEnvelope`, `NewObservedTargetEnvelope`,
  `NewObservedRuleEnvelope`, and `NewCoverageWarningEnvelope` build
  observability fact envelopes.
- `TargetConfig`, `SourceConfig`, `CollectionResult`, `Target`, `Rule`, and
  `Warning` define the collector contract. See `doc.go` for the godoc package
  contract.

## Dependencies

The package depends on `internal/collector` for `CollectedGeneration`,
`internal/facts` for fact envelopes and stable IDs, `internal/scope` for scope
generation identity, `internal/workflow` for claim input, and
`internal/telemetry` for bounded collector metrics and spans.

## Telemetry

- Spans: `prometheus_mimir.observe`, `prometheus_mimir.fetch`.
- Metrics: `eshu_dp_prometheus_mimir_provider_requests_total`,
  `eshu_dp_prometheus_mimir_fetch_duration_seconds`,
  `eshu_dp_prometheus_mimir_facts_emitted_total`,
  `eshu_dp_prometheus_mimir_rate_limited_total`,
  `eshu_dp_prometheus_mimir_retries_total`,
  `eshu_dp_prometheus_mimir_redactions_total`, and
  `eshu_dp_prometheus_mimir_stale_total`.

Labels stay bounded to provider, status class, fact kind, and redaction reason.
Instance IDs, target URLs, label values, raw PromQL, tenant IDs, tenant
headers, and token values must not appear in metric labels.

## Gotchas / invariants

- Live facts are `source_class=observed`; they do not replace declared IaC
  evidence when declared evidence is present and current.
- `HTTPClient` redacts or drops scrape URLs, target label values, discovered
  label values, raw PromQL, annotations, tenant IDs, and tenant headers before
  returning normalized results.
- Prometheus targets are read from `/api/v1/targets?state=active`; Mimir skips
  target collection because Mimir does not expose Prometheus scrape-target
  state.
- Mimir tenant IDs are request headers only. Facts keep tenant presence and a
  fingerprint, not the raw tenant value.
- Rules or targets older than `TargetConfig.StaleAfter` are emitted as stale
  observed facts plus stale coverage warnings. A zero stale window disables
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

Observability Evidence: operators can diagnose live Prometheus/Mimir reads
through `prometheus_mimir.observe` / `prometheus_mimir.fetch`, provider request
counts, fetch duration, facts emitted by fact kind, stale counts, rate-limit
counts, retry counts, and metadata redaction counts.
