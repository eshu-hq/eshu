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
  span of its own). `Route.PlainDetail` additionally requires the fixed message
  as a plain `{"detail": ...}` body (`PlainDetailBody`), for a family whose
  contract is that shape; without it either body shape passes.
  `Route.FaultStatus` names the status a fault answers when it is not 500
  (`CustomFaultStatus`), for an operational 503 such as a code-evidence read
  or a configuration-state sentinel. `Route.NoFence` marks a write path with
  no reader fence, so its stale-reader case is checked as a fault.
  `Route.CancelMessage` names the message a client cancel answers when it is
  not the route's message, for a multi-step route whose cancel surfaces at an
  earlier step. `Route.Stage`
  additionally checks the step's stage-log verdict (`StageLogVerdict`): one
  `stage_failed` line for a fault, one `stage_canceled` line for a client
  cancel, none for a mapped fence verdict. `Route.HandlerSpanName`, when
  non-empty, pins the telemetry name of the handler span the step opens
  (`HandlerSpanName`); empty means the name is not checked.
- `Run(t, suite)` — drives every route through every case and reports each
  wrong answer as a test error named `route / case`. It opens no subtests:
  a `t.Run` call in this non-test file would count as an unregistered query
  callsite under `internal/queryplan`'s inventory (parent invariant 4).
- `Check(suite, route, case)` — the same check returning an error that
  joins every violation.
- `Evaluate(suite, route, case)` — the violations as `[]Violation`, each
  naming the `Assertion` it broke. `Assertions()` lists every assertion
  `Check` makes (22, one exported constant each).
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

Each row below is one or more `Assertion` constants, evaluated independently,
so a route wrong in two ways reports both. For every case the body carries no
`Canary` token (credential, host, relation, Cypher). Then by kind:

| Kind | Status | Body | Span |
| --- | --- | --- | --- |
| `Fault` | 500 | fixed message | Error, description = message, 1 exception, no cancel event |
| `ClientCancel` | 499 | fixed message | Unset, 0 exceptions, 1 cancel event with no attributes |
| `InnerCancel` | 500 | fixed message | same as `Fault` |
| `ReaderFence` | 503 | `backend_unavailable` envelope + `Retry-After` | not checked |

With `Route.FaultStatus` set, a `Fault` and an `InnerCancel` answer that
status instead of 500 (`CustomFaultStatus`); the span and body checks are
unchanged. With `Route.NoFence`, the `ReaderFence` case is checked as a
`Fault`. With `Route.Stage`, the case's stage-log records must carry the
step verdict (`StageLogVerdict`): a fault logs one `stage_failed` record at
ERROR with the stage's operation, stage, and `error_cause` (`canceled` for an
inner cancel, `unknown` otherwise); a client cancel logs one `stage_canceled`
record at INFO with the operation, stage, a `repo_id`, and a numeric
`duration_seconds` and no error attributes; a mapped fence verdict logs
neither.

With a `HandlerTracer`, exactly one span must open directly under the test's
`request` span (`OneHandlerSpan`), and that is the span checked; without one
the `request` span itself is checked, standing in for the otelhttp server
span. The fault description is checked only when the status is Error, so an
Unset fault span reports `FaultSpanError` alone.

## Gotchas / invariants

- `Serve` swaps the package tracer var named by `Suite.HandlerTracer` for the
  request and restores it before returning; `Run`, `Check`, and `Evaluate`
  reach it through `Serve`, and a route test may call `Serve` directly (the
  incident 404 test does). Any test whose suite sets `HandlerTracer` must not
  be parallel.
- `InnerCancel` is the case that keeps the `tracing.ClientCanceled`
  conjunction honest: a route that answers 499 for any `context.Canceled`
  passes the client-cancel case and fails this one.
- A route whose handler fake checks the request context before returning the
  injected error never reaches the failure step on `ClientCancel`. Make the
  fake return the injected error unconditionally.

## Proof

