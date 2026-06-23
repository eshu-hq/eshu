package relationships

// confidence_calibration_test.go implements the statistical calibration gate for
// the DefaultConfidenceRegistry (issue #3510). The labeled golden corpus and the
// goldenCase/goldenLabel types live in confidence_calibration_corpus_test.go.
//
// # Calibration method
//
// The golden corpus carries a FIXED GoldenConfidence score per case, measured
// independently of DefaultConfidenceRegistry (see the corpus file header). The
// per-kind gate compares the live registry prior against the kind's golden
// POSITIVE optimum — the labeled confidence a clean match of that kind earns —
// not against a value derived from the registry. For each EvidenceKind:
//  1. Collect the fixed golden positive scores for that kind.
//  2. The golden optimum is their mean (all corpus positives for a kind are
//     identical literals today, so the mean is that literal).
//  3. Assert the REGISTRY prior is within ±0.05 of that golden optimum.
//
// Because the optimum is a fixed literal and the value under test is the live
// registry prior, a drift in confidence.go beyond tolerance FAILS the gate. This
// is the #3657 review fix: the earlier harness derived the golden positive score
// from the registry itself (positive = registryValue), so any drift moved the
// optimum along with the value and could never fail — it could only ever be
// logged as tier tension.
//
// A separate P/R sweep (sweepThresholds) over the fixed positive and negative
// scores still validates that an acceptance threshold can cleanly separate
// positives from negatives; see TestGoldenNegativeScoresBelowPositives and the
// sweep property tests.
//
// # Tier-ordering invariants
//
// Tier-ordering invariants from #3490 are preserved by the separate confidence.go
// invariant tests. The calibration gate here does not relax tolerance for tier
// tension: an out-of-tolerance prior is a real failure, not a logged note.
//
// # Reproducibility
//
// The fixed golden corpus is the sole input. Re-running on the same commit
// always produces the same output. Any failure names the kind, the registry
// prior, and the golden optimum precisely.

import (
	"math"
	"testing"
)

// calibrationResult is the per-kind calibration outcome.
type calibrationResult struct {
	Kind            EvidenceKind
	RegistryValue   float64
	GoldenOptimum   float64 // mean of the kind's fixed golden positive scores
	SweepThreshold  float64 // F1-optimal acceptance threshold over fixed pos/neg scores
	SweepF1         float64
	PositiveCount   int
	NegativeCount   int
	WithinTolerance bool // |registry - goldenOptimum| ≤ calibrationTolerance
}

// calibrationTolerance is the maximum allowed gap between a registered prior and
// the golden F1-optimal threshold for its kind. A gap above this fails the gate.
const calibrationTolerance = 0.05

// sweepThresholds computes precision, recall, and F1 across thresholds [lo, hi]
// stepping by step, and returns the *lowest* threshold with maximum F1 (ties
// resolved by preferring the lower threshold = better recall).
func sweepThresholds(positiveConf, negativeConf []float64, lo, hi, step float64) (bestThr, bestF1, bestP, bestR float64) {
	bestF1 = -1
	for thr := lo; thr <= hi+step/2; thr += step {
		thr = math.Round(thr/step) * step
		tp, fp, fn := 0, 0, 0
		for _, c := range positiveConf {
			if c >= thr {
				tp++
			} else {
				fn++
			}
		}
		for _, c := range negativeConf {
			if c >= thr {
				fp++
			}
		}
		var p, r, f1 float64
		if tp+fp > 0 {
			p = float64(tp) / float64(tp+fp)
		}
		if tp+fn > 0 {
			r = float64(tp) / float64(tp+fn)
		}
		if p+r > 0 {
			f1 = 2 * p * r / (p + r)
		}
		// Strictly greater: prefer lower threshold at ties (better recall).
		if f1 > bestF1 {
			bestF1 = f1
			bestThr = thr
			bestP = p
			bestR = r
		}
	}
	return bestThr, bestF1, bestP, bestR
}

