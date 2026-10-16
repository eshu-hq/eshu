# 7674 slice 9: status, evidence, and investigation packet failures

Slice 9 of #7674 moves the failure answers of the root status read routes,
the evidence routes, and the investigation packet routes onto fixed
messages. A stale or timed-out PostgreSQL reader now answers the retryable
`503`, and a client cancel answers `499`. The routes with no
capability-matrix key pass `querycontract.NoCapability` from an exactly
allowlisted file until #8227 registers their keys. The route list and
messages are in `docs/public/reference/http-api/status-evidence-failures.md`.

## No-Regression Evidence

No-Regression Evidence: baseline `a6bca3b92` (origin/main, the branch's
merge base after the rebase) versus this branch. No SQL, Cypher, transaction, lock, queue, lease, or
batching path changed. The backend is PostgreSQL through the guarded reader
for the status snapshot, live activity, governance audit, content,
relationship evidence, admission decision, and drift finding reads, and the
graph for the scoped index count. Input shape, statement text, statement
count, and row counts are the same as the baseline: only the error branches
of the handlers changed. The queryplan source-coverage digests for
`(*StatusHandler).getIndexStatus` and `(*StatusHandler).getScopedIndexStatus`
moved because their error branches changed; their single
`MATCH (r:Repository)` count statement and call count did not
(`TestHotCypherManifestCoversEveryProductionQueryCall` passes). The added
work on a failed read is one `WriteGraphReadError` call, a switch over
sentinel checks that returns false for a non-fence error, then
`tracing.WriteServerFailure`. The success path is unchanged. Measured with
stub stores, no live backend: `go test ./internal/query/... -count=1` passes
78 packages (139 top-level tests skip without a live backend or an opt-in
environment variable). The four route tests (25 routes, each driven through
the canary, reader fence, client cancel, and inner cancel cases, plus a fence
capability check and the scoped graph cases) failed 131 checks on the
baseline handlers and pass on this branch.

## Observability Evidence

Observability Evidence: a failed step now marks the request span (or the
handler span on the routes that open one) through
`tracing.WriteServerFailure` or `tracing.ServerFailureEnvelope`: Error status
with the fixed message and one `exception` event for a server fault, or
only the `eshu.request.client_canceled` event and Unset status for a client
cancel. The shared harness in `go/internal/query/testutil/failure` asserts
both on every converted route. No instrument was added, removed, or renamed;
the routes keep `eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`, and a canceled read moves from the
`5xx` to the `4xx` status class on that histogram, as on the earlier #7674
slices.
