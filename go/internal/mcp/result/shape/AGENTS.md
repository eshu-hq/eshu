# AGENTS.md — MCP result shape guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for rendering, the response budget, and transport rules.
3. `../../dispatch_envelope.go` for `renderToolResult` (success results) and
   `mcpToolErrorResult` (error results). Each branches on the shape.
   `applyResponseBudget` in `../../dispatch_budget.go` also reads the shape:
   it skips `ResourceOnly` under `structured`.

## Invariants

- Keep this package dependency-neutral: standard library only.
- `Dual` is the default and its output must stay byte-for-byte what Eshu sent
  before this package existed. A change to `Dual` rendering is a wire break.
- The accepted values are a closed set. Adding one means updating the
  `shape` telemetry attribute, the env registry, the docs, and every reader of
  the result.

## Failure modes

- `Parse` is strict and does not trim. Every caller trims first
  (`parseShapeParam`, `loadResultShape`, and the golden corpus gate's flag
  parsing), so a new caller must trim too or padded input is rejected.
- A new value that the budget code does not know would be counted as `Dual`.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/result/shape ./internal/mcp -count=1
go vet ./internal/mcp/result/shape ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root.
