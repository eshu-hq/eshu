# MCP satellite re-pin to v0.2.5 (#9731)

Advances the Dockerfile `mcp-satellite` stage and the shared `ESHU_MCP_REF`
default (registry, installer, gate helper, reference doc) from v0.2.2 to the
published v0.2.5 satellite build. String constants only: no control flow, no
query, no queue, lease, schema, or graph-write change.

No-Regression Evidence: the edited Go line is one registry default string,
read at startup like every other `ESHU_*` default; no request, parse, emit,
or projection path branches on its value. Proven by
`go test ./internal/envregistry/ -count=1` green,
`scripts/verify-env-registry-doc.sh` green (the reference row derives from
the same default), and the pinned digest verified identical on tags 0.2.5,
0.2, and latest with `gh attestation verify` exit 0. Full CI on the PR head
re-runs the suite, including the E2E legs that exercise the new satellite.

No-Observability-Change: no metric, span, log line, status endpoint, or
alert threshold is added, removed, or renamed; operators keep the existing
installer, gate, and E2E signals.
