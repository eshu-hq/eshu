# #11453: supervisor drain + satellite image swap, no hot-path impact

Issue #11453 moves local supervisor sessions, the image API binary, and
gate defaults off the in-tree `eshu-api`. The runtime-affecting files
are process-wiring only: `localsupervisor/mcpbridge.go` (session child
`eshu-api` → `eshu-core-api` with `--addr`, plus a pre-spawn refusal on
non-Neo4j backends), `host.go` (child wiring), and an `envregistry`
installer-pin entry. No query path, Cypher, queue, lease, claim,
batching, or concurrency knob is touched; spawn remains one child per
session with the same supervision tree and teardown symmetry.

No-Regression Evidence: `go test -count=1 ./internal/cli/localsupervisor/
./internal/cli/vulnscan/ ./internal/eshulocal/` green (incl. RED-first
guard and spawn tests); `go test ./cmd/golden-corpus-gate/` green;
`docker build` exit 0 with the digest-pinned satellite stage; live
supervisor flow through `StartMCPSession` vs real pinned Neo4j+PG
containers green (core `/health` + `/readyz` 200, MCP initialize +
graph-backed tool call answered, zero children after teardown; full
pass 0.60s), and both negative legs refuse pre-spawn in ~0.05s with
nothing spawned. Backend/versions: PG `ci-postgres-bookworm` +
Neo4j `ci-neo4j-community` per `scripts/prove-core-api-live.sh`;
satellite `eshu-api@sha256:2a2eaf51` (build 45658098, rollback v0.6.0).
Failure direction is strictly safer than base (fail-closed refusal
replaces a 2-minute `/health` wait against a dead child).

No-Observability-Change: no new metric, span, log line, or status
surface. The only new operator signal is the pre-spawn refusal error
naming `ESHU_GRAPH_BACKEND=neo4j`, which replaces a timeout against a
child that could never serve.
