# Query handler spans

## Purpose

Starts the per-route tracing span for query HTTP reads and tags it with the
attributes an operator triages on, and owns the shared answer a query route
gives when a server-side read fails. No routing, no handler logic, no storage.

This package was `go/internal/query/queryspan` until #6818 moved it here and
dropped the `query` prefix from the package name.

## Ownership boundary

This package owns the handler span's name, its attributes, the tracer name,
and how a failed read is reported on that span. It does not own routes,
handlers, graph reads, or any storage adapter. Those stay in the root query
package or in a handler-family package. Each route still owns its own fixed
failure message and decides the call order around the helpers.

## Exported surface

`HandlerTracer`, `StartHandlerSpanWith`, `WriteServerFailure`,
`ServerFailureEnvelope`, and `ClientCanceledEvent`, described in
[doc.go](doc.go).

## Dependencies

`go.opentelemetry.io/otel` (plus `otel/attribute`, `otel/codes`, and
`otel/trace`), `go/internal/telemetry` for the shared service-namespace
attribute, and `go/internal/query/querycontract` for the error writer, the
error envelope, and `StatusClientClosedRequest`. It deliberately does not live
in `querycontract`, which is documented as free of OpenTelemetry: one file
importing it there would make every family that imports the contract package
inherit OpenTelemetry whether it starts a span or not.

## Telemetry

`StartHandlerSpanWith` starts the span carrying `http.route`,
`eshu.capability`, and `service.namespace`. The tracer name is
`eshu/go/internal/query`, deliberately unchanged from when this code lived in
that directory, because that name is what saved span queries and dashboards
match on. The row in
`docs/public/observability/telemetry-coverage.md` points at `handler.go`.

`WriteServerFailure` and `ServerFailureEnvelope` mark the span already in the
request context; they start no span. A server fault adds the standard
`exception` event and sets the span status to Error with the route's fixed
message as its description. A client cancel (499) leaves the status unset and
adds the `eshu.request.client_canceled` event with no attributes. The otelhttp
server span then also stays unset for 499, and its server request metric
carries `http.response.status_code=499`, so a dashboard can split client walk-aways from server faults. Neither helper
logs.

No-Observability-Change: the span name, its three attributes, and the tracer
name are identical to what package `query` emitted before this move. Moving the
code changed where it sits, not what it emits.

## Gotchas / invariants

The tracer is a parameter, not a package lookup, and that is load-bearing rather
than stylistic. Package `query` keeps its own swappable `queryHandlerTracer` var
that six span tests replace with a recording provider. Reading a package-level
tracer inside `StartHandlerSpanWith` would ignore that swap: handlers would emit
to the real tracer while the recorder saw none. That failure compiles cleanly and
reports only as `ended spans = 0, want 1`, which is why it is written down here.

`HandlerTracer` is a function, not an exported var, so no importer can reassign
the shared tracer. A caller that wants a swappable seam seeds its own
package-local var from it.

A client cancel needs both halves of the test: the error wraps
`context.Canceled` and the request context is canceled. A `context.Canceled`
from an inner context while the request is still live is a server fault and
answers 500 with a span error.

No-Regression Evidence: the seam is proven live by mutation, not by the tests
merely passing. Rewriting `StartHandlerSpanWith` to call `HandlerTracer()`
instead of its `tracer` argument still builds at exit 0 and makes
`TestHandleLanguageQueryEmitsLanguageQuerySpan` fail with
`ended spans = 0, want 1 (handleLanguageQuery must emit exactly one span)`.
Restoring the argument returns the suite to exit 0. 17 span cases run.

## Related docs

- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
- [Traces reference](../../../../docs/public/reference/telemetry/traces.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
