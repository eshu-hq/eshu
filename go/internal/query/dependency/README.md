# dependency

## Purpose

Serves `GET /api/v0/dependencies`: a bounded, graph-backed inventory of
package dependency edges, forward or reverse. See `doc.go` for the contract.

## Ownership boundary

Owns the route, its Cypher (`cypher.go`) and its response shape (`Row`). It
does not own repository ownership truth, which is a reducer correlation
concern, or the package-registry family's own dependency reads
(`internal/query/package/registry`).

## Layout

- `handler.go` — `Handler`, `Row`, request validation, paging and telemetry.
- `cypher.go` — the forward and reverse traversals and their parameters.
  `ForwardCypher` is exported for root's plan-binding test.
- `capability.go` — `Capability` and `Support`, the single declaration of the
  `dependencies.list` row. `contract/capability_matrix.go` registers it for
  production; `main_test.go` registers it for this package's tests, which
  never link root.
- `handler_tracing.go` — this package's handler span seam.
- retired evidence record — the route's query-shape contract and measured
  proof (#1646), moved here from the query root with the handler.

## Dependencies

`internal/query/querycontract` (profiles, response writers, row decoders,
truth envelope), `internal/query/tracing` (handler span), and
`internal/telemetry` (span name and instruments).

## Telemetry

- Span `telemetry.SpanQueryDependencies` per request, with `http.route` and
  `eshu.capability` attributes.
- `DependencyListDuration` histogram and `DependencyListErrors` counter, both
  tagged with `direction`.

Unchanged by the #6642 move: same span name, same tracer (`tracing.HandlerTracer`),
same instruments and attributes.

## Gotchas / invariants

- Reverse direction without `package` is a 400, not an unanchored scan.
- `after_name` and `after_edge` must arrive together.
- The queryplan manifest pins `listDependencies` by source hash
  (`QP-SC-DEPS`); any body edit re-pins it.

## Move evidence (#6642)

`dependencies.go`, `dependencies_cypher.go` and `dependencies_test.go` moved
here as `handler.go`, `cypher.go` and `handler_test.go` (`git mv`). Exported
names dropped the package-name stutter: `DependenciesHandler` -> `Handler`,
`DependencyRow` -> `Row`. Root identifiers the handler called
(`QueryParam`, `WriteError`, `StringVal`, `BuildTruthEnvelope`, ...) were thin
forwarders onto `querycontract`, so the handler now calls `querycontract`
directly and behaves the same. The `dependencies.list` row moved out of the
capability matrix literal into `Support()` with identical values, so the
family's own tests can register it without a copy.

## Failed reads (#7674)

A failed graph read on `GET /api/v0/dependencies` answers the fixed
`dependency graph read failed`, never the backend error text.
`querycontract.WriteGraphReadError` runs first (fence or graph-availability
verdicts answer 503/504), then `tracing.WriteServerFailure`: `500` with the
error on the handler span, or `499` when the caller canceled the request.
The `listDependencies` source digest in `query-source-coverage.yaml` was
re-pinned for this body edit; its Cypher is unchanged.

## Related docs

- [HTTP API: evidence and supply chain](../../../../docs/public/reference/http-api/evidence-and-supply-chain.md)
- OpenAPI path: `internal/query/openapi/paths/repository/dependencies.go`
