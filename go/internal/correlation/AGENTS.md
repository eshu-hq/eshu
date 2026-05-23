# internal/correlation Agent Rules

The root package is a reporting helper over completed evaluations. It MUST NOT
evaluate rules, run admission, render explain output, write graph truth, or
materialize deployable units.

## Read First

MUST read these before editing:

1. `README.md` and `doc.go`.
2. `observability.go` and `observability_test.go`.
3. `engine/README.md` and `model/README.md`.
4. Root correlation truth gates before changing counters that affect reducer
   status or query truth.

## Local Invariants

- `BuildSummary` MUST stay a pure, single-pass reduction over
  `engine.Evaluation`.
- `Summary.EvaluatedRules` MUST come from `Evaluation.OrderedRuleNames`, not
  the raw rule-pack length.
- One rejected candidate may increment multiple reason counters. Do not collapse
  low-confidence and tie-break conflict signals.
- Provisional candidates are upstream pipeline bugs and MUST NOT be counted as
  admitted or rejected here.
- New summary fields MUST represent data already present in the evaluation.
  Re-evaluation belongs in `engine` or `admission`.

## Proof

Run the focused gate for any edit:

```bash
cd go
go test ./internal/correlation -count=1
go vet ./internal/correlation
go doc ./internal/correlation
```

Reason or state changes usually need `go test ./internal/correlation/... -count=1`.
Docs-only edits also need the package-doc verifier for this directory and
`git diff --check`.
