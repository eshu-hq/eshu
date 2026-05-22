# Eshu CLI Command

## Purpose

`cmd/eshu` builds the unified Eshu CLI and local service launcher. The binary
owns the Cobra command tree, local graph/service orchestration, graph backend
install commands, operator/admin commands, documentation verification, and
diagnostics.

## Ownership boundary

`eshu` owns CLI parsing and local orchestration. Service internals stay in the
service binaries:

- `eshu api start` execs `eshu-api`.
- `eshu mcp start --transport http` execs `eshu-mcp-server`.
- `eshu mcp start` in stdio mode attaches through `local-host mcp-stdio`.
- `eshu graph start` supervises the local owner and discovers
  `eshu-ingester` and `eshu-reducer` through `PATH`.

It does not own query handler behavior, collector parsing, reducer truth, or
deployed Kubernetes runtime wiring.

## Exported surface

This is a `package main` binary. Its public contract is the command tree,
`--version` / `-v`, `eshu version`, and command-specific flags.

The command tree covers service launch, local graph lifecycle, local indexing,
query helpers, admin operations, diagnostics, config, and documentation truth
verification. `root.go` owns persistent flags such as `--database` and
`--visual`.

## Dependencies

- Cobra for command dispatch.
- `internal/eshulocal` for workspace-local ownership and layout.
- `internal/query` for profile and graph-backend contracts used by local
  service attachment.
- `internal/buildinfo` for version output.
- Service binaries on `PATH` for exec-based local and service launch paths.

## Telemetry

The Cobra dispatcher does not bootstrap OTEL. Telemetry runs inside launched
runtimes through the shared `telemetry` package. CLI errors print to stderr and
return exit code `1` unless a command defines a more specific code, such as
ambiguous `trace service` results exiting `3`.

## Gotchas / invariants

- `SilenceUsage` and `SilenceErrors` are set on the root command.
- `eshu graph start` needs fresh helper binaries on `PATH`; run
  `./scripts/install-local-binaries.sh` before local runtime validation.
- The local authoritative profile rebuilds from the workspace source tree on
  owner start and clears rebuildable local Postgres/runtime and graph data.
- `eshu graph start --progress auto` renders the local status panel; `plain`,
  `quiet`, `--verbose`, and `--logs` change only presentation/log routing.
- Bolt readiness uses a real Bolt handshake, not a TCP-only dial, because
  embedded NornicDB can accept TCP before Bolt is ready.
- Embedded and process NornicDB use per-workspace credentials written under the
  local graph data directory and passed to child services.
- `eshu mcp start --workspace-root <repo>` attaches to the active local owner
  and fails fast if owner, Postgres, or graph health is not ready.
- `eshu scan` runs `eshu-bootstrap-index` against one local source, then polls
  API/status readiness until the pipeline is healthy, queues are drained, and
  no failures or dead letters remain.
- `eshu docs verify [path]` validates Markdown-family CLI, HTTP endpoint,
  `ESHU_*` environment-variable, local repo path, tagged or digested container
  image reference, and Terraform block-address claims. It also marks known
  shell-command examples as unsupported claim types and ignores generic path
  examples such as globs, placeholders, and bare filenames. Without `--persist`,
  it does not open Postgres or graph connections.
- `--database` mutates the process environment via `os.Setenv`.

## Focused tests

```bash
cd go
go test ./cmd/eshu -count=1
go test ./cmd/eshu -run 'TestDocs|TestRootVersion|TestGraph|TestMCP' -count=1
```

## Related docs

- [Service runtimes](../../../docs/public/deployment/service-runtimes.md)
- [CLI reference](../../../docs/public/reference/cli-reference.md)
- [CLI indexing](../../../docs/public/reference/cli-indexing.md)
- [Local testing](../../../docs/public/reference/local-testing.md)
