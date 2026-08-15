# AGENTS.md — cmd/eshu guidance for LLM assistants

## Read first

1. `go/cmd/eshu/README.md` — binary purpose, subcommand groups, configuration,
   and evidence markers. Its "Gotchas / invariants" section routes to three
   sibling docs that hold the per-command contracts:
   `gotchas-onboarding-and-dogfood.md`, `gotchas-read-surface-commands.md`,
   and `gotchas-local-runtime-and-graph.md`
2. `go/cmd/eshu/root.go` — `rootCmd`, persistent flags (`--database`,
   `--visual`), and root subcommand registration
3. `go/cmd/eshu/service.go` — `runMCPStart`, `runAPIStart`, `procexec.Exec`,
   `procexec.Executable`; how the binary execs runtime processes
4. `go/cmd/eshu/basic.go` — indexing subcommands (`index`, `list`, `watch`,
   `query`, `stats`); `runIndex` delegates to `eshu-bootstrap-index` via
   `indexLookPath`
5. `go/cmd/eshu/graph.go` — the `graph` and `install` subcommand trees and
   their `RunE`s (`runGraphStatus`, `runGraphStart`, `runGraphStop`,
   `runGraphLogs`, `runGraphUpgrade`). The status, stop, logs, and upgrade
   logic itself is in `go/internal/cli/localsupervisor`.
6. `go/cmd/eshu/local_host.go` — the hidden `local-host` supervisor entry
   point. Registration and signal handling only; the supervisor is
   `go/internal/cli/localsupervisor`.
7. `go/internal/cli/procexec` — the shared re-exec seam: `procexec.Executable`,
   `procexec.Getwd`, `procexec.LookPath`, `procexec.Exec`, `procexec.Environ`,
   `procexec.CleanExecutableArg0`, `procexec.MergeEnvironment`. `eshu mcp start`
   (both the stdio and the HTTP path), `eshu graph start`, and `eshu watch` hand
   the process over through it, which is what lets their tests substitute the
   seams instead of losing the test process to a real `syscall.Exec`. Three
   re-exec paths do not, so do not assume you can stub them the same way:
   `eshu api start` and `eshu serve` call `syscall.Exec` directly in
   `service.go` with `exec.LookPath` and `os.Environ`, and `eshu index` goes
   through its own `indexLookPath` / `indexExec` pair in `basic.go`, which
   `basic_test.go` substitutes instead. Route a new re-exec through `procexec`;
   its `AGENTS.md` carries the substitution rules tests must follow
8. `go/internal/cli/` — where command logic that is not process wiring lives,
   because this directory is `package main`. `eshu report`'s digest and
   artifact logic is in `go/internal/cli/opdigest`; the local Eshu service and
   every `eshu graph` subcommand's logic is in
   `go/internal/cli/localsupervisor`. Read the target package's `AGENTS.md`
   before changing the wrapper that calls it.

## Invariants this package enforces

- **`SilenceUsage` and `SilenceErrors`** — both set to `true` on the `rootCmd`
  literal in `root.go`, so Cobra does not print usage on every error. Removing
  either breaks operator scripts that parse `stderr`.
- **`--database` mutates the process environment** — the `PersistentPreRunE` on
  that same `rootCmd` literal calls
  `os.Setenv("ESHU_RUNTIME_DB_TYPE", globalDatabase)`. This affects every child
  process exec'd in the same process.
- **Service-launch replaces the process image** — `eshu mcp start` (both paths),
  `eshu api start`, `eshu serve`, `eshu graph start`, `eshu watch`, and
  `eshu index` all reach `syscall.Exec`, so no Eshu logic runs after the exec
  point: nothing deferred, no flush, no cleanup. Anything the operator must see
  has to be written before the call. Which route they take differs and matters
  when you write a test — `procexec.Exec` for `mcp start` (`service.go`),
  `graph start` (`graph.go`), and `watch` (`basic.go`); a bare `syscall.Exec`
  for `api start` and `serve` (`service.go`); the local `indexExec` var for
  `index` (`basic.go`).
- **Removed commands use `removedCommandError`** — deprecated and removed
  commands (`delete`, `clean`, `unwatch`, `add-package`, `finalize`) call
  `removedCommandError` in `contract.go` instead of silently succeeding or
  panicking. Any new removal must follow this pattern.

## Common changes and how to scope them

- **Add a new `admin` subcommand** → add a `cobra.Command` in `admin.go`,
  wire it to `adminCmd` or `adminFactsCmd`, call `apiClientFromCmd` for
  authenticated requests. Why: `admin.go` owns the full admin subcommand tree;
  scattering admin commands into other files makes auditing harder.

- **Add a new `graph` subcommand** → add a `cobra.Command` to `graph.go`'s
  `init()` and add its `run*` func in the same file, but put the behaviour it
  invokes in `go/internal/cli/localsupervisor`. Why: the `graph` subcommand
  tree is fully wired in `graph.go` and the `graphCmd` var is defined there,
  while everything that is not flag reading, printing, or the exit-code
  contract belongs in a package that can be imported and tested.

