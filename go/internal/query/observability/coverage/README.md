# observability/coverage

## Purpose

Serves `GET /api/v0/observability/coverage/correlations`: reducer-owned
observability coverage correlations for one monitored resource or service.
See `doc.go` for the contract.

## Ownership boundary

Owns the route, its request validation, and the Postgres read model of
`reducer_observability_coverage_correlation` facts. The reducer that writes
those facts, and the MCP tool in `internal/mcp/observability/coverage`, are
outside this package.

## Layout

- `handler.go` — `Handler`, `CorrelationResult`, validation and paging.
  At least one anchor filter is required; see `doc.go`.
- `correlations.go` — `ObservabilityCorrelationStore`, `CorrelationFilter`,
  `CorrelationRow`, `PostgresCorrelationStore` and its two SQL queries.
- `capability.go` — `Capability` and `Support`, the single declaration of the
  `observability.coverage.correlations.list` row.
- `handler_tracing.go` — this package's handler span seam.

## Dependencies

`internal/query/querycontract`, `internal/query/auth` (scoped grants),
`internal/query/tracing`, `internal/telemetry`.

## Telemetry

Span `telemetry.SpanQueryObservabilityCoverageCorrelations` per request, with
`http.route` and `eshu.capability` attributes, from the shared handler tracer.
Unchanged by the #6642 move.

## Gotchas / invariants

- The store interface is named `ObservabilityCorrelationStore`, not
  `CorrelationStore`: `internal/mcp`'s route-serves-data registry matches store
  types by name, and a bare `CorrelationStore` also matches the kubernetes,
  service-catalog, CI/CD and codeowners correlation stores.
- `internal/mcp/route_serves_data_registry_routes.go` pins this route's file
  paths and type names; a rename or file move here must update it.

## Move evidence (#6642)

`observability_coverage.go`, `observability_coverage_correlations.go` and
their test moved here as `handler.go`, `correlations.go` and
`correlations_test.go` (`git mv`). Names dropped the `ObservabilityCoverage`
prefix the path now carries. The handler's root forwarders
(`QueryParam`, `WriteError`, `StringVal`, ...) became direct `querycontract`
calls, and the test's auth helpers direct `auth` calls. The root test
helper `openScopeQueryerTestDB` moved to `testutil.OpenScopeQueryerTestDB`
so this package's tests can use it.

## No-Regression Evidence

No-Regression Evidence: both SQL queries, their parameters, the 200-row cap and
the decode path are unchanged; only package qualifiers and names differ.
`go test ./internal/query/... ./internal/mcp/... ./cmd/...` passes.

## No-Observability-Change

No-Observability-Change: same span name and tracer as the root handler; no
metric or log changes.

## Failed reads (#7674)

A failed store read on `GET /api/v0/observability/coverage/correlations`
answers the fixed `list observability coverage correlations failed`, never
the backend error text. `querycontract.WriteGraphReadError` runs first (a
stale or timed-out reader answers the retryable 503 with `Retry-After`),
then `tracing.WriteServerFailure`: `500` with the error on the handler span,
or `499` when the caller canceled the request.

No-Regression Evidence (#7674): the change runs only after a read has already
returned an error. No SQL, query parameter, call count, row bound, or success
path changed. A failure now costs one span `RecordError`/`SetStatus` and a
fixed-string write instead of formatting the error into the body.
`go test ./internal/query/... ./internal/queryplan/... -count=1` and
`go test -race ./internal/query/observability/coverage/...` exit 0.

Observability Evidence (#7674): a server fault records the backend error on
the handler span as an `exception` event and sets status Error with the fixed
message; a client cancel adds `eshu.request.client_canceled`, leaves the
status Unset, and answers `499`. `server_failure_test.go` asserts both span
shapes with a recording tracer.

## Related docs

- [Remote validation](../../../../../docs/internal/remote-validation/prod-observability-coverage-correlations.md)
- [Read models](../../read-models.md)

The correlation store exposes `NewPostgresCorrelationStoreWithReadStore` for a
guarded query-only reader; the legacy constructor remains available.
