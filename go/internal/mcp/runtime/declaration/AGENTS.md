# AGENTS.md — MCP runtime declaration route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the request-selection contract.
4. `tools.go` and `tools_test.go` for the owned registration definition.
5. `../../dispatch_repositories.go` for the root adapter that calls `Route`.
6. `../../../query/runtime/declaration/README.md` for the handler contract.

## Invariants

- Keep only family membership, pure argument-to-request selection, and the
  tool definition here. Fanout, the registration splice and its order, the
  adapter, and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package runtimedeclarationtools`.
- Preserve `POST /api/v0/runtime-declarations/query` and the tool name.
- Forward every argument as sent. Do not select keys, default values, or coerce
  types here: the handler's strict decode is what rejects a misspelled filter,
  and a dropped filter answers with every declaration.
- Return the zero request and `handled=false` for any other tool, including
  near-miss names.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.
- The `limit` bounds and `repo_ids` cap in the schema mirror the handler's
  (`maxLimit` 200, `maxRepoIDs` 100); change them together.

## Verification

```bash
go test ./internal/mcp/runtime/declaration ./internal/mcp -count=1
go vet ./internal/mcp/runtime/declaration ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root.
