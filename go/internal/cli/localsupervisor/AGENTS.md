# AGENTS.md — go/internal/cli/localsupervisor guidance for LLM assistants

## Read first

1. `go/internal/cli/localsupervisor/README.md` — purpose, ownership boundary,
   exported surface, invariants.
2. `go/internal/cli/localsupervisor/doc.go` — the godoc contract and the full
   environment-variable inventory (what this package reads, what it writes).
3. `go/cmd/eshu/local_host.go` — the hidden `local-host watch` /
   `local-host mcp-stdio` cobra wrapper. Shows how the two halves fit together.
4. `go/cmd/eshu/graph.go` — the `eshu graph` and `eshu install` wrapper.
5. `go/cmd/eshu/vuln_scan_local.go` — the largest external consumer, and the
   reason roughly twenty names here are exported.
6. `docs/public/deployment/service-runtimes.md` — what each supervised service
   is for.

## Invariants this package enforces

- **One owner per workspace, and the lock is the proof.** The owner lock is
  held for the whole of `RunOwnedHostWithLayout`. Never decide a workspace is
  free from the owner record alone: check `ProcessAlive` and `SocketHealthy`,
  and reclaim a stale record under the lock. A recorded pid may have been
  reused by an unrelated process — signalling it is a bug, not a cleanup.

- **Every started resource is unwound in reverse.** Teardown is `defer`red next
  to the code that starts each resource, so a failure part-way through startup
  leaves nothing behind. If you add a resource, add its `defer` at the start
  site. Do not collect teardown at the end of the function.

- **No process wiring for NEW operator messages.** No cobra flags, no
  `fmt.Print*`, no `os.Exit`, no NEW write to `os.Stdout`/`os.Stderr`, and no
  NEW operator message through `slog` or the standard `log` package. Take an
  `io.Writer` and write to that. The `slog` and `log` half of that rule is not
  hypothetical: both are already imported here, both already bypass `out`, and
  neither is visible to `host_writer_test.go`, so a new `slog.Info(...)` would
  otherwise break no rule and be caught by no test. `go/cmd/eshu` is
  `package main`, so nothing can import it — anything that reads a flag or maps
  to an exit code stays there.

  Writes that already reach a process stream are below, deliberately without a
  count: the set moves with the build tag and with the next call site anyone
  adds, and nothing enforces a number written here. Re-derive it before you rely
  on it, from inside the package directory:

  ```bash
  rg -n 'os\.(Stdout|Stderr)|slog\.|fmt\.Print|\blog\.' --glob '!*_test.go' .
  ```

  The trailing `.` matters. Without a path, `rg` reads stdin and an agent
  running the command hangs instead of getting output.

  Some of them carry no message of this package's own: terminal log mode assigns
  `os.Stdout`/`os.Stderr` to a child `exec.Cmd`; the embedded graph runtime
  pipes the process-global streams into the graph log while NornicDB starts up;
  and that same runtime points the standard `log` package at the graph log
  (`redirectEmbeddedNornicDBStandardLogger`) and holds it there until the
  backend stops, so unlike the stream swap it covers the whole run. The last two
  are compiled only under `-tags nolocalllm`; a plain build gets the stub, which
  redirects nothing.

  The rest is this package's own operator output, and it predates the move out
  of `cmd/eshu`. `localHostProgressWriter` (`progress.go`) is `os.Stderr`, and
  the whole local progress display goes through it — the plain renderer and the
  Bubble Tea TUI both. `children.go` logs a `slog` record when a child exits
  cleanly, and `graph_bootstrap.go` passes `slog.Default()` to
  `graph.EnsureSchemaWithBackend`, which logs a record per schema statement.
  `eshu` installs no `slog` handler, and Go's default one writes through the
  standard `log` package, so those two follow that package's writer: `os.Stderr`
  on a plain build or under `ESHU_NORNICDB_RUNTIME=process`, and
  `<logs>/graph-nornicdb.log` under `nolocalllm` with the embedded runtime up.
  A caller's `io.Writer` redirects none of it. Threading `out` into
  `startLocalHostProgressReporter` would change what `eshu watch` and
  `eshu graph start` print live, so it needs its own before/after proof rather
  than a drive-by edit.

- **The injected writer has exactly one test that reads it back.**
  `TestRunOwnedHostWithLayoutWritesOperatorLinesToInjectedWriter` in
  `host_writer_test.go`. Every other test passes `io.Discard`, and the CLI
  passes `os.Stderr` — so a call site left as `fmt.Fprintf(os.Stderr, ...)`
  produces byte-identical CLI output and no other test can see it. Add a
  `fmt.Fprint*` call to `RunOwnedHostWithLayout` and you add a line to that
  test's `wantLines`, or the new call site is unproven.

- **Worker-count defaults never overwrite an operator's value.** The CPU-aware
  defaults apply only to `local_authoritative` + NornicDB, and only through
  `setOverrideIfUnset`.

## Common changes

- **Adding a supervised child service:** start it with `StartChildProcess`,
  `defer StopChildProcess`, append it to `children`, and decide whether a clean
  exit should keep the owner alive (`WaitOwnerChildren`) or end the run
  (`WaitManagedChildren`). Add its environment through `ChildOverrides` so it
  gets log wiring.
- **Adding an environment variable:** update the inventory in `doc.go` and the
  README in the same change. A variable this package writes into a child
  environment is part of the child's contract, not an implementation detail.
- **Changing the graph runtime:** touch both `graph_embedded_nornicdb.go`
  (`//go:build nolocalllm`) and `graph_embedded_stub.go` (`//go:build
  !nolocalllm`). They declare the same two names under mutually exclusive tags.

## Failure modes seen here before

- **A text search says the two embedded-graph files reference each other.**
  They do not; only one is compiled per build. Always build BOTH
  configurations: `go build ./...` and `go build -tags nolocalllm ./...`.
- **`basic.go` launches this package through a string.** `eshu watch` re-execs
  `"local-host", "watch"` as a `syscall.Exec` argument. No symbol sweep can see
  that edge, so a change to the `local-host` argv contract must be checked
  against `go/cmd/eshu/basic.go` by hand.
- **A green unit test is not proof the owner runs.** The supervisor's real
  failure modes are ordering and teardown. Prove a lifecycle change against a
  real owner run, not only against stubbed seams.

## Do not change without review

- The owner-record schema or the lock protocol (shared with
  `internal/eshulocal` and with `eshu graph status`/`stop`).
- The `local-host` argv contract.
- The `ESHU_MCP_ALLOW_UNAUTHENTICATED` loopback default in `mcpenv.go`. It is
  a security gate (#5168): the escape hatch is deliberately withheld from a
  non-loopback bind so a Helm pod fails closed.
- The reset-before-start behaviour of `local_authoritative`. It deletes the
  workspace's Postgres data, graph data, and repo cache on every start.