`check_test.go` drives a converted sample route (GREEN, in both span modes).
`samples_test.go` holds one wrong sample route per `Assertion`, each wrong in
exactly one way. `TestEveryAssertionHasOneWrongSample` requires the sample list
and `Assertions()` to match one for one, and
`TestCheckCatchesEachWrongSampleOnItsAssertionOnly` requires each sample to
violate its own assertion and no other. `TestEveryReportedAssertionIsListed`
type-checks the package and requires every constant-valued `Assertion`
expression in check code to be a declared `Assertion` constant listed in
`Assertions()`, and every listed one to be reported somewhere. It inspects
constant values only: an `Assertion` held in a package-scope `var`, or a
conversion of a non-constant value, is outside it. Those are excluded by the
`AGENTS.md` rule that every assertion is a declared constant, which review
enforces, not this test. `TestCanaryScanChecksEveryToken` drives one route per canary token
that leaks only that token (credential, host, relation, Cypher) and requires
a `NoCanaryInBody` violation and nothing else. Weakening any assertion,
dropping a canary token, or reporting an unlisted constant `Assertion`
therefore turns the package RED. This was run as 21 `go test -overlay` mutants plus the
review's K1 (one canary token) and K2 (an unlisted assertion), all RED
(#7674 slice 5 PR body). The type-check test reads source from disk, so
mutate `check.go` on disk for it, not with `-overlay`.

## Remainder

`service`, `terraform/drift` and `workitem` (#8172 slice 1), `secrets`,
`iac` and `package/registry` (#8172 slice 2), `supply/chain`, `impact`
and `entity` (#8172 slice 3), and `codequery` and `contentread`
(#8172 slice 4) now run through `Run`, on top of the earlier retrofits. One
family still carries a custom harness this one does not yet cover:
`selector`. #8172 tracks moving it here. `codequery` keeps its
sentinel-503 test on the local serve/assert helpers, and `selector` keeps its
typed-answer test: `Run` asserts one fixed message with 500/499/503
statuses, which cannot express per-class 404/400 answers.

No-Regression Evidence (#8172 slice 2): test-only change. `Route.PlainDetail`
adds one body-shape check to `Check`, off unless a route sets it. The three
converted families change no production code, so their read paths, bounds, and
costs are unchanged. Their `go test` packages run in under 2 s each.

No-Observability-Change (#8172 slice 2): the new assertion reads the response
body only. It emits no signal and adds no span, metric, or log.

No-Regression Evidence (#8172 slice 4): test-only change. No helper change:
every converted route fits the existing `Route` fields (`Method`, `Body`,
`Message`, `Handler`) and the existing span split (a `HandlerTracer` suite
for routes that open a handler span, a tracer-less suite checking the request
span for routes that record there, as the old harnesses did by falling back
to it). The two converted families change no production code, so their read
paths, bounds, and costs are unchanged. Their `go test` packages run in under
1 s each (codequery 0.08 s, contentread 0.04 s, failure 0.14 s).

No-Observability-Change (#8172 slice 4): the conversion reads responses and
the existing handler/request spans only. It emits no signal and adds no span,
metric, or log.

No-Regression Evidence (#8172 slice 3): test-only change. `FaultStatus`,
`CancelMessage`, `NoFence`, `Stage`, and `HandlerSpanName` add five opt-in
route checks to the helper, each off unless a route sets it, each pinned by a
seeded wrong-route sample. The three converted families change no production
code, so their read paths, bounds, and costs are unchanged. Their `go test`
packages run in under 1 s each (supply/chain 0.05 s, impact 0.15 s,
entity 0.32 s, failure 0.30 s).

No-Observability-Change (#8172 slice 3): the new options assert response
status, body, handler span name, and the handler's existing stage-log lines
only. They emit no signal and add no span, metric, or log.

No-Regression Evidence: test-only package with no production importer
(`internal/queryplan`'s production-import guard rejects one). The `MATCH (n)`
in `harness.go` is canary text inside the `Canary` string, standing in for a
Cypher fragment a backend error might quote; no query runs here, and the
queryplan callsite inventory (`TestHotCypherManifestCoversEveryProductionQueryCall`)
passes with this package in the tree. The retrofitted families' production
code is unchanged, so their read paths, bounds, and costs are unchanged.

No-Observability-Change: test-only package; it emits nothing at runtime. It
asserts the existing handler-span signals (`exception` event, Error status,
`eshu.request.client_canceled`) that `tracing.WriteServerFailure` already
emits.

## Related docs

- `../README.md` and `../AGENTS.md` — the `testutil` contract.
- `../../tracing/README.md` — `WriteServerFailure` and the cancel rule.
