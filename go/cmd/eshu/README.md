# eshu

## Purpose

`eshu` is the unified Eshu CLI and service launcher. The same binary drives
local indexing workflows, launches the API and MCP runtimes, owns the
embedded local graph lifecycle, manages graph backend installs, runs
operator/admin workflows, and hosts the `doctor` diagnostic.

## Ownership boundary

This binary owns the Cobra command tree, flag parsing, and local Eshu service
orchestration. It does not own service runtime internals:
`eshu api start` and `eshu mcp start` exec `eshu-api` and `eshu-mcp-server`.
`eshu graph start` owns the local-authoritative supervisor and discovers
`eshu-reducer` and `eshu-ingester` via `PATH`.

## Entry points

- `main` in `go/cmd/eshu/main.go` (delegates to `rootCmd.Execute`)
- root command in `go/cmd/eshu/root.go`
- subcommand groups:
  - service launch: `mcp`, `api`, `serve` plus aliases (`service.go`);
    `version`, `help`, `doctor` (`root.go`, `doctor.go`)
  - indexing: `index`, `list`, `stats`, `delete`, `clean`, `query`,
    `watch`, `unwatch`, `watching`, `add-package`, `finalize` plus
    `i`/`ls`/`rm`/`w` aliases (`basic.go`)
  - `graph`, `install` with `nornicdb`, `status`, `start`, `stop`,
    `logs`, `upgrade` (`graph.go`, `graph_install.go`,
    `local_graph.go`)
  - `admin`: `facts`, `reindex`, `tuning-report`, `list`, `decisions`,
    `replay`, `dead-letter`, `skip`, `backfill`, `replay-events`
  - `config`, `neo4j`, `find`, `analyze`, `ecosystem`, `workspace`,
    `local-host`

## Configuration

Persistent flags in `root.go`: `--database` sets `ESHU_RUNTIME_DB_TYPE`
for the process; `-V`, `--visual` toggles interactive graph visualization.
Root flags `--version` and `-v`, plus the `eshu version` command, print the
build-time application version from `internal/buildinfo`. Subcommands define
their own flags. Service launch reads the runtime env contract (`ESHU_API_ADDR`,
`ESHU_MCP_TRANSPORT`,
`ESHU_MCP_ADDR`, `ESHU_POSTGRES_DSN`, `ESHU_GRAPH_BACKEND`, `NEO4J_*`).

## Telemetry

The Cobra dispatcher does no OTEL bootstrap. Telemetry runs inside each
launched runtime via the shared `telemetry` package. Errors print to
`os.Stderr`; the binary exits 1 on any Cobra error.

## Gotchas / invariants

- `SilenceUsage` and `SilenceErrors` are set on the root command
- `eshu graph start` requires `eshu-reducer` and `eshu-ingester` on `PATH`;
  fresh local Eshu service runs need `go/bin` on `PATH` after rebuilding
- `graphBoltHealthy` sends the Bolt magic + four version proposals and reads
  the 4-byte server response. The response must match one offered protocol
  version; `00 00 00 00` means the server rejected negotiation and is not ready.
  A TCP-only dial is insufficient because embedded NornicDB accepts connections
  before the Bolt protocol handler is fully ready, causing a handshake EOF on
  the first schema bootstrap attempt.
- `eshu graph stop` sends `SIGTERM` to the owner supervisor for both
  `local_lightweight` and `local_authoritative` profiles only after ownership
  checks pass. Lightweight stop requires the recorded Postgres socket to be
  healthy before signaling the owner PID; stale records with a reused PID are
  cleaned up without sending a signal. Authoritative stop additionally waits for
  the graph sidecar (NornicDB) to become unreachable.
- The default local graph path is embedded NornicDB when `eshu` is built with
  `nolocalllm`; `ESHU_NORNICDB_RUNTIME=process` is the only runtime-mode
  override, while `ESHU_NORNICDB_BINARY` selects process mode for a specific
  backend binary
- Embedded and process NornicDB both use the per-workspace credentials written
  under the local graph data directory; child services receive the same values
  through `ESHU_NEO4J_USERNAME`, `ESHU_NEO4J_PASSWORD`, `NEO4J_USERNAME`, and
  `NEO4J_PASSWORD`
- Embedded NornicDB must wire Bolt through the HTTP server's role, database
  access, and resolved-access callbacks. Without that shared RBAC path,
  authenticated child services can connect but projector writes to the default
  `nornic` database fail with a Neo4j security-forbidden error.
- `--database` mutates the process environment via `os.Setenv`

## Related docs

- [Service runtimes](../../../docs/docs/deployment/service-runtimes.md)
- [CLI reference](../../../docs/docs/reference/cli-reference.md)
- [CLI indexing](../../../docs/docs/reference/cli-indexing.md)
