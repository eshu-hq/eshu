# Correlation Model

## Purpose

`correlation/model` defines the candidate, evidence, state, and rejection-reason
types shared by the correlation pipeline.

## Ownership boundary

This package owns the shared data model and validation rules. It does not own
rule schemas, evaluation flow, admission gates, winner selection, rendering, or
materialization.

## Exported surface

Use `doc.go` and `go doc ./internal/correlation/model` for the contract.
Callers depend on `Candidate`, `EvidenceAtom`, `CandidateState`,
`RejectionReason`, and their validation methods.

## Dependencies

`model` imports only the Go standard library.

## Telemetry

None. These are pure data types.

## Gotchas / invariants

- Candidate and evidence confidence values must stay in `[0,1]`.
- Required identity fields are trimmed before validation; whitespace-only
  values are invalid.
- `EvidenceAtom.Value` may be empty. It is an optional qualifier, not identity.
- `CandidateStateProvisional` is an intermediate state. Final engine output
  should be admitted or rejected.
- A candidate can carry multiple rejection reasons. Do not collapse
  low-confidence, structural-mismatch, and tie-break reasons into one value.
- Fixtures should use `Validate` instead of constructing invalid pipeline states
  for convenience.

## Related docs

- `go/internal/correlation/README.md`
- `go/internal/correlation/rules/README.md`
- `go/internal/correlation/engine/README.md`
- `go/internal/correlation/admission/README.md`
