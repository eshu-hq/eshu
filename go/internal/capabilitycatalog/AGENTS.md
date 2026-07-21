# AGENTS.md — internal/capabilitycatalog guidance for LLM assistants

## Read first

1. `go/internal/capabilitycatalog/README.md` — inputs, output, maturity rules,
   and the generated artifact.
2. `go/internal/capabilitycatalog/catalog.go` — `Build`, the catalog/entry/
   surface types, surface classification, and maturity derivation wiring.
3. `go/internal/capabilitycatalog/reconcile.go` — the `Finding` taxonomy and the
   reconciliation that compares overlay and signals against the matrix.
4. `docs/public/reference/capability-conformance-spec.md` and
   `docs/public/reference/capability-catalog.md` — the contract this package
   mirrors.

## Invariants this package enforces

- **No mcp/query imports.** The catalog must stay free of HTTP and graph
  dependencies. Live registries are injected through `Signals`. Importing `mcp`
  or `query` here would create a cycle once those packages consume the catalog.
- **Deterministic output.** Entries are sorted by capability id, surfaces by
  tool, proof signals by kind then ref. `MarshalCatalog` produces stable JSON.
  Any new collection must be sorted before it reaches the artifact.
- **Build is pure.** `Build` does no I/O. Only `LoadMatrix`, `LoadOverlay`,
  `BuildFromSpecs`, and `Load` touch the filesystem or the embedded artifact.
- **Findings are the gate.** A non-empty `[]Finding` means the catalog is not
  reconciled. The generator's verify mode fails on findings; never silence a
  finding by loosening reconcile — resolve it in `specs/capability-catalog.v1.yaml`.

## Common changes and how to scope them

- **New catalog field** → add it to `Entry` (and `OverlayCapability` if
  editorial), thread it through `buildEntry`, regenerate the artifact, and update
  the drift test expectation. Why: the embedded artifact is golden.
- **New reconciliation rule** → add a `FindingKind`, a helper in `reconcile.go`,
  and a focused test; resolve any real-spec findings it surfaces in the overlay.
- **Matrix shape change** → update `matrixFile*` structs and `convertCapability`;
  keep `effectiveStatus` correct for rows that omit `status`.
- **New verification kind** → add it to `allowedVerificationKinds` in
  `matrix.go` first; an unlisted key is a hard `LoadMatrix` error by design
  (#5407). Do not loosen the allow-list to unblock a one-off spec change.
- **New or moved `remote_validation` ref** → commit the evidence at
  `docs/internal/remote-validation/<ref>.md`. The baseline is a frozen audited
  set guarded by a `# FROZEN_MAX: <N>` ceiling: it may shrink but not grow, so
  `-update` will NOT quietly baseline a new unverified `production:supported`
  ref — the regenerated file lands over the ceiling and fails the gate. Do not
  hand-edit the baseline; the only legitimate ways to clear the gate are
  committing an artifact or, for a deliberate scope decision, an explicit
  separately-reviewed raise of the FROZEN_MAX line. The systemic per-row
  burn-down is tracked in #5552 (blocks epic #5344).

## Failure modes and how to debug

- Symptom: `go:embed data/catalog.generated.json` build failure → cause: the
  artifact file is missing → run `go run ./cmd/capability-inventory -mode generate`.
- Symptom: drift test fails as stale → cause: specs or registry changed without
  regenerating → regenerate the artifact and commit it.
- Symptom: drift test fails with findings → cause: a new MCP tool or matrix tool
  is unmapped → add an overlay exemption/non-MCP-surface with a reason, or map it.

## What NOT to change without an ADR

- The capability id vocabulary or profile ids — they are a product contract
  shared with `go/internal/query/contract.go` and the matrix.
