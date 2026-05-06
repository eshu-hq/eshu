# Go Runtime

The Go module owns the Eshu runtime: CLI, API, MCP server, ingester, reducer,
workflow coordinator, local owner mode, storage adapters, parser support, and
query surfaces.

Use `go test ./...` for broad package validation and `golangci-lint run ./...`
before merging Go changes. The root README and local runbooks show the install
paths for users; this directory is for maintainers working on the runtime.

## Where to read next

This directory is the Go module root. Two children carry the actual code
and the rich documentation:

- `go/cmd/` — every Eshu binary, with per-binary `README.md` + `AGENTS.md`.
- `go/internal/` — every internal package, with per-package `README.md` +
  `AGENTS.md` + `doc.go`.

Open `go/cmd/README.md` for the binary-to-runtime map and the pipeline
shape diagram, or `go/internal/README.md` for the internal-package layout
diagram and the where-to-start-by-intent table.

## Pipeline at a glance

```mermaid
flowchart LR
  source[git source] --> collector
  collector --> parser
  parser --> facts[(facts)]
  facts --> projector
  projector --> reducer
  projector --> cypher
  reducer --> cypher
  cypher --> graph[(graph backend)]
  facts --> postgres[(postgres)]
  api --> graph
  api --> postgres
  mcp[mcp-server] --> api
```

## Per-package documentation convention

Every Go package directory under `go/` carries three files: `doc.go`
(godoc contract), `README.md` (architectural and operational lens with
mermaid flows), and `AGENTS.md` (LLM-assistant guidance with file:line
invariants and anti-patterns).

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

- `docs/docs/architecture.md`
- `docs/docs/deployment/service-runtimes.md`
- `docs/docs/reference/local-testing.md`
