# AGENTS.md — go/internal/mcp/code/divergence

## Read first

1. `go/internal/mcp/code/divergence/README.md` — purpose and handler defaults
2. `go/internal/mcp/code/divergence/doc.go` — the route-selection contract
3. `go/internal/query/codequery/divergence.go` — the bounded reads behind the two POST paths
4. `go/internal/query/codedivergence/doc.go` — the finding contract (reasons sum to score, counted suppressions)

## The rule that shapes this package

Pure route selection, no execution. `Route` maps decoded arguments to an
internal request; the parent `mcp` package owns registration order, fanout,
dispatch, authorization, budgets, envelopes, and telemetry. Keep every tool
name, path, and body key stable — the dispatch contract test locks them.

## Verification

```bash
cd go && go test ./internal/mcp/code/divergence/... -count=1
```
