# Correlation Confidence Calibration

This page documents how Eshu derives the per-signal correlation confidence
priors and the acceptance threshold from a labeled golden set, rather than by
hand. It is the provenance record for issue #3510 (follow-up to #3490, epic
#3480).

## Why calibration exists

Issue #3490 centralized the previously-scattered correlation confidence
literals into two documented, overridable registries and pinned their
tier-ordering invariants with tests:

- `DefaultConfidenceRegistry` in `go/internal/relationships/confidence.go` —
  per-`EvidenceKind` priors for cross-repository relationship evidence.
- `DefaultWorkloadSignalConfidence` in
  `go/internal/reducer/workload_signal_confidence.go` —
  per-`WorkloadSignalKind` priors for workload admission.

After #3490 the absolute numbers were centralized and overridable but still
hand-tuned. Issue #3510 replaces hand-tuning with a deterministic, reproducible
calibration gate: a labeled golden set with **fixed** per-case scores provides an
independent calibration optimum per signal, and a test asserts the registered
priors are within tolerance of that golden truth.

## What is calibrated

| Surface | Registry | Calibration harness |
|---|---|---|
| Relationship evidence priors | `DefaultConfidenceRegistry` | `go/internal/relationships/confidence_calibration_test.go` + `confidence_calibration_corpus_test.go` |
| Workload admission priors | `DefaultWorkloadSignalConfidence` | `go/internal/reducer/workload_signal_calibration_test.go` |

## The golden set

The golden set is a synthetic, labeled corpus defined in the calibration test
files. It uses only structural/synthetic identifiers — no proprietary or
organization-specific data — so it is safe to ship and fully reproducible.

Each case carries:

- the signal under test (`EvidenceKind` or `WorkloadSignalKind`),
- a ground-truth label,
- a **fixed `GoldenConfidence` score** — a literal, measured independently of
  the registry under test (see "Independent golden truth" below), and
- a human-readable rationale documenting why the label applies.

Labels:

- **positive** — a clean match that genuinely defines the
  relationship/workload; scored at the labeled full-match confidence.
- **negative** — a false positive (name collision, partial path match, wildcard
  principal, third-party image, CI-only repo); scored at a marginal degraded
  confidence (the labeled positive × 0.88).
- **ambiguous** — a degraded partial-match confidence (labeled positive × 0.80);
  a single such fact must not resolve alone and requires corroboration.

### Independent golden truth

The `GoldenConfidence` literals are the cornerstone of the gate. They are
**fixed** values captured when the #3490 priors were found
calibration-consistent, and they do **not** move when a registry prior changes.
The gate compares the live registry prior against the kind's golden positive
optimum (the mean of its fixed positive literals). Because the truth is fixed and
the value under test is the live prior, a prior that drifts beyond tolerance
**fails** the gate.

This is the #3657 review fix. The earlier harness derived every case score from
the registry (`positive = registryValue`, `negative = registryValue × 0.88`), so
any drift moved the calibration optimum along with the value under test; drift
could only ever be logged as tier tension and never failed.
`TestBadOverrideFailsCalibrationGate` and
`TestWorkloadBadOverrideFailsCalibrationGate` inject a bad prior via
`WithOverrides(...)` and assert the gate reports it out of tolerance, proving the
gate is no longer self-referential.

Coverage rules enforced by tests:

- Every registered signal has at least two positive cases and one negative case
  (`TestGoldenSetCoverage`, `TestWorkloadGoldenSetCoverage`). Adding a new
  `EvidenceKind` or `WorkloadSignalKind` without golden entries fails CI.

## The calibration model

Each golden case has a **fixed** `GoldenConfidence` literal, frozen at the
priors measured when the #3490 values were found calibration-consistent. Per
kind the literals follow the band model below, but they are stored as literals,
not recomputed from the registry:

- positive score = labeled full-match confidence (the kind's clean-match prior)
- negative score = labeled positive × 0.88 (a marginal false positive)
- ambiguous score = labeled positive × 0.80 (a degraded partial match)

`TestGoldenNegativeScoresBelowPositives` and
`TestWorkloadGoldenNegativeScoresBelowPositives` assert the negative literals
stay strictly below the positive literals per kind, which the precision/recall
sweep requires to be mathematically sound.

## The per-kind gate

For each signal the harness computes the kind's **golden positive optimum** —
the mean of its fixed positive literals — and asserts the live registry prior is
within ±0.05 of it. The optimum is a fixed value independent of the registry, so
a prior drift in `confidence.go` or `workload_signal_confidence.go` beyond
tolerance fails the gate. The failure names the kind, the registry prior, and the
golden optimum.

`TestPerKindCalibrationMatchesRegistry` and
`TestWorkloadPerKindCalibrationMatchesRegistry` are these gates. There is **no
tier-tension escape hatch**: an out-of-tolerance prior is a real failure, not a
logged note. The #3490 tier-ordering invariants are enforced separately (see
below) and are unaffected.

