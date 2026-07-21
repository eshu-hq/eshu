# AGENTS.md — cmd/capability-inventory guidance for LLM assistants

## Read first

1. `go/cmd/capability-inventory/README.md` — purpose, flags, invariants.
2. `go/cmd/capability-inventory/main.go` — `run`, `verify`, and `mcpSignals`;
   the entire business logic delegates to `internal/capabilitycatalog`.
3. `go/internal/capabilitycatalog/README.md` — the reconciliation contract this
   binary drives.

## Invariants this package enforces

- **Thin driver.** `main` only calls `run`. `run` collects the MCP registry via
  `mcp.ReadOnlyTools`, calls `capabilitycatalog.BuildFromSpecs`, and dispatches
  on `-mode`. No reconciliation logic belongs here.
- **verify is the gate.** It fails when findings exist or the embedded artifact
  is stale. Do not weaken this; it is the drift defense for the catalog.
- **generate is deterministic.** Output bytes change only when the matrix,
  overlay, or registry changed.

## Common changes and how to scope them

- **New mode that needs the full reconciled catalog** → add a case to the
  `switch` in `run`, document the flag, add a `main_test.go` case. Why: the
  switch is the mode dispatch point for catalog-backed modes.
- **New mode that does not need the full catalog** (like `budget-proof` and
  `remote-validation`) → add an early `if *mode == "..."` return in `run`
  before `BuildFromSpecs` runs, in its own file (`remote_validation_mode.go`
  is the worked example), with its own test file. Why: `BuildFromSpecs`
  collects the live MCP registry and is unnecessary work for a mode that only
  needs `LoadMatrix`.
- **Additional live signal** (for example API operation ids) → extend
  `mcpSignals` to populate the new `Signals` field; keep collection deterministic.

## Failure modes and how to debug

- Symptom: `verify` reports stale → regenerate with `-mode generate` and commit.
- Symptom: `verify` lists findings → resolve them in
  `specs/capability-catalog.v1.yaml`, do not edit reconcile to hide them.
- Symptom: `-specs` not found → run from the `go` module directory or pass an
  absolute `-specs` path.

## What NOT to change without an ADR

- The `-mode` names (`report`, `generate`, `verify`, `docs`, `product-claims`,
  `budget-proof`, `remote-validation`) — CI and contributor workflows depend
  on them. `product-claims` is a narrower view of `docs` (product claim
  ledger guard only, see #4073) and must keep calling the same
  `checkProductClaims` helper `docs` mode uses so the two modes cannot
  silently diverge on what a ledger finding is.
