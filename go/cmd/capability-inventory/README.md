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

# Product claim ledger guard only: the same ledger/marker/live-issue check
# `docs` mode runs, without the capability-state and collector-state marker
# scans. Use this when another gate in the same CI run already covers those
# (mcp-schema-drift.yml runs full `docs` mode on every PR), so the
# product-claim-ledger workflow does not repeat the whole docs-tree scan.
go run ./cmd/capability-inventory -mode product-claims

# Capability budget proof guard: fail when a public measurement artifact does
# not bind every supported p95/max-scope budget row to measured API/MCP proof.
go run ./cmd/capability-inventory -mode budget-proof \
  -budget-artifact ../capability-budget-proof.json

# Remote-validation artifact-existence gate: fail when a matrix
# remote_validation ref has no committed docs/internal/remote-validation/<ref>.md
# artifact and is not listed in specs/remote-validation-baseline.txt.
go run ./cmd/capability-inventory -mode remote-validation

# Regenerate the burn-down baseline from the current tree.
go run ./cmd/capability-inventory -mode remote-validation -update

# Authenticated exact-head #5273 graph-read sweep. ESHU_MCP_TOKEN is the user
# bearer token; ESHU_API_KEY is the separately labeled admin/all-scope
# credential required by direct Cypher and visualization.
ESHU_API_BASE_URL=https://api.example.invalid \
ESHU_MCP_URL=https://mcp.example.invalid/mcp \
ESHU_MCP_TOKEN=... ESHU_API_KEY=... \
go run ./cmd/capability-inventory -mode graph-read-probe
```

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-mode` | `report` | `report`, `generate`, `verify`, `docs`, `product-claims`, `budget-proof`, `remote-validation`, or `graph-read-probe` |
| `-specs` | `../specs` | path to the specs directory (matrix, overlay, surface overlay) |
| `-out` | `internal/capabilitycatalog/data/catalog.generated.json` | catalog artifact output path (generate mode) |
| `-surface-out` | `internal/capabilitycatalog/data/surface-inventory.generated.json` | surface artifact output path (generate mode) |
| `-budget-artifact` | empty | public capability budget proof artifact path (budget-proof mode) |
| `-docs` | `../docs/public` | path to the docs directory (docs and product-claims modes) |
| `-root` | `..` | path to the repository root (surface enumeration, remote-validation mode) |
| `-remote-validation-baseline` | `../specs/remote-validation-baseline.txt` | path to the remote_validation burn-down baseline (remote-validation mode) |
| `-update` | `false` | regenerate the remote-validation baseline instead of checking it (remote-validation mode) |
| `-api-base-url` | `ESHU_API_BASE_URL` | branch-built API base URL (graph-read-probe mode) |
| `-mcp-url` | `ESHU_MCP_URL` | exact branch-built MCP HTTP endpoint URL (graph-read-probe mode) |
| `-user-token-env` | `ESHU_MCP_TOKEN` | env-var name holding the user bearer token (graph-read-probe mode) |
| `-admin-token-env` | `ESHU_API_KEY` | env-var name holding the admin/all-scope credential required by direct Cypher surfaces (graph-read-probe mode) |

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
  enables that with the read-only Actions token on pull requests and trusted
  claim-relevant push, schedule, and manual-dispatch events.
- `product-claims` mode runs only the product claim ledger guard (the same
  check `docs` mode runs as its last step), skipping the capability-state and
  collector-state marker scans. It exists so a CI workflow or local run that
  only needs the ledger/live-issue guard does not have to repeat the full
  docs-tree walk `docs` mode performs; see #4073. Both modes share the same
  `checkProductClaims` helper, so they can never diverge on what counts as a
  ledger finding.
- `generate` writes deterministic JSON for both the catalog and the surface
  inventory; the same inputs always produce the same bytes, so a regenerated
  artifact only changes when the matrix, overlay, registry, or a live surface
  changed.
- `remote-validation` mode (`remote_validation_mode.go`) is the artifact-
  existence gate for `remote_validation` proof-IDs (#5407): it never builds
  the full catalog, only `capabilitycatalog.LoadMatrix` plus
  `CheckRemoteValidationArtifacts` and the baseline's ratcheting FROZEN_MAX
  ceiling check (`RemoteValidationBaselineCeilingExceeded`), so it stays cheap
  enough to run on every matrix or baseline change. The ceiling fails the gate
  when the baseline entry count exceeds it, so the frozen debt set cannot grow.
  `-update` regenerates `specs/remote-validation-baseline.txt` from the current
  tree and ratchets FROZEN_MAX down to the new count; it never raises the
  ceiling and never requires a human to hand-edit the file.
- `graph-read-probe` derives the complete current API/MCP name set from the
  served OpenAPI and MCP registries plus the five known directly registered
  HTTP surfaces. Its checked-in fixture registry currently executes the seven
  direct #5273 graph-read entry points, then fails explicitly with a count and
  first name for every current surface that still lacks a safe fixture. It
  never silently skips an unsupported target or prints token values.

## Related

- `go/internal/capabilitycatalog/README.md`
- `docs/public/reference/capability-catalog.md`
