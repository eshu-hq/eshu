# Agent instructions: queryspan

Read `doc.go` and `README.md` before editing. This package is two functions;
almost every change here is a contract change.

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

## Common changes

Adding an attribute: update `StartHandlerSpanWith`, the Telemetry section of
`README.md`, and the row in `docs/public/observability/telemetry-coverage.md`.
Confirm the label set stays low-cardinality; a per-request value here multiplies
across every query route.

## Verification

From `go/`: `go test ./internal/query/... -count=1`. Prove the tracer seam by
mutation rather than by the suite passing: make `StartHandlerSpanWith` ignore its
argument and confirm the span tests fail, then restore.
