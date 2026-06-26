# AGENTS.md - internal/collector/contracttest guidance

## Read First
1. `README.md` — package purpose and exported surface.
2. `contracttest.go` — the helpers.
3. `doc.go` — package contract statement.
4. `specs/collector_fact_contract.v1.yaml` — the per-collector spec.

## Invariants
- Test-only package. No runtime imports beyond `testing`, `context`, `facts`,
  and `awscloud`.
- Every exported helper calls `t.Helper()` first.
- `AssertFactKinds` checks subset membership — undeclared kinds fail the test.
- `AssertRequiredPayloadKeys` checks declared keys only — extra keys are
  allowed (some facts carry service-specific optional fields).
- `ScanFunc` is deliberately a function type, not a scanner interface, so
  callers can wrap any scanner without implementing an interface.

## Common Changes
- Add a new contract assertion by adding an exported function with a `t
  *testing.T` first parameter and `t.Helper()` call.
- Extend `FactKindShape` only when a meaningful cross-collector constraint
  emerges (e.g., forbidden keys, schema version checks).

## What Not To Change Without An ADR
- Do not add runtime imports (database, HTTP, queue).
- Do not add collector-specific assertions — those belong in each collector's
  own test files.
