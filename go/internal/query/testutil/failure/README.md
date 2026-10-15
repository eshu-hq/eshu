# testutil/failure

## Purpose

One copy of the failure-case harness that each #7674 route conversion used to
carry inline. A route conversion moves a read's 500 onto
`tracing.WriteServerFailure` behind `querycontract.WriteGraphReadError`; its
regression proves three things per failure site: the backend error text never
reaches the body, a stale reader answers a retryable 503, and a client cancel
answers 499 without reading as a server fault on the span. Eleven handler
families carried a ~200-line copy of that harness before this package.

## Ownership boundary

Same as the parent `testutil`: test-only, no production behavior, helpers in
ordinary `.go` files. The route's fixed message stays a constant in the
route's own package; the handler tracer var stays in the route's package and
is passed here by pointer.

## Exported surface

- `Suite`, `Route`, `Mounter` — what a test declares: the routes, the request
  that reaches each failure site, the fixed message, the handler constructor,
  and the package handler tracer (`*trace.Tracer`, nil when the route opens no
  span of its own).
- `Run(t, suite)` — drives every route through every case and reports each
  wrong answer as a test error named `route / case`. It opens no subtests:
  a `t.Run` call in this non-test file would count as an unregistered query
  callsite under `internal/queryplan`'s inventory (parent invariant 4).
- `Check(suite, route, case)` — the same assertion returning an error.
- `Cases`, `Case`, `Kind` (`Fault`, `ClientCancel`, `InnerCancel`,
  `ReaderFence`) and `Canary` — the failure table.
- `Serve` and `Message` — one request on a recording tracer, and the error
  text of a `detail` or envelope body, for single-request checks.

```go
func TestRoutesAnswerFixedServerFailures(t *testing.T) {
	failure.Run(t, failure.Suite{
		HandlerTracer: &cicdHandlerTracer,
		Routes: []failure.Route{{
			Name: "run correlations count", Path: "/api/v0/ci-cd/run-correlations/count?scope_id=s",
			Message: runCorrelationsCountFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Aggregates: failingStore{err: err}} },
		}},
	})
}
```

## What Check asserts

For every case the body carries no `Canary` token (credential, host, relation,
Cypher). Then by kind:

| Kind | Status | Body | Span |
| --- | --- | --- | --- |
| `Fault` | 500 | fixed message | Error, description = message, 1 exception, no cancel event |
| `ClientCancel` | 499 | fixed message | Unset, 0 exceptions, 1 cancel event with no attributes |
| `InnerCancel` | 500 | fixed message | same as `Fault` |
| `ReaderFence` | 503 | `backend_unavailable` envelope + `Retry-After` | not checked |

With a `HandlerTracer`, the checked span is the one span opened directly under
the test's `request` span; without one it is the `request` span itself, which
stands in for the otelhttp server span.

## Gotchas / invariants

- `Run` and `Check` swap the package tracer var for the request and restore
  it before returning. A test using a `HandlerTracer` must not be parallel.
- `InnerCancel` is the case that keeps the `tracing.ClientCanceled`
  conjunction honest: a route that answers 499 for any `context.Canceled`
  passes the client-cancel case and fails this one.
- A route whose handler fake checks the request context before returning the
  injected error never reaches the failure step on `ClientCancel`. Make the
  fake return the injected error unconditionally.

## Proof

`check_test.go` builds a converted sample route (GREEN, in both span modes)
and eight sample routes each wrong in one way: leaking the error text, a fence
answered 500, a cancel answered 500 (twice: with and without a clean span), a
cancel recording an exception, an inner cancel answered 499, a wrong message,
and no handler span (RED). Each assertion was mutated off in turn and the
matching wrong route then passed, which is the evidence each one is
load-bearing (#7674 slice 5 PR body).

No-Observability-Change: test-only package; it emits nothing at runtime.

## Related docs

- `../README.md` and `../AGENTS.md` — the `testutil` contract.
- `../../tracing/README.md` — `WriteServerFailure` and the cancel rule.
