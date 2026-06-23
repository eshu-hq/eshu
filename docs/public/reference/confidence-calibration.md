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
calibration procedure: a labeled golden set drives a precision/recall sweep that
derives the F1-optimal threshold per signal, and a test asserts the registered
priors are coherent with that golden set.

## What is calibrated

| Surface | Registry | Calibration harness |
|---|---|---|
| Relationship evidence priors | `DefaultConfidenceRegistry` | `go/internal/relationships/confidence_calibration_test.go` |
| Workload admission priors | `DefaultWorkloadSignalConfidence` | `go/internal/reducer/workload_signal_calibration_test.go` |
| Acceptance threshold | `DefaultConfidenceThreshold` (`resolver.go`) | `TestCalibrateAcceptanceThreshold` |

## The golden set

The golden set is a synthetic, labeled corpus defined in the calibration test
files. It uses only structural/synthetic identifiers — no proprietary or
organization-specific data — so it is safe to ship and fully reproducible.

Each case carries:

- the signal under test (`EvidenceKind` or `WorkloadSignalKind`),
- a ground-truth label, and
- a human-readable rationale documenting why the label applies.

Labels:

- **positive** — the extractor emits the full registered prior and the
  relationship/workload is real.
- **negative** — the extractor runs on a false positive (name collision,
  partial path match, wildcard principal, third-party image, CI-only repo) and
  emits a degraded confidence.
- **ambiguous** — the extractor emits a degraded partial-match confidence; a
  single such fact must not resolve alone and requires corroboration.

Coverage rules enforced by tests:

- Every registered signal has at least two positive cases and one negative case
  (`TestGoldenSetCoverage`, `TestWorkloadGoldenSetCoverage`). Adding a new
  `EvidenceKind` or `WorkloadSignalKind` without golden entries fails CI.

## The calibration model

Confidence values for the sweep are derived from the registered prior so the
sweep is always relative to the registry, never a hard-coded literal:

- positive confidence = registered prior (a clean, full match)
- negative confidence = registered prior × 0.88 (a marginal false positive)
- ambiguous confidence = registered prior × 0.80 (a degraded partial match)

The 0.88 and 0.80 scaling factors model the gap between a clean match, a
marginal false positive, and a degraded partial match. `TestCalibrationNegativeScalingIsConsistent`
asserts the negative scaling always keeps negative confidence strictly below
positive confidence, which is required for the sweep to be mathematically sound.

## The precision/recall sweep

For each signal the harness:

1. Sweeps candidate thresholds across the signal's range in 0.01 steps.
2. At each threshold counts true positives, false positives, and false
   negatives over the golden cases.
3. Computes precision = TP / (TP + FP) and recall = TP / (TP + FN).
4. Computes F1 = 2 · P · R / (P + R).
5. Selects the **lowest** threshold that maximizes F1 (ties resolved in favor of
   recall). `TestSweepPrefersLowerThresholdOnF1Tie` pins this tie-break rule.

The sweep is pure and deterministic: `TestCalibrationSweepIsDeterministic`
asserts two runs on identical inputs produce identical output.

## Asserting the registry matches the golden set

`TestPerKindCalibrationMatchesRegistry` and
`TestWorkloadPerKindCalibrationMatchesRegistry` are the calibration gates. For
each signal they assert the registered prior is within ±0.05 of the F1-optimal
threshold derived from that signal's golden cases. Any drift fails the test and
names the new optimal value precisely, so re-deriving the registry is a matter
of running the test and reading the diagnostic.

The calibration is applied through #3490's existing `WithOverrides(...)` hook
when an operator wishes to recalibrate at runtime; the default registry values
are the calibrated baseline asserted by these tests.

## Acceptance threshold calibration

`TestCalibrateAcceptanceThreshold` pools all positive and negative cases across
every signal and runs a single sweep to find the F1-optimal acceptance
threshold. The current `DefaultConfidenceThreshold = 0.75` is a deliberate
**policy floor**: it is set below the lowest registered prior
(`GCPCloudRelationship = 0.82`) so that any single registered fact that cleanly
matches always resolves.

The pooled sweep finds an F1-optimal value (~0.84) above the lowest registered
prior. Raising the threshold to that value would prevent the weakest-prior
registered kinds from resolving on a single clean match, breaking the design
contract that every registered kind resolves alone. This is the documented
**policy tension**: the test asserts the threshold is never *below* the
calibrated value (which would be too aggressive) and logs the tension when the
calibrated value sits above the lowest prior. To resolve the tension in future,
raise the lowest registered prior above the sweep-optimal value, or accept the
conservative floor.

## Tier-ordering invariants are preserved

The #3490 tier-ordering invariants are not weakened by calibration:

- `TestConfidenceTierMonotonicity`,
  `TestConfidenceRegistryEntriesRespectTierFloors`,
  `TestWorkloadSignalTierFloorsMonotonic`, and
  `TestWorkloadSignalRuntimeSignalsOutrankCISignals` all stay green.

When a calibrated suggestion would cross a tier floor — or land within the same
tier as the registry value but more than 0.05 away — the calibration test
records the **tier tension**, logs it for transparency, and keeps the registry
value clamped to preserve the tier ordering. Per the issue's guidance, the
invariant wins and the tension is documented rather than silently broken.

## Reproducing the calibration

The golden set is the sole input. Re-running the calibration on the same commit
always produces the same output. To regenerate the suggested values and inspect
the precision/recall basis:

```bash
cd go && go test ./internal/relationships ./internal/reducer \
  -run 'Calibrat|GoldenSet|Ambiguous|Sweep' -v -count=1
```

The verbose output prints, per signal, the registry value, the calibrated
suggestion, and the F1/precision/recall at the calibrated threshold. Any
out-of-tolerance drift fails the run and names the value to update in
`confidence.go` or `workload_signal_confidence.go`.

## Provenance summary

- Method: per-signal F1-maximizing precision/recall sweep over a synthetic
  labeled golden set, threshold derived from the curve, ±0.05 tolerance against
  the registered prior.
- Inputs: synthetic golden cases (public/synthetic only) defined in the
  calibration test files.
- Reproducibility: deterministic test harness; no external corpus, no network,
  no randomness.
- Invariants: #3490 tier-ordering invariants preserved; tier tensions documented
  and clamped rather than broken.
