# AGENTS.md — go/internal/query/codedivergence

## Read first

1. `go/internal/query/codedivergence/README.md` — score contract and suppression catalogue
2. `go/internal/query/codedivergence/doc.go` — the package contract
3. `docs/internal/evidence/6834-code-divergence-theory.md` §5 and §8 — the measured precision, thresholds, and score invariant this package implements
4. `go/internal/query/codequery/divergence.go` — the HTTP handlers serving this contract

## Rules that shape this package

- Score is members × tokens and reasons sum to it exactly. Any new reason
  must carry the weight that keeps the invariant; boolean signals carry 0
  and say so in their sentence.
- Suppression is counted, never silent. A new rule needs a name, a
  suppress-pair test, a near-miss test, and a counts entry.
- No drifted (Jaccard) findings here — exact and renamed only. Drifted
  belongs to the #6837 reducer.
- Never read `source_cache`; suppression reads path, name, language, and
  token count only.
- Convention-outlier findings name their cohort source (interface, router,
  package) and carry the cohort evidence on Finding.Outlier; confidence
  inherits the weakest majority CALLS edge and inferred majorities are
  labelled. Cohort tests live in cohort_test.go, selection and assembly in
  outlier_test.go.

## Verification

```bash
cd go && go test ./internal/query/codedivergence/ -count=1
```
