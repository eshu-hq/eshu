# Correlation Admission

## Purpose

`correlation/admission` applies the bounded confidence and structural-evidence
gates that decide whether one correlation candidate is admitted or rejected.

## Ownership boundary

This package owns the candidate-level gate and returned outcome. It does not
order rules, pick winners, append rejection reasons, render explain output, or
materialize graph truth.

## Exported surface

Use `doc.go` and `go doc ./internal/correlation/admission` for the contract.
`Evaluate` validates the candidate and evidence requirements, checks confidence,
checks exact-match evidence selectors, and returns a copy of the candidate with
state updated plus the gate outcome.

## Dependencies

`admission` depends on `correlation/model` for candidate and evidence atoms and
`correlation/rules` for evidence requirements and selectors.

## Telemetry

None. Callers attach telemetry around evaluation.

## Gotchas / invariants

- Threshold must be in `[0,1]`; values outside that range return an error.
- Empty requirement sets are structurally satisfied.
- Selector matching is exact string comparison. Whitespace, case, and prefixes
  are not normalized.
- Unknown evidence fields resolve to an empty value and fail matching instead of
  raising an error.
- `Evaluate` returns a copy and does not mutate the input candidate.
- Rejection reasons are appended by the engine after it inspects the outcome.
- A candidate is admitted only when both confidence and structure gates pass.

## Related docs

- `go/internal/correlation/engine/README.md`
- `go/internal/correlation/rules/README.md`
- `go/internal/correlation/model/README.md`
- `docs/public/reference/relationship-mapping.md`
