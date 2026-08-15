# eshu CLI Gotchas — Local Runtime And Graph Lifecycle

Split out of [`README.md`](README.md) for issue #6059 so that file stays under
the repository's 500-line cap. This page carries owner startup and shutdown,
local-authoritative reset, worker sizing, the foreground progress panel,
`mcp start` attach, and embedded NornicDB behavior — everything about the
processes and graph store this binary supervises rather than the commands it
dispatches. The root-command invariants and the pointer back to these pages
stay in `README.md`.

## Invariants

- `eshu graph start` requires `eshu-reducer` and `eshu-ingester` on `PATH`;
  fresh local Eshu service runs need `go/bin` on `PATH` after rebuilding
- `eshu mcp start --workspace-root <repo>` attaches to the active local owner.
  The stdio path execs the internal `local-host mcp-stdio` attach command, while
  `--transport http` and legacy `--transport sse` exec `eshu-mcp-server` with
  the owner-derived Postgres DSN, graph backend, graph URI, and workspace
  credentials. HTTP attach fails fast if the owner record, Postgres socket, or
  graph backend health probe is not ready.
- `eshu graph start` acquires `owner.lock` through the local host startup path
  before embedded Postgres starts. If an earlier shutdown removed `owner.json`
  but left a live workspace `postmaster.pid`, startup verifies PID liveness,
  socket health, and the Postgres protocol before running `pg_ctl stop` and
  starting a fresh embedded Postgres.
- `local_authoritative` rebuilds from the workspace source tree on owner start,
  so startup clears the rebuildable Postgres `data` / `runtime` directories and
  the local NornicDB graph store before launching children. It also clears the
  filesystem selector manifest under `cache/repos` so a restarted owner cannot
  mistake an empty fresh Postgres for an unchanged source tree. The reset
  preserves managed Postgres binaries and logs while avoiding stale queue rows,
  old graph nodes, and NornicDB search-index warmup over obsolete data
  (`local_host_reset.go`).
- For `local_authoritative` + NornicDB, the local owner sets snapshot, parse,
  projector, and reducer worker env vars to the developer machine's CPU count
  before launching `eshu-ingester` and `eshu-reducer`. Explicit env vars still
  win, so a developer can lower or raise a single pool without changing the
  owner code (`internal/cli/localsupervisor/config.go` and `host.go`).
- Foreground `eshu graph start` defaults child service logs to workspace log
  files (`eshu-ingester.log`, `eshu-reducer.log`) while `--progress auto`
  renders a branded Bubble Tea progress panel on the terminal alternate screen.
  The panel leads with a verdict (`Watching`, `Indexing`, `Settling`,
  `Complete`, or `Attention`) and uses animated Ember-to-Signal-Teal bars with
  stage states and known-work denominators: collector generations and
  projector/reducer work items. An active collector generation is the current
  snapshot and counts as done in this table; pending collector generations keep
  the verdict at `Indexing` until the collector settles. `Complete` means every
  known stage has drained; if shared projection intents still need to become
  graph-visible, the verdict stays at `Settling` and the panel prints a
  `Shared projections` backlog line with outstanding and in-flight counts. The
  table pads columns by display width, so colored progress bars do not shift
  the `Done`, `Active`, `Waiting`, or `Failed` counts. It shows `idle` when the
  status store has no active denominator yet. `--progress plain` writes
  append-only text snapshots, `--verbose` and `--logs terminal` restore direct
  terminal logs for debugging, `--logs quiet` discards child logs, and
  `--progress quiet` suppresses the progress reporter.
- `graphBoltHealthy` sends the Bolt magic + four version proposals and reads
  the 4-byte server response. The response must match one offered protocol
  version; `00 00 00 00` means the server rejected negotiation and is not ready.
  A TCP-only dial is insufficient because embedded NornicDB accepts connections
  before the Bolt protocol handler is fully ready, causing a handshake EOF on
  the first schema bootstrap attempt.
- `eshu graph stop` sends `SIGTERM` to the owner supervisor for both
  `local_lightweight` and `local_authoritative` profiles only after ownership
  checks pass. Lightweight stop requires the recorded Postgres socket to be
  healthy before signaling the owner PID; otherwise it acquires `owner.lock`,
  stops any recorded embedded Postgres child, and only then removes stale
  metadata. Authoritative stop uses the same lock-before-reclaim discipline when
  the owner PID is already gone: if the graph is unhealthy, it stops any
  recorded embedded Postgres child and removes stale metadata. If the lock is
  still held, the record is preserved for the running owner or the next reclaim
  path. Authoritative stop additionally waits for the graph sidecar (NornicDB)
  to become unreachable.
- The default local graph path is embedded NornicDB when `eshu` is built with
  `nolocalllm`; `ESHU_NORNICDB_RUNTIME=process` is the only runtime-mode
  override, while `ESHU_NORNICDB_BINARY` only chooses the specific backend
  binary after process mode is selected
- Embedded NornicDB writes its effective runtime settings to
  `graph-nornicdb.log` after `nornicdb.Open` applies library defaults. The line
  includes parallel execution, worker count, memory limit, GC percent, object
  pooling, query cache, embedding, Heimdall, and Qdrant gRPC state so
  performance runs can cite the actual active settings rather than inferred
  defaults.
- Embedded and process NornicDB both use the per-workspace credentials written
  under the local graph data directory; child services receive the same values
  through `ESHU_NEO4J_USERNAME`, `ESHU_NEO4J_PASSWORD`, `NEO4J_USERNAME`, and
  `NEO4J_PASSWORD`
- Embedded NornicDB must wire Bolt through the HTTP server's role, database
  access, and resolved-access callbacks. Without that shared RBAC path,
  authenticated child services can connect but projector writes to the default
  `nornic` database fail with a Neo4j security-forbidden error.
