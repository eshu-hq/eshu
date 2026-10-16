# 8176: capability-matrix keys for the five operator admin read routes

Issue #8176 registers one capability-matrix key per operator admin read
route (work-items query, decisions query, decision evidence read,
replay-events query, repository-reindex catalog lookup) and answers a
fenced PostgreSQL reader with the retryable 503 through
`querycontract.WriteGraphReadError` carrying that key. Non-fence errors
keep their existing 500 mapping; backend-text removal stays with the
#7674 slice and is pinned by `TestAdminReadNonFenceErrorKeepsStatus500`.

## No-Regression Evidence

No-Regression Evidence: no SQL, Cypher, queue, lease, batching, or
concurrency change. The added work on the non-fence path is one
`WriteGraphReadError` call that runs `mapGraphReadHTTPError`, a switch
over three sentinel checks (`ErrGraphUnavailable`, the reader-fence
predicate, `ErrGraphReadDeadline`), and returns false, leaving the
caller's 500 untouched. No store query, ordering, limit, or timeout
changed on any of the five reads. The fence path previously answered
500 with the reader error text, so the 503 envelope replaces an error
response, not a success. Test proof:
`TestAdminReadReaderFenceCarriesCapability` (5/5 fail pre-fix with 500,
pass with 503 plus `Retry-After` and the exact key) and the unchanged
`./internal/query/admin/` suite.

## Observability Evidence

No-Observability-Change: the fence path adds no instrument. Verified by
inspection: `querycontract/http.go`, `graphreaderr.go`, and `truth.go`
contain no OTel, span, or metric calls on the `WriteGraphReadError`
path. The 503 status itself remains visible to access logs and the
`http.response.status_code` metric through the existing writers.
