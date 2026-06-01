# Loki Collector

## Purpose

`internal/collector/loki` collects bounded live Grafana Loki metadata for
observability evidence. It gives Eshu no-IaC fallback, drift candidates, and
freshness validation without treating provider state as declared GitOps truth.

## Ownership boundary

This package owns Loki API target validation, metadata normalization, fact
envelope construction, and source-local failure classification. It does not
write graph state, compare declared/applied/observed evidence, or decide service
coverage truth; reducers and query surfaces own those decisions.

## Exported surface

- `ClaimedSource` and `NewClaimedSource` implement the workflow-claim source.
- `HTTPClient` and `NewHTTPClient` read bounded Loki REST endpoints.
- `NewSourceInstanceEnvelope`, `NewObservedLogSignalEnvelope`,
  `NewObservedRuleEnvelope`, and `NewCoverageWarningEnvelope` build
  observability fact envelopes.
- `TargetConfig`, `SourceConfig`, `CollectionResult`, `LogSignal`, `Rule`, and
  `Warning` define the collector contract. See `doc.go` for the godoc package
  contract.

## Dependencies

The package depends on `internal/collector` for `CollectedGeneration`,
`internal/facts` for fact envelopes and stable IDs, `internal/scope` for scope
generation identity, `internal/workflow` for claim input, and
`internal/telemetry` for bounded collector metrics and spans.

## Telemetry

- Spans: `loki.observe`, `loki.fetch`.
- Metrics: `eshu_dp_loki_provider_requests_total`,
  `eshu_dp_loki_fetch_duration_seconds`, `eshu_dp_loki_facts_emitted_total`,
  `eshu_dp_loki_rate_limited_total`, `eshu_dp_loki_retries_total`,
  `eshu_dp_loki_redactions_total`,
  `eshu_dp_loki_high_cardinality_rejected_total`, and
  `eshu_dp_loki_stale_total`.

Labels stay bounded to provider, status class, fact kind, and redaction or
rejection reason. Instance IDs, private URLs, raw LogQL, label values, tenant
IDs, tenant headers, token values, and provider response bodies must not appear
in metric labels.

## Gotchas / invariants

- Live facts are `source_class=observed`; they do not replace declared IaC
  evidence when declared evidence is present and current.
- `HTTPClient` uses Loki metadata endpoints only: labels, allowlisted label
  values, series metadata, and ruler rule metadata. It does not call log query
  endpoints that return stream entries.
- Label values are counted and fingerprinted only when explicitly allowlisted
  and within the configured cardinality bound. High-cardinality values become
  `observability.coverage_warning` facts instead of raw payload fields.
- Loki tenant IDs are request headers only. Facts keep tenant presence and a
  fingerprint, not the raw tenant value.
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

Observability Evidence: operators can diagnose live Loki reads through
`loki.observe` / `loki.fetch`, provider request counts, fetch duration, facts
emitted by fact kind, high-cardinality rejection counts, stale counts,
rate-limit counts, retry counts, and metadata redaction counts.
