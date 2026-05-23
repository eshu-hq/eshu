# cmd/eshu Agent Rules

These rules apply only inside `go/cmd/eshu/`. Root `AGENTS.md` still controls
global proof, performance, concurrency, and skill requirements.

## Read First

- `go/cmd/eshu/README.md`
- `go/cmd/eshu/doc.go`
- `go/cmd/eshu/main.go`
- `go/cmd/eshu/root.go`
- `go/cmd/eshu/service.go`
- `go/cmd/eshu/basic.go`
- `go/cmd/eshu/graph.go`

## Local Invariants

- MUST keep `SilenceUsage` and `SilenceErrors` on `rootCmd`; operator scripts
  depend on stable stderr.
- MUST keep `--database` wired through `PersistentPreRunE` to
  `ESHU_RUNTIME_DB_TYPE`; child process exec paths inherit that environment.
- MUST preserve service launch through the existing exec helpers. Code after an
  exec point does not run.
- MUST keep removed commands on `removedCommandError`; deleted command names
  must not silently succeed or panic.
- MUST keep `local-host watch` and `local-host mcp-stdio` names stable unless
  every caller in `eshu mcp start` and `eshu graph start` is changed together.
- MUST keep hidden local-host commands hidden. They are supervisor entrypoints,
  not public CLI surface.
- MUST keep normal data-plane behavior out of Cobra `RunE` functions. This
  package dispatches to API clients, exec helpers, or internal packages.
- MUST rebuild helper binaries before validating `eshu graph start`,
  `eshu mcp start`, or `eshu index` paths that discover binaries through
  `PATH`.

## Change Gates

- New `admin` commands belong in `admin.go` and MUST use `apiClientFromCmd`
  for authenticated API requests.
- New `graph` commands belong in `graph.go` and MUST be wired into the existing
  `graphCmd` tree.
- New persistent flags belong in `root.go`; flags that affect child processes
  MUST be threaded through environment setup deliberately.
- Local progress-panel changes MUST preserve the status distinctions between
  pending collector generations, active queue work, shared projection backlog,
  complete state, and failure/dead-letter state.
- Command contract changes MUST update the CLI reference and focused command
  tests.

## Focused Verification

```bash
cd go
go test ./cmd/eshu -count=1
go test ./cmd/eshu -run 'TestDocs|TestRootVersion|TestGraph|TestMCP' -count=1
```
