# cmd/capability-inventory

`capability-inventory` generates and verifies the reconciled Eshu capability
catalog artifact. It is the only binary that joins the editorial overlay and the
live MCP tool registry into the deterministic catalog consumed by docs, CI, the
API, MCP, and the console.

It also generates and verifies the **surface inventory**: every platform surface
across six categories (command binaries, collector families, reducer domains, API
routes, MCP tools, console pages) enumerated from live code, specs, and the
source tree, reconciled against `specs/surface-inventory.v1.yaml`. The drift gate
fails CI when a surface is added or removed in code without regenerating the
committed `internal/capabilitycatalog/data/surface-inventory.generated.json`.

## Usage

Run from the `go` module directory:

```bash
# Print reconciliation findings and the entry count.
go run ./cmd/capability-inventory -mode report

# Regenerate the committed artifact after a matrix or overlay change.
go run ./cmd/capability-inventory -mode generate

# Drift gate: fail when findings exist or the embedded artifact is stale.
go run ./cmd/capability-inventory -mode verify

# Docs guards: fail when a capability-state marker contradicts the catalog, or a
# collector-state marker contradicts the surface inventory / claims implemented
# without linked promotion proof.
go run ./cmd/capability-inventory -mode docs
```

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-mode` | `report` | `report`, `generate`, `verify`, or `docs` |
| `-specs` | `../specs` | path to the specs directory (matrix, overlay, surface overlay) |
| `-out` | `internal/capabilitycatalog/data/catalog.generated.json` | catalog artifact output path (generate mode) |
| `-surface-out` | `internal/capabilitycatalog/data/surface-inventory.generated.json` | surface artifact output path (generate mode) |
| `-docs` | `../docs/public` | path to the docs directory (docs mode) |
| `-root` | `..` | path to the repository root (surface enumeration) |

## Invariants

- The binary is a thin driver: all reconciliation lives in
  `internal/capabilitycatalog`. `main.go` collects the MCP registry through
  `mcp.ReadOnlyTools` and the live surfaces through `scope`, `reducer`, `query`,
  and the source tree, builds, and dispatches on `-mode`.
- `verify` is the CI gate. It fails when catalog or surface reconciliation
  findings exist or when either embedded artifact differs from a fresh
  regeneration. `docs` mode never enumerates the source tree, so it needs no
  `-root`.
- `generate` writes deterministic JSON for both the catalog and the surface
  inventory; the same inputs always produce the same bytes, so a regenerated
  artifact only changes when the matrix, overlay, registry, or a live surface
  changed.

## Related

- `go/internal/capabilitycatalog/README.md`
- `docs/public/reference/capability-catalog.md`
