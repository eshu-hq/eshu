# Accuracy Golden Gate

## Purpose

`internal/accuracygate` is the continuous accuracy gate for epic #3480's
fourth quality bar (issue #3499). It rolls three already-real measurement
surfaces into one CI gate that fails on accuracy regressions and publishes
per-language / per-evidence-kind metrics that are tracked over time in git:

1. **Complexity** — per-language cyclomatic complexity correctness (issues
   #3488 / #3524). Measured: straight-line functions score `1` and branchy
   functions score the hand-counted McCabe value, for every language that ships
   real complexity. A scored language regressing to a constant fails the gate.
2. **Resolvers** — cross-repo call-edge precision/recall plus resolver language
   coverage (issue #3487). Measured: the `resolutionparity` source-derived
   call-graph observation path scored with `parser/goldenaudit.ScoreAccuracy`,
   and a MEASURED covered-language count — each documented resolver language is
   exercised with a per-language fixture and counted only when its resolver
   actually produces the expected CALLS edge, so removing a resolver drops the
   count below the floor.
3. **Correlation** — admission-decision precision/recall against the golden
   correlation suite (`admissionaudit`, relates to #3490). Measured: the
   observed admission state for each golden case is DERIVED by running the real
   `correlation/admission.Evaluate` classifier plus a generation-freshness
   comparison over a per-case input, then audited against the golden expectation
   with `admissionaudit.Audit`. The observation is production behavior, not a
   copy of the expected state, so a flipped admission decision or dropped graph
   fact fails the gate.

## Ownership boundary

This package owns only aggregation, scoring rollup, the baseline contract, the
threshold gate, and the published metrics snapshot. It does not parse source,
resolve calls, run the reducer, or query storage. The gate's test imports the
parser, the reducer call-row extraction, `resolutionparity`, the
`correlation/admission` classifier, and `admissionaudit`, drives real production
logic over golden inputs, and feeds the observed results in — so the gate
measures shipped behavior rather than re-asserting hand-written numbers (no
tautology). The correlation and resolver-coverage measurements specifically
observe production output, not the golden expectation, and each has a regression
proof (`TestAccuracyGoldenGateDetectsCorrelationRegression`,
`TestAccuracyGoldenGateDetectsResolverCoverageRegression`).

## Exported surface

See `doc.go` for the godoc contract.

- `Dimension`, `Metric`, `Threshold`, `Baseline`, `Measurement` describe the
  contract.
- `LoadBaseline` reads and validates the checked-in `testdata/baseline.json`.
- `Evaluate` scores a `Measurement` against a `Baseline` into a `GateResult`
  whose `Pass`, `Summary`, and `FailureMessage` drive the test and CI.
- `ScorePredictions` builds a precision/recall `Metric` from a labeled confusion
  matrix; `CoverageMetric` builds one from a scored-item count.
- `Publish` / `PublishedMetrics.Encode` render the deterministic published
  metrics snapshot.

## The baseline is the published, tracked contract

`testdata/baseline.json` is the source of truth a reviewer reads to see what
accuracy the repo guarantees, and the floor the gate enforces. Thresholds are
**minimums**: accuracy may improve freely, but a drop below the published floor,
a scored language regressing, or a missing measurement fails the gate. Raising a
floor after a real improvement is a deliberate, reviewed commit — that git
history is how metrics are tracked over time.

## Running the gate

```bash
cd go && go test ./internal/accuracygate -count=1
# or the CI wrapper:
scripts/verify_accuracy_golden_gate.sh
```

The gate runs as a static-fixture Go test (no live services), so it is part of
the normal `go test ./...` CI lane and the dedicated verify step.

## Telemetry

This package emits no metrics, spans, or logs. It is a test and verification
helper; production parse, resolve, and admission timing remain owned by their
respective packages.

## Gotchas / invariants

- Measurements must come from the real harnesses, never from hand-written
  numbers that mirror the baseline. A tautological gate proves nothing.
- A gated dimension with no measurement fails (a deleted measurement cannot
  silently disable a floor).
- Output is deterministic: dimensions render in a fixed order and JSON marshals
  label keys sorted.

## Related docs

- `go/internal/parser/goldenaudit/README.md` — edge precision/recall scorer
- `go/internal/admissionaudit/README.md` — correlation admission audit
- `go/internal/resolutionparity/` — source-derived call-graph observation
- `docs/public/reference/local-testing.md` — verification gates
