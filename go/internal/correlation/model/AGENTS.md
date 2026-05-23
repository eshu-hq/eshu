# internal/correlation/model Agent Rules

This package owns the shared candidate, evidence, state, and rejection-reason
types. It MUST remain pure data and validation: no rule matching, admission,
winner selection, rendering, telemetry, or materialization logic.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `types.go` and `types_test.go`.
3. Root correlation truth gates before changing any value that can flow into
   materialization or explain output.

## Local Invariants

- Candidate and evidence confidence values MUST stay in `[0,1]`.
- Required identity fields MUST reject blank or whitespace-only values.
- `EvidenceAtom.Value` is optional and may be empty.
- `CandidateStateProvisional` is pre-evaluation only. Final engine results
  MUST be admitted or rejected.
- `RejectionReasons` is a slice, not a set. Duplicate prevention belongs in the
  engine/admission flow, not this model package.
- Fixtures SHOULD call `Validate`; invalid model fixtures hide pipeline bugs.

## Change Rules

- New rejection reason: add the constant here, append it in the engine, update
  root summary if operators need a counter, and update explain/status tests.
- New candidate or evidence fields MUST define validation rules and update every
  downstream fixture constructor.
- New state values require validation plus an audit of switches in `engine`,
  `admission`, `correlation`, and explain/status consumers.

## Do Not Change Without Proof

- State and rejection reason string values are wire/output contracts once they
  appear in explain text, APIs, logs, or persisted graph/status data.
- `Candidate.CorrelationKey` is the tie-break and materialization grouping key.
  Changing its meaning requires correlation truth proof.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/correlation/model -count=1
go vet ./internal/correlation/model
go doc ./internal/correlation/model
```

Model-contract changes require `go test ./internal/correlation/... -count=1`.
Docs-only edits also need the package-doc verifier for this directory and
`git diff --check`.
