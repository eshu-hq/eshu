# cmd/eshu-local-api

## Purpose

`cmd/eshu-local-api` is the entry point for the `eshu-local-api` binary. It
boots OTEL telemetry, opens writer and guarded reader pools plus a
graph driver, wires all query handlers through `internal/query`, mounts the
shared runtime admin surface, wraps the combined mux with shared bearer-token,
scoped-token, and dashboard browser-session authentication, and listens for
HTTP traffic until `SIGINT` or `SIGTERM`.

The binary serves the NornicDB backend or an unset backend
(`ESHU_GRAPH_BACKEND=nornicdb` or empty) with the lightweight or
authoritative local profiles. It takes the listen address on `ESHU_API_ADDR`
and answers `/healthz`. On Neo4j the session and scan paths use
`eshu-core-api` (`go/cmd/core-api`, `-addr` plus `/health`) instead; any
other backend value is refused pre-spawn by the caller, never by this binary.

## Where this fits in the pipeline

The local supervisor spawns one session `eshu-local-api` per MCP session on
NornicDB or an unset backend, and the repository-scan one-shot used it before
the vulnscan half of `eshu-hq/eshu#11514` repointed scans at `eshu-core-api`.
Querying serves the workspace stores; the satellite `eshu-mcp-server` reads
it through `-core-base-url`.

## Lifecycle / workflow

`main` initializes OTEL via `telemetry.NewBootstrap` and
`telemetry.NewProviders`, then calls `wireAPI`. Failure at any wiring step
releases already-acquired connections and exits; the `cleanup` closure
returned by `wireAPI` closes Postgres and the graph driver on normal
shutdown.

`wireAPI` resolves `ESHU_QUERY_PROFILE`, `ESHU_GRAPH_BACKEND`, opens the
graph driver via `openQueryGraph` (skipped when
`ESHU_QUERY_PROFILE=local_lightweight`), opens and pings Postgres, then
builds the `query.APIRouter` with all handler structs wired to the concrete
`query.Neo4jReader` and `query.ContentReader` adapters.

## History

Relocated out of `go/cmd/api` under `eshu-hq/eshu#11514` (session half, owner
decision (b)-narrowed on `eshu-hq/eshu#11467`): the serving subset moved here
under its surviving binary name so the later `eshu-hq/eshu#11456` removal can
delete the leftover tree. Behavior is unchanged by the move; only the source
directory and the consumer build references changed.

## Build and test

```bash
go build ./cmd/eshu-local-api/
go test ./cmd/eshu-local-api/... -count=1
```

## Relocation evidence (#11514)

No-Regression Evidence (#11514): session-half move of the serving tree from
`go/cmd/api` to `go/cmd/eshu-local-api` (111 renames plus comment and
path-string repoints, no logic change). Baseline `origin/main`
`859e7f6001`, after the relocation commit on `drive/11514-session-half`:
`go build ./...` exit 0;
`go test ./cmd/eshu-local-api/...` ok (both packages);
`go test ./internal/cli/localsupervisor/ ./internal/envregistry/
./internal/workflow/ ./internal/cli/vulnscan/` ok;
`bash scripts/test-verify-ask-eshu-local-proof.sh` exit 0. The session-spawn
matrix (nornicdb/unset to `eshu-local-api`, neo4j to `eshu-core-api`) and the
unknown-backend pre-spawn refusal stay green throughout. Backend/version:
not applicable (no backend or query change; identical sources, new
directory). Input shape: rename-only diff plus path-string repoints; the two
diagnostic import-path edits are the only code-line changes. Safe because the
serving bytes are identical and every consumer build (Dockerfile, installer,
corpus shells, latency-gate api branch) resolves the new path.

No-Observability-Change (#11514): no metric, span, log, status, or telemetry
identity changed. The OTEL service name and the `--version` display name are
deliberately kept so dashboards and alerts are unaffected.
