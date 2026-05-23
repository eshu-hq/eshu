# cmd/mcp-server Agent Rules

These rules apply only inside `go/cmd/mcp-server/`. Root `AGENTS.md` still
controls global proof, performance, concurrency, and skill requirements.

## Read First

- `go/cmd/mcp-server/README.md`
- `go/cmd/mcp-server/doc.go`
- `go/cmd/mcp-server/main.go`
- `go/cmd/mcp-server/wiring.go`
- `go/internal/mcp/README.md`
- `go/internal/query/README.md`

## Local Invariants

- MUST keep version probes before telemetry, pprof, datastore, graph, or
  transport setup.
- MUST validate query profile, graph backend, and API key before opening
  datastore or graph connections.
- MUST require Postgres through `ESHU_POSTGRES_DSN` or
  `ESHU_CONTENT_STORE_DSN`.
- MUST keep stdio mode free of HTTP routes and the admin surface.
- MUST keep mounted `/api/*` routes protected by query auth; MCP transport auth
  is not a substitute for API route auth.
- MUST keep MCP query router stores non-nil for IaC, package, CI/CD, SBOM,
  supply-chain, and container-image read models.
- MUST keep provider shutdown on a background context so cancelled roots do not
  prevent telemetry flush.

## Change Gates

- New query handlers MUST be added to `query.APIRouter`, wired in
  `newMCPQueryRouter`, asserted in wiring tests, and matched with MCP dispatch
  definitions and docs.
- Transport changes MUST update service-runtime and environment docs, plus
  startup, shutdown, auth, and telemetry tests.
- New env vars MUST be validated before datastore connection and covered by
  wiring tests.
- Admin-surface changes MUST go through the shared runtime mount path and keep
  HTTP-only behavior explicit.

## Focused Verification

```bash
cd go
go test ./cmd/mcp-server -run 'Test.*Version|Test.*Wiring|Test.*Runtime|Test.*Transport' -count=1
go test ./cmd/mcp-server ./internal/mcp ./internal/query -count=1
```