// goldenScoresForKind returns the fixed golden positive and negative scores for a
// kind, read straight from the independent corpus (never from the registry).
// Ambiguous cases are excluded; they are checked separately by
// TestAmbiguousCasesRequireCorroboration.
func goldenScoresForKind(kind EvidenceKind) (pos, neg []float64) {
	for _, c := range goldenSet {
		if c.Kind != kind {
			continue
		}
		switch c.Label {
		case goldenPositive:
			pos = append(pos, c.GoldenConfidence)
		case goldenNegative:
			neg = append(neg, c.GoldenConfidence)
		}
	}
	return pos, neg
}

// calibrateKind compares the live registry prior for one EvidenceKind against
// the kind's golden positive optimum (the mean of its fixed golden positive
// scores) and runs the P/R sweep over the fixed scores for diagnostics. reg is
// the registry under test; passing a derived registry with a bad override lets a
// test prove the gate fails on drift.
func calibrateKind(reg *ConfidenceRegistry, kind EvidenceKind) calibrationResult {
	registryValue := reg.ConfidenceFor(kind)
	pos, neg := goldenScoresForKind(kind)

	if len(pos) == 0 {
		return calibrationResult{Kind: kind, RegistryValue: registryValue}
	}

	var sum float64
	for _, p := range pos {
		sum += p
	}
	goldenOptimum := sum / float64(len(pos))

	sweepThr, sweepF1, _, _ := sweepThresholds(pos, neg, 0.50, 0.99, 0.01)
	within := math.Abs(registryValue-goldenOptimum) <= calibrationTolerance

	return calibrationResult{
		Kind:            kind,
		RegistryValue:   registryValue,
		GoldenOptimum:   goldenOptimum,
		SweepThreshold:  sweepThr,
		SweepF1:         sweepF1,
		PositiveCount:   len(pos),
		NegativeCount:   len(neg),
		WithinTolerance: within,
	}
}

// TestGoldenSetCoverage asserts structural completeness: every registered
// EvidenceKind has at least two positive cases and one negative case. Fails
// early when a new kind is added to models.go without golden entries.
func TestGoldenSetCoverage(t *testing.T) {
	t.Parallel()

	posCount := make(map[EvidenceKind]int)
	negCount := make(map[EvidenceKind]int)
	for _, c := range goldenSet {
		switch c.Label {
		case goldenPositive:
			posCount[c.Kind]++
		case goldenNegative:
			negCount[c.Kind]++
		}
	}

	for _, kind := range allEvidenceKinds {
		if posCount[kind] < 2 {
			t.Errorf("kind %q has %d positive cases in golden set, want ≥2", kind, posCount[kind])
		}
		if negCount[kind] < 1 {
			t.Errorf("kind %q has %d negative cases in golden set, want ≥1", kind, negCount[kind])
		}
	}
}

// TestGoldenPositiveScoresAreIndependentOfRegistry guards the #3657 review fix:
// the golden positive score for each kind must be a fixed in-band probability,
// and every registered kind must have at least one positive literal in the
// corpus. The real protection is that the corpus file holds literals and
// TestBadOverrideFailsCalibrationGate proves drift fails; this test pins the
// structural shape so a regression to registry-derived scores is visible.
func TestGoldenPositiveScoresAreIndependentOfRegistry(t *testing.T) {
	t.Parallel()

	seen := make(map[EvidenceKind]bool)
	for _, c := range goldenSet {
		if c.Label != goldenPositive {
			continue
		}
		if c.GoldenConfidence <= 0 || c.GoldenConfidence > 1 {
			t.Errorf("kind %q case %q: golden positive score %.4f is not a probability in (0,1]",
				c.Kind, c.ID, c.GoldenConfidence)
		}
		seen[c.Kind] = true
	}
	for _, kind := range allEvidenceKinds {
		if !seen[kind] {
			t.Errorf("kind %q has no positive golden score literal in the corpus", kind)
		}
	}
}

