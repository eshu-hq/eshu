# Accuracy Golden Gate

The accuracy golden gate is the continuous quality bar that fails CI on accuracy
regressions across three dimensions at once: per-language cyclomatic complexity
correctness, cross-repo call-edge precision/recall plus resolver coverage, and
correlation admission precision/recall. It is the fourth quality bar of the
capability regression audit (issue #3499, epic #3480).

## Why this gate exists

Eshu previously measured each accuracy dimension in separate, independently
asserted tests. There was no single gate that proved, on every change, that
complexity rankings stay meaningful, that cross-repo call edges keep resolving,
and that correlation admission stays truthful — and no published, tracked-over-
time metric an operator or reviewer could diff between runs.

The gate closes that gap. It measures **real** values from the shipped parser,
reducer, and admission-audit harnesses, then asserts each measured metric meets
or exceeds a published floor. It is intentionally:

- **Not a tautology.** Measurements come from the real harnesses, never from
  hand-written numbers that mirror the floor. A dedicated test
  (`TestAccuracyGoldenGateDetectsRegression`) proves a zeroed measurement fails.
- **Static-fixture.** The gate runs as a Go test with no live services, so it
  is part of the normal CI lane and a dedicated verify step.
- **Floor-based.** Thresholds are minimums. Accuracy may improve freely; a drop
  below the floor fails. Raising a floor after a measured improvement is a
  deliberate, reviewed commit.

## What each dimension measures

| Dimension | Measured by | Metric |
| --- | --- | --- |
| `complexity` | Each language's straight-line and branchy fixtures parsed through `parser.DefaultEngine`. | Coverage: count of languages whose straight-line function scores `1` and branchy function scores its hand-counted McCabe value. A language reverting to a constant drops from coverage. |
| `resolvers` | Caller→callee `CALLS` edges extracted by `reducer.ExtractCodeCallRows` and scored with `parser/goldenaudit.ScoreAccuracy`, plus the published [#3487 resolver coverage matrix](https://github.com/eshu-hq/eshu/blob/main/go/internal/reducer/README.md). | Precision/recall of observed vs golden edges, and the count of resolver-covered languages. |
| `correlation` | Admission decisions over the `correlation_admission_golden` suite run through the real `admissionaudit.Audit`. | Precision/recall of admitted (positive-class) decisions against fixture intent. |

## The published baseline

[`go/internal/accuracygate/testdata/baseline.json`](https://github.com/eshu-hq/eshu/blob/main/go/internal/accuracygate/testdata/baseline.json)
is the published accuracy contract and the floor the gate enforces. Every known
dimension must carry a threshold so a dropped dimension cannot silently disable
its gate; an unknown dimension key is a fixture error. Its git history is how
per-dimension metrics are tracked over time.

The gate also emits a deterministic per-dimension / per-label metrics snapshot
(`PublishedMetrics`) on every run, logged in CI, so a reviewer can diff two runs
and see exactly which language or evidence kind moved.

## Running the gate

```bash
scripts/verify_accuracy_golden_gate.sh
# or directly:
cd go && go test ./internal/accuracygate -count=1
```

The CI workflow runs `scripts/verify_accuracy_golden_gate.sh` as a dedicated
step (`.github/workflows/test.yml`).

## Changing a floor

Lowering a floor to make a regression pass is not allowed — fix the regressed
dimension instead. Raising a floor is encouraged after a real, measured
improvement: update `baseline.json` and the per-language fixtures in the same
commit so the new guarantee is reviewed and recorded.

## Related references

- [Local Testing](local-testing.md) — verification matrix entry for this gate.
- [Cypher Performance](cypher-performance.md) — adjacent hot-path accuracy and
  performance contracts.

## Performance Evidence

- No-Regression Evidence: #3499 adds the `accuracygate` package plus a CI step
  that scores already-shipped measurement harnesses (parser complexity, reducer
  `ExtractCodeCallRows` resolvers, `admissionaudit` correlation) against a
  checked-in baseline. Baseline: current reducer, parser, and admission runtime
  behavior. After: byte-identical runtime behavior — no reducer domain, Cypher,
  graph write, worker, lease, batch size, or concurrency knob changes; the gate
  only reads existing output at test time. Backend/version: NornicDB and Neo4j
  compatibility paths unchanged. Input shape: golden fixtures under the package
  `testdata`. Terminal queue and row counts: unaffected — no queue or projection
  path is touched. Safe because it adds an observational test gate, not a runtime
  code path.
- No-Observability-Change: no new or changed spans, metrics, metric labels,
  logs, or status fields on any runtime path; the deterministic
  `PublishedMetrics` snapshot is emitted only during the gate test run.
