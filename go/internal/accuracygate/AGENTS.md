# AGENTS.md — internal/accuracygate guidance

## Read first

1. `README.md` — package purpose, ownership boundary, and the no-tautology rule
2. `doc.go` — godoc contract
3. `gate.go` — dimensions, baseline contract, threshold gate, report rollup
4. `golden_gate_test.go` — how real measurements are taken and wired
5. `docs/public/reference/local-testing.md` — verification gates

## Invariants this package enforces

- The gate measures shipped behavior. Measurements come from the real parser,
  resolutionparity, and admissionaudit harnesses, never from hand-written
  numbers that mirror the baseline.
- `testdata/baseline.json` floors are minimums. Accuracy may improve freely; a
  drop below the floor, a scored language regressing, or a missing measurement
  fails the gate.
- The package stays standard-library-only plus `parser/goldenaudit` for the
  Score convention. It does not import parser, reducer, query, or storage from
  non-test files.
- Output is deterministic: fixed dimension order, sorted labels.

## Common changes

- Raising a floor after a real, measured improvement is a deliberate reviewed
  commit. Never lower a floor to make a regression pass.
- Adding a dimension requires: a `Dimension` constant, a baseline threshold,
  real measurement wiring in the test, and README/doc.go updates.

## What not to change

- Do not feed the gate hand-written metrics that duplicate the baseline.
- Do not add production telemetry or graph/storage dependencies here.
- Do not make a missing or gated-but-unmeasured dimension pass.
