# internal/proofofvalue

`proofofvalue` is the scoring and aggregation library behind Eshu's
proof-of-value harness (issue #3497). It answers one question with real
numbers: **does an agent answer better with Eshu than without it?**

## What it does

The package is a pure library. It takes:

- a **question set** with ground-truth labels derived from a fixture corpus, and
- **predictions** from two strategies — a baseline (plain text search / grep)
  and Eshu (the real reachability analyzer) —

and produces an honest `Report`: per-strategy accuracy, dead-artifact
precision/recall, false-positive and false-negative counts, a per-question
audit trail, and the with-minus-without `Delta`.

It computes **no answers itself**. Callers supply predictions produced by real
tools over a real corpus, so the reported delta is reproducible and cannot be
fabricated by this package.

## Honesty boundary

- Misses are reported, not hidden: `DeadFalseNegative` and `DeadFalsePositive`
  are first-class fields.
- The scorer has no bias toward Eshu. If Eshu answers worse, `AccuracyDelta`
  goes negative (see `TestScoreDoesNotInflateWhenEshuIsWrong`).
- Malformed input (missing predictions, unknown labels, duplicates) fails loudly
  rather than silently scoring as a miss.

## Pieces

| File | Responsibility |
| --- | --- |
| `score.go` | Types and `Score`: confusion counts, ratios, delta. |
| `validate.go` | Input validation and prediction indexing. |
| `baseline.go` | `BaselineReachability`: the faithful grep-only verdict model. |
| `harness.go` | `BuildRun`: derive questions + both strategies from a corpus. |

## Corpus

The default corpus is the dead-IaC product-truth fixture
(`tests/fixtures/product_truth/dead_iac`) with ground truth in
`tests/fixtures/product_truth/expected/dead_iac.json`. The Eshu strategy calls
`internal/iacreachability.Analyze` over the same files the baseline searches.

## Run it

```bash
cd go && go run ./cmd/proof-of-value --out /tmp/proof-of-value.json
cd go && go test ./internal/proofofvalue -count=1
```

See `go/cmd/proof-of-value/README.md` for the runner and how to read results.
