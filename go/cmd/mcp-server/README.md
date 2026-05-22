# cmd/mcp-server

## Purpose

`cmd/mcp-server` builds `eshu-mcp-server`, the MCP transport runtime for Eshu
tools. It serves MCP over HTTP or stdio, wires the same query/content stores
used by the HTTP API, and mounts the shared runtime admin surface in HTTP mode.

## Ownership boundary

This command owns MCP process startup, transport selection, query router wiring,
API auth wrapping for mounted `/api/*` routes, pprof opt-in, and shutdown. Tool
dispatch lives in `internal/mcp`; query semantics live in `internal/query`.

## Exported surface

The command package exports no API. Its process contract is `--version`/`-v`,
`ESHU_MCP_TRANSPORT`, `ESHU_MCP_ADDR`, datastore/backend/profile config,
HTTP routes, stdio mode, and signal-driven shutdown. See `doc.go` for the
binary summary.

## Dependencies

The binary wires `internal/mcp`, `internal/query`, `internal/runtime`,
`internal/storage/postgres`, `internal/app`-style runtime admin helpers, and
`internal/telemetry`. It reads graph/content/status through ports and
Postgres-backed adapters.

## Telemetry

Startup uses `telemetry.NewBootstrap("mcp-server")`, service/component
`mcp-server`, runtime startup/shutdown/connect events, optional pprof through
`ESHU_PPROF_ADDR`, and the shared runtime info gauge. Per-request metrics and
spans are emitted by query handlers and MCP dispatch, not duplicated here.

## Gotchas / invariants

- Version probes must exit before telemetry, pprof, datastore, or transport
  setup.
- `stdio` mode does not start HTTP routes or the admin surface.
- `wireAPI` validates API key, query profile, and graph backend before opening
  stores.
- `ESHU_POSTGRES_DSN` or `ESHU_CONTENT_STORE_DSN` is required before graph
  connection setup.
- Mounted `/api/*` routes use query auth, not MCP transport auth.
- MCP-exposed route stores such as IaC, package, CI/CD, SBOM, supply-chain, and
  container-image stores must stay non-nil.

## Focused tests

```bash
cd go
go test ./cmd/mcp-server -run 'Test.*Version|Test.*Wiring|Test.*Runtime|Test.*Transport' -count=1
go test ./cmd/mcp-server ./internal/mcp ./internal/query -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/guides/mcp-guide.md`
- `docs/public/run-locally/docker-compose.md`
- `go/internal/mcp/README.md`
- `go/internal/query/README.md`
