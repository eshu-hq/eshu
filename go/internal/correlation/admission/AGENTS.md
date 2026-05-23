# internal/correlation/admission Agent Rules

This package owns the candidate-level confidence and structural-evidence gates.
It MUST NOT order rules, choose winners, append rejection reasons, render
explain output, or materialize graph truth.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `admission.go` and `admission_test.go`.
3. `../rules/README.md`, `../engine/README.md`, and `../model/README.md`.
4. Root correlation truth gates before changing admission behavior.

## Local Invariants

- Thresholds MUST stay in `[0,1]`.
- `Evaluate` MUST validate candidates and requirements before evaluating gates.
- `Evaluate` MUST return a copy and must not mutate the input candidate.
- Empty requirement sets MUST pass structure.
- `MatchAll` selectors are conjunctive against one evidence atom.
- Selector comparisons MUST remain exact string equality: no case-folding,
  whitespace tolerance, prefix matching, or normalization.
- Unknown evidence fields resolve to empty string and fail non-empty selectors.
- Rejection reasons are appended by the engine, not here.

## Change Rules

- New `EvidenceField` values MUST add `evidenceFieldValue` dispatch and tests.
- Matching semantic changes affect every pack and require architecture-owner
  approval, docs, and positive/negative/ambiguous proof.
- New gates require an `Outcome` field plus engine-owned rejection reason
  handling.
- Do not infer platform, environment, cluster, or service truth from evidence
  string patterns.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/correlation/admission -count=1
go vet ./internal/correlation/admission
go doc ./internal/correlation/admission
```

Admission-contract changes require `go test ./internal/correlation/... -count=1`.
Docs-only edits also need the package-doc verifier for this directory and
`git diff --check`.
