# Query handler spans

## Purpose

Starts the per-route tracing span for query HTTP reads and tags it with the
attributes an operator triages on. One function and one tracer accessor; no
routing, no handler logic, no storage.

## Ownership boundary

This package owns the handler span's name, its attributes, and the tracer name.
It does not own routes, handlers, graph reads, or any storage adapter. Those
stay in the root query package or in a handler-family package.

## Exported surface

`HandlerTracer` and `StartHandlerSpanWith`, described in [doc.go](doc.go).

## Dependencies

`go.opentelemetry.io/otel` (plus `otel/attribute` and `otel/trace`) and
`go/internal/telemetry` for the shared service-namespace attribute. It
deliberately does not live in `querycontract`, which is documented as
standard-library-only: one file importing OpenTelemetry there would make every
family that imports the contract package inherit OpenTelemetry whether it starts
a span or not.

## Telemetry

`StartHandlerSpanWith` starts the span carrying `http.route`,
`eshu.capability`, and `service.namespace`. The tracer name is
`eshu/go/internal/query`, deliberately unchanged from when this code lived in
that directory, because that name is what saved span queries and dashboards
match on. The row in
`docs/public/observability/telemetry-coverage.md` points at `handlerspan.go`.

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

No-Regression Evidence: the seam is proven live by mutation, not by the tests
merely passing. Rewriting `StartHandlerSpanWith` to call `HandlerTracer()`
instead of its `tracer` argument still builds at exit 0 and makes
`TestHandleLanguageQueryEmitsLanguageQuerySpan` fail with
`ended spans = 0, want 1 (handleLanguageQuery must emit exactly one span)`.
Restoring the argument returns the suite to exit 0. 17 span cases run.

## Related docs

- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
