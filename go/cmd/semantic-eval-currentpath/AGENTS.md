# AGENTS.md - cmd/semantic-eval-currentpath guidance

## Read first

1. `go/cmd/semantic-eval-currentpath/README.md`
2. `go/cmd/semantic-eval-currentpath/main.go`
3. `go/internal/semanticeval/README.md`
4. `go/internal/semanticeval/currentpath/README.md`
5. `docs/docs/adrs/2026-05-15-nornicdb-semantic-retrieval-evaluation.md`

## Invariants

- Keep the binary one-shot and read-only.
- Do not import graph, storage, reducer, collector, query handler, or MCP
  packages. All runtime access must go through HTTP via `currentpath.Runner`.
- Keep requests bounded by suite mode, limit, and timeout.
- Do not write private repository ids into checked-in suites; use `{repo_id}`.
- Add tests before changing flag behavior, placeholder substitution, JSON
  output, or scoring flow.

## Verification

- `go test ./cmd/semantic-eval-currentpath -count=1`
- `go test ./internal/semanticeval/... -count=1`
- `scripts/verify-package-docs.sh`
