# Evidence: parent-deadline expiry is not a retryable reader condition (#10658)

`hostedReadinessSnapshotBudgetExpired` (go/internal/query/status_hosted_readiness.go)
gated the retryable 503 on `!tracing.ClientCanceled(ctx, err)`. A parent
request-context deadline surfaces as a bare `context.DeadlineExceeded` with no
fence sentinel, so it sailed through and was misreported as a transient reader
condition with `Retry-After`, sending operators to the wrong trail. The
predicate now additionally requires a live request (`ctx.Err() == nil`); an
expired parent falls through to the shared 500 verdict. The retryable branch
was also moved onto `querycontract.ReaderRetryableEnvelope` with byte-identical
wire output. No hot path, backend, query shape, or schema changed.

## Proof

No-Regression Evidence: deterministic `httptest` shape comparison on the same
two inputs, no backend involved. Input A (expired parent ctx + bare
`DeadlineExceeded`): baseline 503 with `Retry-After` (wrong), after this change
500 `internal_error` with no `Retry-After` (pinned by
`TestHostedReadinessParentDeadlineIsNotReaderRetryable`). Input B (live ctx +
bounded snapshot budget timeout): 503 with `Retry-After` before and after
(pinned by the pre-existing `TestHostedReadinessSnapshotTimeoutAnswersRetryable503`,
envelope and plain variants). Full `go/internal/query` and
`go/internal/query/querycontract` suites pass on the follow-up head, `go vet`
clean. The change is failure-branch-only; the predicate is pure and the one
extra envelope allocation happens on an already-failing request.

No-Observability-Change: no new metrics, spans, or status fields. The
parent-deadline case now travels the pre-existing shared-failure path, so it
gains the request-id ERROR log line and span the 503 branch skipped — that is
the documented repair of the wrong-trail complaint, not new telemetry surface.
