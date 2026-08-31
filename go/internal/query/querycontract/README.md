# Query contracts

## Purpose

`querycontract` holds the stable types and small helpers that query families
need without depending on the root `query` package.

## Ownership boundary

This package owns query profiles, truth and error envelopes, freshness causes,
HTTP response helpers, the shared capability registry, and the graph/content
read ports. It does not own routes, handler orchestration, graph queries, or
Postgres implementations. Those remain in the root query package or a family
package.

## Exported surface

The exported surface is described in [doc.go](doc.go). Root `query` aliases the
types and wraps the functions so existing imports keep their current API.

## Dependencies

Every file here uses only the Go standard library except `handlerspan.go`, which
imports `go.opentelemetry.io/otel` (plus `otel/attribute` and `otel/trace`) and
`go/internal/telemetry` for the shared service-namespace attribute. `GraphQuery`
and `ContentStore` are consumer-owned ports; concrete adapters remain outside
this leaf package.

## Telemetry

`StartHandlerSpan` and `StartHandlerSpanWith` (`handlerspan.go`) start the
per-route span for query HTTP reads, tagged with `http.route`,
`eshu.capability`, and `service.namespace`. The tracer is named
`eshu/go/internal/query` — deliberately, even though the code lives here. That
name is what saved span queries and dashboards match on, so it tracks the
surface the spans describe rather than the directory the code sits in. The row
in `docs/public/observability/telemetry-coverage.md` points at
`handlerspan.go`.

`StartHandlerSpanWith` takes the tracer as an argument rather than reading the
package-level `HandlerTracer`. Package `query` keeps its own swappable
`queryHandlerTracer` var, which six span tests replace with a recording
provider; reading `HandlerTracer` directly here would ignore that swap and the
tests would observe zero spans while the handler quietly emitted them.

Nothing else in the package emits metrics, spans, or logs.

No-Observability-Change: the span name, its attributes, and the tracer name are
unchanged from when this code lived in package `query`. Moving it changed where
the code sits, not what it emits.

## Performance

Moving the row-value decoders here put a forwarding wrapper in front of four
functions the query read paths call constantly — `StringVal` from about 325 root
files. The question that raises is whether the extra call frame costs anything
on a hot row-decode loop.

It does not: the compiler removes it entirely.

No-Regression Evidence: `cd go && go build -gcflags='-m' ./internal/query/`
reports `can inline StringVal`, `can inline BoolVal`, `can inline IntVal` and
`can inline StringSliceVal` for the four root wrappers; `inlining call to
querycontract.BoolVal`, `... IntVal` and `... StringSliceVal` where each wrapper
calls into this package; and `inlining call to StringVal` at each caller
(`neo4j.go:307,309,311,313,317` among others). Both hops collapse at compile
time, so a decode site emits the same code it did before the move. No benchmark
is cited because there is no runtime delta to measure — the indirection does not
survive compilation.

Observability marker for the same change:

No-Observability-Change: the handler span's name, its three attributes, and the
tracer name are unchanged from when this code lived in package `query`. The
existing `eshu_dp_api_request_duration_seconds` parent and `query.*` span still
cover the path.

## Gotchas / invariants

Capability registration is ordered and rejects duplicate initialization in the
contract tests. The low-level compatibility setter remains last-write-wins for
existing root-package tests. Unknown capabilities still panic when building a
truth envelope, and an unknown required profile still defaults to
`local_full_stack`. Once root declares the canonical capability order, an
incomplete, duplicated, or unknown entry fails closed instead of returning a
partial inventory.

`K8sSelectCandidate` carries selector presence separately from selector value.
Family code must preserve absent, present-empty, and present-nonempty states
when converting it into matcher input.

No-Regression Evidence: `go test ./internal/query/... -count=1` passed after the
final boundary edit. During the scratch proof, the complete query-playbook
family and its four test files moved under `internal/query/playbook`; its route,
catalog-order, resolver, recursive root-query tests passed, followed by
`go build ./...` and `go vet ./...`, both at exit 0. The family move was then
reverted, leaving only this contract boundary. Mutation runs proved the unknown-
capability panic test fails if the root truth wrapper stops delegating and the
selector tri-state test fails if candidate presence is dropped.

## Verification

From `go/`, run `go test ./internal/query/... -count=1`, `go build ./...`, and
`go vet ./...`. From the repository root, run
`scripts/verify-package-docs.sh` and the B-7 golden-corpus proof selected by
the parent package instructions.

## Related docs

- [Source layout](../../../../docs/public/reference/source-layout.md)
- [HTTP API](../../../../docs/public/reference/http-api.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
