# Go Runtime

The Go module owns the Eshu runtime: CLI, API, MCP server, ingester, reducer,
workflow coordinator, local owner mode, storage adapters, parser support, and
query surfaces.

Use [Local Testing](../docs/public/reference/local-testing.md) for the focused
gate that matches the surface you touched. The root README and local runbooks
show install paths for users; this directory is for maintainers working on the
runtime.

## Where to read next

This directory is the Go module root. Two children carry the actual code
and the rich documentation:

- `go/cmd/` — every Eshu binary, with per-binary `README.md`, `doc.go`, and
  scoped `AGENTS.md`.
- `go/internal/` — every internal package, with per-package `README.md`,
  `doc.go`, and scoped `AGENTS.md`.

Open `go/cmd/README.md` for the binary-to-runtime map, or
`go/internal/README.md` for the internal-package layout diagram and the
where-to-start-by-intent table.

## Per-package documentation convention

Every Go package directory under `go/` carries three docs with separate jobs:

- `doc.go` for the godoc contract.
- `README.md` for the architectural and operational lens humans read.
- `AGENTS.md` for scoped instructions that Codex and other coding-agent
  harnesses load while editing that package tree.

The `eshu-folder-doc-keeper` skill at `.agents/skills/` defines the
writing standards. The drift checker at
`scripts/check-docs-stale.sh` warns when source moves under a stale
README/doc.go pair. The slop gate at `scripts/verify-doc-claims.sh`
confirms every backticked Go identifier appears in source and every
file:line cite resolves correctly.

## Dependencies

This is the Go module root, not a Go package. Internal package boundaries
are documented under `go/internal/*/README.md` and
`go/internal/*/doc.go`.

## Telemetry

The runtime telemetry contract is owned by `internal/telemetry`. All
runtime-affecting packages route metrics, spans, and structured logs
through that contract.

## Related docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