## The precision/recall sweep

The harness also runs a precision/recall sweep over the fixed positive and
negative scores, retained as a diagnostic that the scores are separable:

1. Sweeps candidate thresholds across the signal's range in 0.01 steps.
2. At each threshold counts true positives, false positives, and false
   negatives over the golden cases.
3. Computes precision = TP / (TP + FP) and recall = TP / (TP + FN).
4. Computes F1 = 2 · P · R / (P + R).
5. Selects the **lowest** threshold that maximizes F1 (ties resolved in favor of
   recall). `TestSweepPrefersLowerThresholdOnF1Tie` pins this tie-break rule.

The sweep is pure and deterministic: `TestCalibrationSweepIsDeterministic`
asserts two runs on identical inputs produce identical output. The sweep result
is reported in the per-kind diagnostic but the gate decision is the prior-vs-
golden-optimum comparison above.

## Proving the gate catches drift

Because the golden truth is independent of the registry, the gate can be proven
non-vacuous. `TestBadOverrideFailsCalibrationGate` (relationships) and
`TestWorkloadBadOverrideFailsCalibrationGate` (workload) build a derived registry
with `WithOverrides(...)` that moves one prior far below its golden optimum and
assert the gate reports the kind out of tolerance. If the gate ever regressed to
deriving the optimum from the registry, these tests would fail.

The calibration is applied through #3490's existing `WithOverrides(...)` hook
when an operator wishes to recalibrate at runtime; the default registry values
are the calibrated baseline asserted by these tests.

## Acceptance threshold

`DefaultConfidenceThreshold = 0.75` is a deliberate **policy floor**: it sits
below the lowest registered prior (`GCPCloudRelationship = 0.82`) so any single
registered fact that cleanly matches always resolves. It is not derived from the
golden sweep — raising it to a pooled F1-optimal value (~0.84) would prevent the
weakest-prior registered kinds from resolving on a single clean match, breaking
the design contract that every registered kind resolves alone. The floor is a
documented policy choice, not a calibration output.

## Tier-ordering invariants are preserved

The #3490 tier-ordering invariants are enforced independently of calibration and
stay green:

- `TestConfidenceTierMonotonicity`,
  `TestConfidenceRegistryEntriesRespectTierFloors`,
  `TestWorkloadSignalTierFloorsMonotonic`, and
  `TestWorkloadSignalRuntimeSignalsOutrankCISignals`.

The calibration gate does not relax its tolerance for tier boundaries. Because
the golden positives equal the calibration-consistent #3490 priors, the priors
satisfy both the tier floors and the ±0.05 golden tolerance simultaneously; a
future edit that breaks either is caught by the corresponding test.

## Reproducing the calibration

The golden set is the sole input. Re-running the calibration on the same commit
always produces the same output. To regenerate the suggested values and inspect
the precision/recall basis:

```bash
cd go && go test ./internal/relationships ./internal/reducer \
  -run 'Calibrat|GoldenSet|Ambiguous|Sweep|Golden' -v -count=1
```

The verbose output prints, per signal, the registry value, the golden positive
optimum, and the sweep threshold/F1 diagnostic. Any out-of-tolerance drift fails
the run and names the value to update in `confidence.go` or
`workload_signal_confidence.go`.

## Provenance summary

- Method: per-signal comparison of the registered prior against a fixed golden
  positive optimum (mean of independent labeled literals), ±0.05 tolerance; a
  precision/recall sweep over the fixed scores is retained as a separability
  diagnostic.
- Inputs: synthetic golden cases with fixed `GoldenConfidence` literals
  (public/synthetic only) defined in the calibration test files.
- Reproducibility: deterministic test harness; no external corpus, no network,
  no randomness.
- Independence: the golden truth is fixed and does not move with the registry;
  `TestBadOverrideFailsCalibrationGate` and
  `TestWorkloadBadOverrideFailsCalibrationGate` prove the gate fails on drift.
- Invariants: #3490 tier-ordering invariants enforced independently and stay
  green.