- **Add a new persistent flag** → add it in `root.go` and thread it through
  `PersistentPreRunE` if it affects child-process env. Why: persistent flags
  apply to all subcommands; adding them only in a leaf file makes them
  invisible to sibling commands.

- **Add a new local-host subcommand** → add a `cobra.Command` inside the
  `init()` in `local_host.go`; keep the command `Hidden: true`, and put the
  supervision behaviour in `go/internal/cli/localsupervisor`. Why:
  `local-host` is the internal supervisor entry point, not a public user
  command, and `eshu watch` reaches it through a `syscall.Exec` string
  argument (`basic.go`) that no symbol search can see.

## Failure modes and how to debug

- Symptom: `eshu mcp start` prints `eshu-mcp-server binary not found in PATH`
  → cause: `exec.LookPath("eshu-mcp-server")` failed; rebuild with
  `cd go && go build -o bin/ ./cmd/mcp-server/` and add `go/bin` to `PATH`.

- Symptom: `eshu index` prints `eshu-bootstrap-index binary not found in PATH`
  → cause: `indexLookPath("eshu-bootstrap-index")` failed; rebuild
  `./cmd/bootstrap-index/` and ensure `go/bin` is on `PATH`.

- Symptom: `eshu graph start` starts a process but the graph does not come up
  → cause: `eshu-reducer` or `eshu-ingester` are not on `PATH`; the
  `local-host watch` supervisor discovers them through `PATH`. Rebuild all
  binaries and check `PATH` before running.

- Symptom: `eshu graph start` appears noisy in local foreground mode → cause:
  child service logs are being routed to the terminal with `--verbose` or
  `--logs terminal`. Default local runs should keep `eshu-ingester.log` and
  `eshu-reducer.log` under the workspace log directory and leave the terminal to
  the branded Bubble Tea known-work progress panel. The verdict line is the
  primary operator signal: `Complete` means all known work drained, `Indexing`
  means pending collector generations or active work remain, `Settling` means
  queued work or shared projection backlog remains, and `Attention` means a
  failure/dead-letter path is present. The collector row treats
  `scope_generations.status='active'` as the current snapshot, not a running
  worker; only pending generations should keep the collector waiting. When
  queue counters are zero but readiness is still `progressing`, check the
  `Shared projections` line before assuming the panel is stale. Use
  `--progress plain` when testing append-only output and `--progress quiet`
  only when another wrapper owns progress display.

- Symptom: the progress table is healthy but stays at `idle` after a
  local-authoritative restart → first check whether `cache/repos` was reset.
  A stale filesystem selector manifest can make the ingester skip collection
  against fresh Postgres state, so `resetLocalAuthoritativeState` must remove
  that directory while preserving embedded Postgres binaries.

- Symptom: `eshu graph status` reports `owner_present=true` but the owner PID is
  already dead → cause: stale `owner.json`; `eshu graph stop` must acquire
  `owner.lock`, stop any recorded embedded Postgres child, and remove the stale
  metadata for both `local_lightweight` and `local_authoritative` profiles.

- Symptom: `local_authoritative` spends minutes on old projector work or stale
  graph retraction after restart → cause: rebuildable local state was preserved
  across owner starts; `local_host_reset.go` must clear Postgres `data` /
  `runtime` and graph `nornicdb` state after `owner.lock` acquisition and
  before embedded Postgres starts.

- Symptom: a `eshu admin` command returns a non-200 response → cause: the
  `APIClient` target URL is wrong or the API server is down; check the
  service URL config and that `eshu api start` is running.

## Anti-patterns specific to this package

- **Business logic in subcommand `RunE` functions** — `RunE` functions should
  call `apiClientFromCmd`, `procexec.Exec`, or a delegating helper. Domain logic
  (graph writes, fact queries, schema checks) belongs in the `internal/*`
  packages that own those surfaces.

- **Direct driver or Postgres calls in this package** — this binary is a CLI
  dispatcher. It must not open Postgres or graph driver connections except
  through `internal/runtime` helpers already used here. All data-plane work
  runs in the launched binaries.

- **Adding a hidden command without tests** — hidden `local-host` subcommands
  have integration-level tests in `local_host_supervision_test.go` and
  `service_local_test.go`. New hidden commands need coverage before merging.

## What NOT to change without an ADR

- The `local-host watch` and `local-host mcp-stdio` subcommand contract — the
  `eshu mcp start` and `eshu graph start` paths hard-code these subcommand names
  when calling `procexec.Exec`; renaming them silently breaks both flows.
- The `--database` flag name and its effect on `ESHU_RUNTIME_DB_TYPE` — external
  scripts and the local-authoritative profile depend on this flag; see
  `docs/public/reference/cli-reference.md`.
