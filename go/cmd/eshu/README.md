# Eshu CLI Command

## Purpose

`cmd/eshu` builds the unified Eshu CLI and local service launcher. The binary
owns the Cobra command tree, local graph/service orchestration, graph backend
install commands, operator/admin commands, documentation verification, and
diagnostics.

## Ownership boundary

`eshu` owns CLI parsing and local orchestration. It does not own query handler
behavior, collector parsing, reducer truth, or deployed Kubernetes runtime
wiring. Service internals stay in service binaries: `eshu api start` execs
`eshu-api`; `eshu mcp start --transport http` execs `eshu-mcp-server`; stdio
MCP attaches through `local-host mcp-stdio`; `eshu graph start` supervises the
local owner and discovers helper binaries through `PATH`.

## Exported surface

This is a `package main` binary. Its public contract is the command tree,
`--version` / `-v`, `eshu version`, and command-specific flags. The tree covers
service launch, local graph lifecycle, local indexing, query helpers, admin
operations, diagnostics, config, graph backend installation, and documentation
truth verification.

## Dependencies

The command uses Cobra for dispatch, `internal/eshulocal` for workspace-local
ownership/layout, `internal/query` for profile and graph-backend contracts,
`internal/buildinfo` for version output, and service binaries on `PATH` for
exec-based launch paths.

## Telemetry

The Cobra dispatcher does not bootstrap OTEL. Telemetry runs inside launched
runtimes through the shared telemetry package. CLI errors print to stderr and
return exit code `1` unless a command defines a more specific code, such as
ambiguous `trace service` results exiting `3`.

## Gotchas / invariants

- `SilenceUsage` and `SilenceErrors` are set on the root command.
- `--database` mutates `ESHU_RUNTIME_DB_TYPE` in the current process before
  child-process exec.
- `eshu graph start` needs fresh helper binaries on `PATH`; run
  `./scripts/install-local-binaries.sh` before local runtime validation.
- Local-authoritative owner start clears rebuildable local Postgres/runtime,
  graph, and filesystem-selector state.
- Bolt readiness uses a Bolt handshake, not a TCP-only dial.
- Embedded and process NornicDB use per-workspace credentials under the local
  graph data directory.
- `eshu mcp start --workspace-root <repo>` fails fast when owner, Postgres, or
  graph health is not ready.
- `eshu docs verify [path]` validates Markdown-family claims without opening
  Postgres or graph connections unless persistence is requested.

## Focused tests

```bash
cd go
go test ./cmd/eshu -count=1
go run ./cmd/eshu docs verify ../go/cmd/eshu --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- [Service runtimes](../../../docs/public/deployment/service-runtimes.md)
- [CLI reference](../../../docs/public/reference/cli-reference.md)
- [CLI indexing](../../../docs/public/reference/cli-indexing.md)
- [Local testing](../../../docs/public/reference/local-testing.md)
