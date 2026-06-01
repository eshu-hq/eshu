# Tempo Collector

## Purpose

`internal/collector/tempo` collects metadata-only evidence from live Grafana
Tempo APIs. It supports teams without source-controlled observability config and
gives reducers freshness or drift evidence to compare against declared and
applied observability facts.

## Ownership boundary

The package owns Tempo source observation, target validation, source fact
envelopes, and bounded provider failure classification. It does not fetch
traces, spans, raw trace IDs, request attributes, TraceQL bodies, or trace
search payloads, and it does not make graph or coverage truth decisions.
Reducers and query surfaces compare these facts with declared and applied
evidence.

## Exported surface

See `doc.go` for the godoc contract.

- `NewHTTPClient` builds a read-only Tempo REST client.
- `HTTPClient.CollectObservedMetadata` reads `/api/echo`,
  `/api/v2/search/tags`, and configured `/api/v2/search/tag/<tag>/values`
  endpoints.
- `NewClaimedSource` builds a workflow-claim source for the hosted collector
  runtime.
- `NewSourceInstanceEnvelope`, `NewObservedTraceSignalEnvelope`, and
  `NewCoverageWarningEnvelope` build observability fact envelopes.

## Dependencies

- `internal/collector` for `FactsFromSlice` and claim-source generation output.
- `internal/facts` for observability fact kinds, schema versions, stable IDs,
  and source confidence.
- `internal/scope` for source scope and generation identity.
- `internal/telemetry` for Tempo spans, provider request counters, fact counters,
  redaction counters, retry/rate-limit counters, and fetch duration histograms.
- `internal/workflow` for claim identity and fencing token propagation.

## Telemetry

Spans:

- `tempo.observe`
- `tempo.fetch`

Metrics:

- `eshu_dp_tempo_provider_requests_total`
- `eshu_dp_tempo_facts_emitted_total`
- `eshu_dp_tempo_rate_limited_total`
- `eshu_dp_tempo_retries_total`
- `eshu_dp_tempo_redactions_total`
- `eshu_dp_tempo_high_cardinality_rejected_total`
- `eshu_dp_tempo_stale_total`
- `eshu_dp_tempo_fetch_duration_seconds`

Metric labels are bounded to provider, status class, and fact kind. Tenant IDs,
URLs, tag values, trace IDs, query text, and provider response bodies stay out
of labels.

## Gotchas / invariants

Tempo search and trace endpoints can return trace IDs, spans, attributes, and
TraceQL-shaped payloads. This package only uses metadata endpoints and stores
tag values as counts plus fingerprints when an operator explicitly allowlists a
tag. High-cardinality tag-value reads emit `observability.coverage_warning`
facts and do not persist value hashes.

The selected Tempo tag endpoints expose `limit` bounds but no page cursor.
Duplicate provider rows and repeated scope/tag responses are deduplicated before
fact emission.

## Related docs

- `docs/public/reference/observability-evidence.md`
- `docs/public/reference/fact-envelope-reference.md`
- `docs/public/reference/telemetry/metrics-ingestion-collectors.md`
- `docs/public/deployment/service-runtimes-collectors.md`
