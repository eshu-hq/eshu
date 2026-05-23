# internal/correlation/engine Agent Rules

This package owns deterministic evaluation orchestration: rule ordering,
candidate admission calls, rejection reason attachment, tie-breaking, and final
result ordering. It MUST NOT define rule packs, classify drift values, render
explain output, or write graph truth.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `engine.go` and `engine_test.go`.
3. `../admission/README.md`, `../rules/README.md`, and `../model/README.md`.
4. Root correlation truth gates before changing which candidates admit.

## Local Invariants

- `Evaluate` MUST validate the pack and return no partial results on error.
- Rule order MUST remain deterministic by `(Priority, Name)`.
- Result order MUST remain deterministic by
  `(CorrelationKey, admitted-before-rejected, ID)`.
- Tie-break winners MUST be selected by higher confidence, then lower
  lexicographic candidate ID. Losers receive `lost_tie_break`.
- `admission.Evaluate` returns outcomes; this package appends rejection reasons.
- Match counts currently apply only to `RuleKindMatch`.
- Do not make map iteration order observable in output, explain traces, or
  replay.
- Do not add namespace/folder/key-prefix heuristics that invent deployment or
  environment truth.

## Change Rules

- New rejection reason: add model constant, append it here, update root summary
  if operators need a counter, and add focused engine tests.
- New rule kind affecting counts: update the match-count loop and tests.
- Tie-break or sort changes are graph-truth changes; they require positive,
  negative, and ambiguous correlation proof plus explain/golden updates.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/correlation/engine -count=1
go vet ./internal/correlation/engine
go doc ./internal/correlation/engine
```

Admission or materialization-shape changes require
`go test ./internal/correlation/... -count=1` and the correlation proof matrix.
Docs-only edits also need the package-doc verifier for this directory and
`git diff --check`.
