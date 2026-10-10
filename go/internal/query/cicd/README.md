# cicd

## Purpose

Serves the CI/CD run correlation reads: `GET /api/v0/ci-cd/run-correlations`
and its cheap-summary aggregates
`GET /api/v0/ci-cd/run-correlations/count` and
`GET /api/v0/ci-cd/run-correlations/inventory`. See `doc.go` for the contract.

## Ownership boundary

Owns the three routes, their request validation, the Postgres reducer read
model (`PostgresRunCorrelationStore` and
`PostgresRunCorrelationAggregateStore`), the run-artifact evidence summary
assembly, and the collector-readiness attach step. The static workflow
artifact evidence itself lives in `internal/query/repositoryartifacts`; the
row/filter/result vocabulary lives in `internal/query/querycontract`.

## Layout

- `handler.go` — `Handler`, the list route, scope validation, access
  scoping, and the empty-page short circuits.
- `run_correlations.go` — `PostgresRunCorrelationStore`, the bounded page
  query, and row decoding.
- `run_correlation_aggregates.go` — `RunCorrelationAggregateStore`, the
  count and grouped-inventory queries.
- `run_correlation_aggregates_handler.go` — the count and inventory routes.
- `evidence_summary.go` — `runCorrelationEvidenceSummary`, the live/static
  evidence bridge.
- `collector_readiness.go` — family-local attach step; must stay
  behavior-identical to root's (see Gotchas).
- `capability.go` — `Capability`, `AggregateCapability` and `Support`, the
  single declaration of both rows.

## Dependencies

`internal/query/querycontract` (capability gate, filter/row/result types,
response writers, truth envelope, readiness envelope builders),
`internal/query/selector` (repository-selector resolution, called directly —
a leaf cannot import root),
`internal/query/repositoryartifacts` (static workflow artifact evidence),
`internal/scope`, `internal/telemetry`. None imports this package, so there
is no cycle.

## Telemetry

Each route opens its handler span through `handler_tracing.go`
(`telemetry.SpanQueryCICDRunCorrelations`,
`telemetry.SpanQueryCICDRunCorrelationAggregate`). Unchanged by the #6642
move.

## Gotchas / invariants

- `NewPostgresRunCorrelationStoreWithReadStore` accepts a query-only reader
  for list requests; `NewPostgresRunCorrelationAggregateStoreWithReadStore`
  accepts a guarded read store for count and inventory requests. The legacy
  constructors keep their `*sql.DB` callers working.
- `collector_readiness.go` is a behavior-identical copy of the sibling
  families' attach step (the root copy is deleted — no package-query handler
  uses it anymore). Drift trips root's
  `TestCollectorListReadinessMatchesHub` parity test. Do not extend it with
  family-specific semantics.
- The list and aggregate routes pass a literal nil graph to
  `selector.ResolveForRequestWithAccess`: the read model needs no graph
  traversal, so selector resolution must not issue a live graph read here.
- An empty grant gets the empty page without a store read; a selector
  outside the grant is a 404 that leaks nothing.
- These are Postgres reads, not graph reads: nothing here registers in
  `internal/queryplan`.

## Move evidence (#6642)

`ci_cd.go`, `ci_cd_evidence_summary.go`,
`ci_cd_run_correlation_aggregates.go`,
`ci_cd_run_correlation_aggregates_handler.go` and
`ci_cd_run_correlations.go` moved here as `handler.go`, `evidence_summary.go`,
`run_correlation_aggregates.go`, `run_correlation_aggregates_handler.go` and
`run_correlations.go` (`git mv`), with their tests as
`run_correlations_test.go`,
`run_correlations_environment_evidence_test.go`,
`run_correlation_aggregates_test.go`,
`run_correlation_aggregates_count_coverage_test.go` and
`evidence_summary_artifact_test.go`. `CICDHandler` became `Handler`; the
`CICD`-prefixed store, filter, count, inventory and double names dropped the
stutter. The SQL predicate-order test moved here as `queries_test.go`.
`ci_cd_authz_test.go`, `ci_cd_story_parity_test.go` and
`ci_cd_story_readback_test.go` stay in root: they drive auth middleware and
other families' surfaces. The dead root selector forwarder
`resolveRepositorySelectorForRequestWithAccess` was removed — every family,
including this one, calls `selector.ResolveForRequestWithAccess` directly.

## No-Regression Evidence

No-Regression Evidence: both SQL query families, their parameters, the list
bounds, the grant handling, the evidence summary assembly and the response
shapes are unchanged; only package qualifiers and names differ.

## No-Observability-Change

No-Observability-Change: no span, metric or log is added or removed.

## Failed reads (#7674)

A failed store read answers a fixed message per route (`list CI/CD run
correlations failed`, `count CI/CD run correlations failed`, `read CI/CD run
correlation inventory failed`), never the backend error text.
`writeCICDReadFailure` runs `querycontract.WriteGraphReadError` first, so a
stale or timed-out PostgreSQL reader answers the retryable `503` with
`Retry-After`, then `tracing.WriteServerFailure`: `500` with the error on the
handler span, or `499` when the caller canceled the request.
`server_failure_test.go` covers each route.

No-Regression Evidence (#7674): the change runs only after a read has already
returned an error. No SQL, query parameter, call count, row bound, or success
path changed. A failure now costs one span `RecordError`/`SetStatus` and a
fixed-string write instead of formatting the error into the body.
`go test ./internal/query/... ./internal/queryplan/... -count=1` and
`go test -race ./internal/query/cicd/...` exit 0.

Observability Evidence (#7674): a server fault records the backend error on
the handler span as an `exception` event and sets status Error with the
route's fixed message; a client cancel adds `eshu.request.client_canceled`,
leaves the status Unset, and answers `499`. `server_failure_test.go` asserts
both span shapes with a recording tracer.

## Related docs

- [Query package README](../README.md)
