# Local Supervisor

## Purpose

`localsupervisor` is the local Eshu service. It takes ownership of a workspace,
starts embedded Postgres and (in `local_authoritative`) a NornicDB graph
backend, supervises the ingester, reducer, and MCP server as child processes,
and tears all of it back down on exit. It also owns the read side an operator
sees through `eshu graph status`, `eshu graph logs`, `eshu graph stop`, and
`eshu graph upgrade`.

Three CLI paths reach it: `eshu graph start` and `eshu watch` re-exec into
`eshu local-host watch`, and `eshu mcp start` (stdio) re-execs into
`eshu local-host mcp-stdio`. `eshu vuln scan` calls in directly to attach to or
start an owner.

## Ownership boundary

This package owns supervision: the owner lock and record, process lifecycle,
child environments, graph backend startup and shutdown, and the two
post-drain finalizers. It does not own cobra wiring — flags, printing, and the
exit-code contract stay in `go/cmd/eshu` (`graph.go`, `local_host.go`,
`service.go`), because `go/cmd/eshu` is `package main` and nothing can import
it.

It writes to no process stream of its own. Callers pass an `io.Writer` for
progress output, so a test can capture what an operator would see. The one
exception is deliberate: in terminal log mode a child process inherits
`os.Stdout`/`os.Stderr` directly, because the point of that mode is for the
child's own output to reach the operator's terminal.

Workspace layout, the owner lock, the owner record, and embedded Postgres
belong to `internal/eshulocal`. Installing and verifying a NornicDB binary
belongs to `internal/cli/graphinstall`, which this package calls through
`ReadGraphVersion`.

## Exported surface

See `doc.go` for the godoc contract. The surface splits three ways:

- **Owner lifecycle** — `RunOwnedHost`, `RunOwnedHostWithLayout`,
  `RunAttachedMCPStdio`, `HostMode`/`ModeWatch`/`ModeMCPStdio`, `BuildLayout`.
- **Graph subcommand logic** — `LayoutForWorkspaceRoot`, `StatusForLayout`,
  `StatusOutput`, `LogsForLayout`, `StopForLayout`, `UpgradeForLayout`,
  `ValidateProgressMode`, `ValidateLogMode`, `ReadGraphVersion`,
  `ResolveGraphBinary`.
- **Attach-and-supervise helpers other commands share** — `RuntimeConfig`,
  `RuntimeConfigFromOwnerRecord`, `ManagedGraph`, `ManagedGraphFromRecord`,
  `StartManagedNornicDB`, `StopManagedGraph`, `ChildEnv`, `ChildOverrides`,
  `Child`, `StartChildProcess`, `StopChildProcess`, `WaitManagedChildren`,
  `WaitOwnerChildren`, `MCPHTTPEnvFromOwner`,
  `MCPHTTPAllowUnauthenticatedOverride`, and the health/record seams
  `ReadOwnerRecord`, `ProcessAlive`, `SocketHealthy`, `GraphHealthy`.

The surface is wide because `eshu vuln scan` reimplements attach-or-start
rather than calling one entry point. Giving that command a single
`AttachOrStart(layout)` would let roughly twenty of these names go back to
unexported; that is a vuln-scan refactor, not part of this package.

The parent-environment seam is not here: this package reads it through
`procexec.Environ` and layers child environments with
`procexec.MergeEnvironment`. `eshu watch`, `eshu scan`, and `eshu vuln scan`
build child environments that must agree with the ones built here, so both
sides share the one seam in `go/internal/cli/procexec` — two separate seams
would let a test stub one and silently miss the other.

## Dependencies

`internal/eshulocal` (workspace layout, owner lock and record, embedded
Postgres), `internal/cli/graphinstall` (binary install and the managed-binary
lookup), `internal/query` (profile and graph-backend parsing),
`internal/graph` and `internal/graphschemacompat` (graph schema bootstrap and
its applied marker), `internal/storage/postgres` (bootstrap definitions,
content-search indexes, IaC reachability materialization), `internal/status`
(the progress report), `internal/buildinfo`, and `internal/cpubudget`.

Under the `nolocalllm` build tag it also imports
`github.com/orneryd/nornicdb/pkg/{auth,bolt,buildinfo,config,cypher,server,storage}`
for the in-process graph runtime.

## Telemetry

This package emits no `eshu_dp_*` metrics and opens no spans. Its operator
signals are the progress lines written to the caller's `io.Writer`, the child
service logs under the workspace log directory, the graph backend log at
`<logs>/graph-nornicdb.log`, and the owner record that `eshu graph status`
reads back. The services it supervises carry their own instrumentation.

## Gotchas / invariants

- **One owner per workspace.** The owner lock is held for the whole run.
  Attach paths check `ProcessAlive` and `SocketHealthy` before trusting a
  record, and a stale record is reclaimed under the lock — never by signalling
  a pid, which may have been reused.
- **Teardown is by `defer`, in reverse order.** A failure part-way through
  startup unwinds everything already started. Adding a resource means adding
  its `defer` next to the code that started it, not at the end.
- **`local_authoritative` resets state before Postgres starts.** The Postgres
  data directory, graph data directory, and repo cache are removed on every
  authoritative start; that profile rebuilds from the workspace source tree.
- **The embedded graph runtime is build-tagged.** `graph_embedded_nornicdb.go`
  (`nolocalllm`) and `graph_embedded_stub.go` (`!nolocalllm`) declare the same
  two names. A text search shows them referencing each other; only one is ever
  compiled. Build both configurations when changing either.
- **The embedded runtime swaps the process-global `os.Stdout`/`os.Stderr`**
  while NornicDB starts, to keep its startup chatter out of an MCP stdio
  session, and restores them after readiness. Do not run it concurrently with
  anything else writing to those streams.
- **Worker-count overrides never overwrite an operator's value.** The
  CPU-aware defaults apply only to the authoritative/NornicDB pair and only
  when the variable is unset.
- **`eshu local-host` is hidden on purpose.** It is an internal re-exec target.
  Changing its argv contract breaks `basic.go`, which launches it through a
  string argument to `syscall.Exec` that no symbol search can see.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/run-locally/local-binaries.md`
- `docs/public/reference/graph-backend-installation.md`
- `docs/public/reference/nornicdb-tuning.md`
