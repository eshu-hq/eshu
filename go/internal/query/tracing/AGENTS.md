# Agent instructions: tracing

Read `doc.go` and `README.md` before editing. This package is a handful of
functions; almost every change here is a contract change.

## Invariants

- The tracer name is `eshu/go/internal/query` and MUST NOT be renamed to follow
  the directory. It is an operator-facing identifier that saved span queries and
  dashboards match on.
- `StartHandlerSpanWith` MUST take the tracer as an argument. Reading a
  package-level tracer instead silently disconnects the swap that six span tests
  in package `query` rely on. That bug compiles cleanly; only
  `ended spans = 0, want 1` reveals it.
- `HandlerTracer` MUST stay a function, never an exported var. An exported var
  is reassignable by any importer, and two family tests swapping it under
  `t.Parallel()` race.
- The span's attribute set (`http.route`, `eshu.capability`,
  `service.namespace`) is the operator contract. Adding one requires a
  telemetry-coverage row update; changing one requires checking the dashboards.
- `WriteServerFailure` and `ServerFailureEnvelope` MUST write only the
  caller's fixed `message`, never `err.Error()`: backend errors quote SQL,
  Cypher, hosts, and credentials (#7626). The client-cancel test MUST stay a
  conjunction: `err` wraps `context.Canceled` AND the request context is
  canceled. Dropping the second half turns an inner-context cancel on a live
  request into a 499 with no span error, hiding a real server fault.
- A client cancel MUST NOT call `RecordError` or set the span status, and the
  `eshu.request.client_canceled` event MUST carry no attributes or error text.
- This package MAY import `querycontract`; `querycontract` MUST NOT import this
  package or OpenTelemetry (its own AGENTS.md dependency rule).

## Common changes

Adding an attribute: update `StartHandlerSpanWith`, the Telemetry section of
`README.md`, and the row in `docs/public/observability/telemetry-coverage.md`.
Confirm the label set stays low-cardinality; a per-request value here multiplies
across every query route.

Moving a route's 500 onto `WriteServerFailure`: keep the call order (route
503s, then `querycontract.WriteGraphReadError` with a literal capability, then
route 400 sentinels, then `WriteServerFailure`), and define the message as a
package constant in the route's own package.

## Verification

From `go/`: `go test ./internal/query/... -count=1`. Prove the tracer seam by
mutation rather than by the suite passing: make `StartHandlerSpanWith` ignore its
argument and confirm the span tests fail, then restore. For the server-failure
helpers, drop the `ctx.Err()` half of the cancel test and confirm the
"canceled error on a live request" cases fail, then restore.
