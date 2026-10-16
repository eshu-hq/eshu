# 7674 slice 12: guard v2 and the delta-union completeness failure

Slice 12 of #7674 replaces the error-text ceiling ratchet with guard v2 (two
test-only AST assertions over `go/internal/query`) and converts the two
routes no earlier slice covered: `GET /api/v0/cloud/inventory` and
`GET /api/v0/admin/delta-union/completeness`. This note covers the second,
whose file holds the completeness check's per-repository Cypher, and the
OpenAPI fragment for it.

## No-Regression Evidence

No-Regression Evidence: baseline `67d65fb43` (origin/main, the branch base)
versus this branch. Only the handler's error branch and the repository-cap
error type changed. The per-repository statement `deltaUnionGraphFilesCypher`,
the ACTIVE read (`ReadDeltaUnionActive`), their call count (one ACTIVE read,
then one anchored graph read per delta-active repository), the input shape
(explicit `?scope=` filters, capped at 500 repositories), and the row counts
are byte-for-byte the same as the baseline: `git diff` over the file changes
no Cypher line. The cap check still runs before the ACTIVE fan-out. On a
failed read the added work is one `tracing.ServerFailureEnvelope` call (a
span status and event) and one `errors.As` check, then the same
`querycontract.WriteJSON` write. The success path is unchanged. Measured with
stub stores, no live backend (the backend is PostgreSQL for the ACTIVE side
and the graph for the per-repository read, neither touched):
`go test ./internal/query/... ./internal/mcp/... ./internal/queryplan/...
-count=1` passes 117 packages. `TestDeltaUnionCompletenessFailuresAnswerFixedText`
failed on the baseline handler (4 of 4 fault and cancel cases leaked the
canary in `detail`) and passes on this branch; the existing completeness
tests (pass, mismatch, non-delta skip, unknown scope, cap before the ACTIVE
read, repository cap, required scope, unconfigured) pass unchanged.

## Observability Evidence

Observability Evidence: a failed ACTIVE or graph read now marks the request
span through `tracing.ServerFailureEnvelope`: Error status with the fixed
message `delta-union completeness check failed` and one `exception` event
for a server fault, or only the `eshu.request.client_canceled` event and
Unset status for a client cancel, which answers `499`. A filter past the
repository cap marks the span the same way with its narrow-the-filter text.
The regression asserts the span for every fault and cancel case. No
instrument was added, removed, or renamed; a canceled read moves from the
`5xx` to the `4xx` status class on `eshu_dp_api_request_duration_seconds`,
as on the earlier #7674 slices.