// TestPerKindCalibrationMatchesRegistry is the deterministic calibration gate.
//
// For each EvidenceKind it sweeps the P/R curve over the FIXED golden scores and
// asserts the live registry prior is within ±0.05 of the F1-optimal threshold.
// A prior that drifts beyond tolerance FAILS — there is no tier-tension escape
// hatch, because the golden optimum is independent of the registry and so a real
// regression in confidence.go cannot move the target.
func TestPerKindCalibrationMatchesRegistry(t *testing.T) {
	t.Parallel()

	for _, kind := range allEvidenceKinds {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			res := calibrateKind(DefaultConfidenceRegistry, kind)
			if res.PositiveCount == 0 {
				t.Errorf("kind %q has no positive golden cases — add at least two", kind)
				return
			}
			if res.NegativeCount == 0 {
				t.Errorf("kind %q has no negative golden cases — add at least one", kind)
				return
			}

			if !res.WithinTolerance {
				t.Errorf(
					"kind %q registry=%.4f golden-optimum=%.4f (sweep-threshold=%.4f F1=%.4f pos=%d neg=%d): "+
						"registry prior is outside ±%.2f of the independent golden optimum; "+
						"a prior in confidence.go drifted — re-derive it from the golden corpus or fix the regression",
					kind, res.RegistryValue, res.GoldenOptimum,
					res.SweepThreshold, res.SweepF1,
					res.PositiveCount, res.NegativeCount, calibrationTolerance,
				)
				return
			}
			t.Logf("OK kind %q registry=%.4f golden-optimum=%.4f sweep-threshold=%.4f F1=%.4f",
				kind, res.RegistryValue, res.GoldenOptimum, res.SweepThreshold, res.SweepF1)
		})
	}
}

// TestBadOverrideFailsCalibrationGate proves the gate actually catches drift.
//
// It builds a derived registry that moves one prior far from its golden optimum
// (an injected regression) and asserts calibrateKind reports the kind as out of
// tolerance. Without the independent golden truth this could not be detected:
// the earlier self-referential harness derived the optimum from the same value
// it moved, so the gap stayed zero and the gate never fired.
func TestBadOverrideFailsCalibrationGate(t *testing.T) {
	t.Parallel()

	const kind = EvidenceKindTerraformAppRepo // golden optimum lies between pos 0.99 and neg 0.8712

	// Sanity: the unmodified registry must pass for this kind, so the failure
	// below is attributable to the injected override, not a pre-existing drift.
	base := calibrateKind(DefaultConfidenceRegistry, kind)
	if !base.WithinTolerance {
		t.Fatalf("precondition failed: kind %q is already out of tolerance on main "+
			"(registry=%.4f golden-optimum=%.4f); cannot prove the gate with this kind",
			kind, base.RegistryValue, base.GoldenOptimum)
	}

	// Inject a regression: drop the prior far below the golden optimum.
	const badPrior = 0.55
	derived, err := DefaultConfidenceRegistry.WithOverrides(map[EvidenceKind]float64{kind: badPrior})
	if err != nil {
		t.Fatalf("WithOverrides(%q=%.2f) returned error: %v", kind, badPrior, err)
	}

	got := calibrateKind(derived, kind)
	if got.RegistryValue != badPrior {
		t.Fatalf("override not applied: registry value for %q = %.4f, want %.4f",
			kind, got.RegistryValue, badPrior)
	}
	if got.WithinTolerance {
		t.Errorf(
			"calibration gate did not catch the injected regression: kind %q badPrior=%.4f "+
				"golden-optimum=%.4f |gap|=%.4f ≤ tolerance %.2f; the gate is self-referential and must be fixed",
			kind, got.RegistryValue, got.GoldenOptimum,
			math.Abs(got.RegistryValue-got.GoldenOptimum), calibrationTolerance,
		)
	}
}

