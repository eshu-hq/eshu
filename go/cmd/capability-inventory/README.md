# cmd/capability-inventory

`capability-inventory` generates and verifies the reconciled Eshu capability
catalog artifact. It is the only binary that joins the editorial overlay and the
live MCP tool registry into the deterministic catalog consumed by docs, CI, the
API, MCP, and the console.

## Usage

Run from the `go` module directory:

```bash
# Print reconciliation findings and the entry count.
go run ./cmd/capability-inventory -mode report

# Regenerate the committed artifact after a matrix or overlay change.
go run ./cmd/capability-inventory -mode generate

# Drift gate: fail when findings exist or the embedded artifact is stale.
go run ./cmd/capability-inventory -mode verify

# Docs freshness guard: fail when a capability-state marker contradicts the catalog.
go run ./cmd/capability-inventory -mode docs
```

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-mode` | `report` | `report`, `generate`, `verify`, or `docs` |
| `-specs` | `../specs` | path to the specs directory |
| `-out` | `internal/capabilitycatalog/data/catalog.generated.json` | artifact output path (generate mode) |
| `-docs` | `../docs/public` | path to the docs directory (docs mode) |

## Invariants

- The binary is a thin driver: all reconciliation lives in
  `internal/capabilitycatalog`. `main.go` only collects the MCP registry through
  `mcp.ReadOnlyTools`, builds, and dispatches on `-mode`.
- `verify` is the CI gate. It fails when reconciliation findings exist or when
  the embedded artifact differs from a fresh regeneration.
- `generate` writes deterministic JSON; the same inputs always produce the same
  bytes, so a regenerated artifact only changes when the matrix, overlay, or
  registry changed.

## Related

- `go/internal/capabilitycatalog/README.md`
- `docs/public/reference/capability-catalog.md`
