# #11462: loadCommitGrants fail-closed note has no hot-path impact

Issue #11462 (arbiter-ruled D3 from #11454) changes one startup-only
function: `loadCommitGrants` in `go/cmd/core-api/commit_listener.go`
now rejects JSON objects without the `grants` key instead of decoding
them to zero grants. The function runs once at listener startup, before
any socket serves; it touches no query path, no Cypher, no queue, lease,
claim, batching, or concurrency knob. The decoded grants feed the same
`ClaimPlaneFence` construction as before for every accepted shape.

No-Regression Evidence: `go test -count=1 ./cmd/core-api/...`
package time 0.577s (fix commit, lane run) vs 0.578s (rebased head
ab4d7e1f98, coordinator rerun, same host class) — no delta beyond
noise; scoped `go test -race ./cmd/core-api/` green in the pre-push
floor; `go vet` and `gofumpt` clean. Existing bare-array and
`{"grants":[...]}` fixtures and tests pass unchanged, so accepted-shape
behavior is byte-identical. Backend/version: none involved (pure
in-memory JSON decode of a local config file; no Postgres/Neo4j).

No-Observability-Change: no new metric, span, log line, or status
surface. The only new operator signal is the startup error text naming
`ESHU_COMMIT_GRANTS_PATH`, which replaces a downstream grant-mismatch
error for the same misconfiguration.