// TestAmbiguousCasesRequireCorroboration verifies that for every ambiguous case
// in the golden set, the fixed degraded score (prior × 0.80) is below
// DefaultConfidenceThreshold — i.e., a single degraded-match fact must not
// resolve alone. This is the operational definition of "ambiguous":
// corroboration is required.
//
// Note: for high-confidence kinds where prior × 0.80 ≥ threshold, the golden set
// intentionally contains no ambiguous cases (see corpus construction comments).
func TestAmbiguousCasesRequireCorroboration(t *testing.T) {
	t.Parallel()

	for _, c := range goldenSet {
		if c.Label != goldenAmbiguous {
			continue
		}
		c := c
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()

			if c.GoldenConfidence >= DefaultConfidenceThreshold {
				t.Errorf(
					"ambiguous case %q (kind=%s): golden score %.4f ≥ threshold %.4f; "+
						"a single degraded match must require corroboration — lower the golden score or the prior",
					c.ID, c.Kind, c.GoldenConfidence, DefaultConfidenceThreshold,
				)
			}
		})
	}
}

// TestCalibrationSweepIsDeterministic verifies that two independent calls to
// sweepThresholds with the same inputs produce identical results. This is a
// property test for the calibration harness itself.
func TestCalibrationSweepIsDeterministic(t *testing.T) {
	t.Parallel()

	pos := []float64{0.90, 0.90, 0.90, 0.84}
	neg := []float64{0.79, 0.79, 0.74}

	thr1, f1a, p1, r1 := sweepThresholds(pos, neg, 0.50, 0.99, 0.01)
	thr2, f1b, p2, r2 := sweepThresholds(pos, neg, 0.50, 0.99, 0.01)

	if thr1 != thr2 || f1a != f1b || p1 != p2 || r1 != r2 {
		t.Errorf("sweep is non-deterministic: run1=(%.4f,%.4f,%.4f,%.4f) run2=(%.4f,%.4f,%.4f,%.4f)",
			thr1, f1a, p1, r1, thr2, f1b, p2, r2)
	}
}

// TestSweepPrefersLowerThresholdOnF1Tie verifies that when two thresholds
// achieve the same F1, sweepThresholds picks the lower one (better recall).
func TestSweepPrefersLowerThresholdOnF1Tie(t *testing.T) {
	t.Parallel()

	// All positives at 0.90; no negatives → all thresholds ≤ 0.90 get F1=1.0.
	// The lowest qualifying threshold is 0.50.
	pos := []float64{0.90, 0.90, 0.90}
	var neg []float64

	bestThr, bestF1, _, _ := sweepThresholds(pos, neg, 0.50, 0.99, 0.01)
	if bestF1 != 1.0 {
		t.Fatalf("expected F1=1.0 with no negatives, got %.4f", bestF1)
	}
	if bestThr != 0.50 {
		t.Errorf("expected lowest threshold 0.50 on F1 tie, got %.2f", bestThr)
	}
}

// TestGoldenNegativeScoresBelowPositives verifies that for every kind the fixed
// golden negative scores sit below the fixed golden positive scores. This
// property must hold for the P/R sweep to be mathematically sound: a negative
// case must never be harder to filter than a positive one at the same threshold.
func TestGoldenNegativeScoresBelowPositives(t *testing.T) {
	t.Parallel()

	for _, kind := range allEvidenceKinds {
		pos, neg := goldenScoresForKind(kind)
		if len(pos) == 0 || len(neg) == 0 {
			continue
		}
		minPos := pos[0]
		for _, p := range pos {
			if p < minPos {
				minPos = p
			}
		}
		maxNeg := neg[0]
		for _, n := range neg {
			if n > maxNeg {
				maxNeg = n
			}
		}
		if maxNeg >= minPos {
			t.Errorf("kind %q: max golden negative %.4f ≥ min golden positive %.4f; corpus scaling invariant broken",
				kind, maxNeg, minPos)
		}
	}
}
