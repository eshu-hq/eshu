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

- **No process wiring in this package.** No cobra flags, no `fmt.Print*`, no
  `os.Exit`, no writes to `os.Stdout`/`os.Stderr` for operator messages. Take
  an `io.Writer` and write to that. `go/cmd/eshu` is `package main`, so nothing
  can import it — anything that reads a flag or maps to an exit code stays
  there.

  Two deliberate exceptions, both about a CHILD process's streams rather than
  this package's own output: terminal log mode assigns `os.Stdout`/`os.Stderr`
  to a child `exec.Cmd`, and the embedded graph runtime temporarily swaps the
  process-global streams while NornicDB starts up.

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
