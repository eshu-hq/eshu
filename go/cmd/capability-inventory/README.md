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
Collector rows also carry a `collector_contract` manifest that maps emitted fact
kinds to projection/read surfaces, proof gates, and fixtures. The verifier fails
when a live collector fact kind is not covered by that manifest.

## Usage

Run from the `go` module directory:

```bash
# Print reconciliation findings and the entry count.
go run ./cmd/capability-inventory -mode report

# Regenerate the committed artifact after a matrix or overlay change.
go run ./cmd/capability-inventory -mode generate

# Drift gate: fail when findings exist or the embedded artifact is stale.
go run ./cmd/capability-inventory -mode verify

# Docs guards: fail when a capability-state marker contradicts the catalog, a
# collector-state marker contradicts the surface inventory, or a broad public
# product claim lacks a source-to-proof ledger row.
go run ./cmd/capability-inventory -mode docs

# Capability budget proof guard: fail when a public measurement artifact does
# not bind every supported p95/max-scope budget row to measured API/MCP proof.
go run ./cmd/capability-inventory -mode budget-proof \
  -budget-artifact ../capability-budget-proof.json
```

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-mode` | `report` | `report`, `generate`, `verify`, `docs`, or `budget-proof` |
| `-specs` | `../specs` | path to the specs directory (matrix, overlay, surface overlay) |
| `-out` | `internal/capabilitycatalog/data/catalog.generated.json` | catalog artifact output path (generate mode) |
| `-surface-out` | `internal/capabilitycatalog/data/surface-inventory.generated.json` | surface artifact output path (generate mode) |
| `-budget-artifact` | empty | public capability budget proof artifact path (budget-proof mode) |
| `-docs` | `../docs/public` | path to the docs directory (docs mode) |
| `-root` | `..` | path to the repository root (surface enumeration) |

## Invariants

- The binary is a thin driver: all reconciliation lives in
  `internal/capabilitycatalog`. `main.go` collects the MCP registry through
  `mcp.ReadOnlyTools` and the live surfaces through `scope`, `reducer`, `query`,
  and the source tree, builds, and dispatches on `-mode`.
- `verify` is the CI gate. It fails when catalog or surface reconciliation
  findings exist or when either embedded artifact differs from a fresh
  regeneration. For collector surfaces, surface findings include missing
  `collector_contract.fact_kinds` entries for live fact kinds. `docs` mode
  checks capability markers, collector markers, and the product claim ledger at
  `specs/product-claims.v1.yaml`; it uses `-root` to validate README/source-line
  anchors, `product-claim` markers, generated surfaces, surface-count
  expectations, proof paths, and catalog proof signals. Set
  `ESHU_VERIFY_PRODUCT_CLAIM_ISSUES_LIVE=1` to also check recorded issue states
  against GitHub with a bounded run-level timeout; `.github/workflows/product-claim-ledger.yml`
  enables that without `GITHUB_TOKEN` on pull requests and with `GITHUB_TOKEN`
  on trusted claim-relevant push, schedule, and manual-dispatch events.
- `generate` writes deterministic JSON for both the catalog and the surface
  inventory; the same inputs always produce the same bytes, so a regenerated
  artifact only changes when the matrix, overlay, registry, or a live surface
  changed.

## Related

- `go/internal/capabilitycatalog/README.md`
- `docs/public/reference/capability-catalog.md`
