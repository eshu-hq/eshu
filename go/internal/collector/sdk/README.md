# Collector SDK

## Purpose

`internal/collector/sdk` holds shared first-party collector helpers for bounded
HTTP execution, safe provider failures, retry-after parsing, and common
failure-class decisions. It exists to keep provider collectors focused on source
truth, pagination, normalization, and redaction instead of duplicating the same
kernel code.

## Ownership boundary

This package owns reusable collector-kernel mechanics only. It does not own
provider endpoint traversal, fact envelope construction, source-specific
redaction, workflow claims, Postgres commits, graph writes, reducers, query
surfaces, or extension-host process protocols.

## Exported surface

See `doc.go` for the godoc package contract. The public surface includes:

- `Version` for the first internal SDK contract line.
- `FailureClass`, shared failure constants, `StatusPolicy`, and
  `ProviderFailure`.
- `HTTPError`, `HTTPDoer`, `JSONRequest`, `ParseBaseURL`,
  `DefaultHTTPClient`, `ParseRetryAfter`, `ParseRetryAfterHeader`,
  `ShouldRetryStatus`, and `DoJSON`.

## Dependencies

The package depends only on the Go standard library. It intentionally imports no
Eshu storage, facts, workflow, telemetry, reducer, query, or graph packages.

## Telemetry

This package emits no telemetry directly. Callers keep provider-specific spans,
metrics, and low-cardinality labels in their owning collector packages.

## Gotchas / invariants

- `HTTPError.Error` never includes provider response bodies.
- `DoJSON` retries only statuses selected by `ShouldRetryStatus` or the caller's
  override, and it closes every response body before retrying or returning.
- `ParseBaseURL` rejects credential-bearing URLs before any request can be
  built.
- Provider collectors may wrap these helpers, but source-specific redaction and
  warning semantics stay local to the provider package.

## Related docs

- `docs/public/guides/collector-authoring.md`
- `docs/public/deployment/service-runtimes-collectors.md`
- `docs/internal/design/1821-collector-extension-sdk-contracts.md`

## Evidence

Line-count marker: before extraction, the four initial adopters (`jira`,
`pagerduty`, `grafana`, and `tempo`) carried 1,727 lines across their local
`failure.go` and `http_client.go` files. After extraction, provider-specific
REST traversal is 1,262 lines across adopter `client.go` files plus Jira's
partial collection wrapper, and the shared SDK production code is 400 lines.
The existing adopter surface is 65 lines smaller while future collectors reuse
the shared request and failure kernel instead of copying it.

No-Regression Evidence: `go test ./internal/collector/sdk
./internal/collector/jira ./internal/collector/pagerduty
./internal/collector/grafana ./internal/collector/tempo -count=1` covers SDK
base URL validation, retry-after parsing, bounded HTTP retries, safe HTTP
errors, and the four adopters' existing pagination, redaction, warning,
rate-limit, and provider failure behavior.

No-Observability-Change: the SDK emits no telemetry directly. Jira, PagerDuty,
Grafana, and Tempo keep their existing provider request counters, fetch
duration histograms, rate-limit counters, fact counters, and provider spans;
metric labels remain bounded to provider, status class, fact kind, and existing
low-cardinality dimensions.
