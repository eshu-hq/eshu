# 8292: delta-union completeness maps the graph-read verdicts

`GET /api/v0/admin/delta-union/completeness` answered a graph outage or a
graph deadline as a plain `500` with no `Retry-After`. The route now answers
the shared retryable `503` and the `504` verdict, and registers the
capability-matrix key `operator.delta_union_completeness.check`.

## No-Regression Evidence

No-Regression Evidence: baseline `1d90acfc7` (origin/main, the branch base)
versus this branch. Only the handler's error branch changed: on a failed read
it runs one `querycontract.GraphReadErrorEnvelope` mapping (a few `errors.Is`
checks) before the existing `500`/`499` mapping. The per-repository statement
`deltaUnionGraphFilesCypher`, the ACTIVE read (`ReadDeltaUnionActive`), their
call count (one ACTIVE read, then one anchored graph read per delta-active
repository), the input shape (explicit `?scope=` filters, capped at 500
repositories), and the row counts are the same as the baseline: `git diff
1d90acfc7 -- go/internal/query/admin/delta_union_completeness.go` changes no
Cypher line and no line on the success path. No new query, lock, queue, or
claim is added, so no concurrency path changes. Measured with stub stores, no
live backend. `TestDeltaUnionCompletenessMapsGraphReadVerdicts` and
`TestDeltaUnionCompletenessVerdictNamesCapabilityInEnvelope` failed on the
baseline handler (graph outage and graph deadline both answered `500`, the
envelope request answered `500`) and pass on this branch. The existing
completeness tests (pass, mismatch, non-delta skip, unknown scope, cap before
the ACTIVE read, repository cap, required scope, unconfigured, fixed-text
failures) pass unchanged: `cd go && go test ./internal/query/... -count=1`.

## Observability Evidence

Observability Evidence: no new metric, span, or log. The operator signal is
the existing request path. The route is served under
`eshu_dp_api_request_duration_seconds` and `eshu_dp_api_request_errors_total`,
which now see a graph outage as `503` and a graph deadline as `504` instead
of folding both into `500`, so an operator can tell a graph outage from a
route fault at 3 AM. A `500` still marks the request span through
`tracing.ServerFailureEnvelope` as before; the shared verdict writer marks no
span, like every other converted graph route. The body carries only the fixed
verdict text, never the backend error. The regression asserts the status,
`Retry-After` on the `503`, the `{"status","detail"}` body shape, and that the
backend canary text is absent.
