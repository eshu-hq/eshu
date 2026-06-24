package searchbench

import (
	"fmt"
	"time"
)

// GateProfile distinguishes the two admission paths for hybrid retrieval. The
// deterministic local hash embedder and a governed production provider carry
// different thresholds and different best-case outcomes.
type GateProfile string

const (
	// GateProfileLocalDeterministic is the no-network hash-embedder proof path.
	// It can meet a modest local bar but is, by policy, never production-ready on
	// its own.
	GateProfileLocalDeterministic GateProfile = "local_deterministic"
	// GateProfileProductionProvider is a governed embedding/provider path that can
	// reach production-ready when it meets the production bar.
	GateProfileProductionProvider GateProfile = "production_provider"
)

// GateDecision is the admission outcome for one measured retrieval run.
type GateDecision string

const (
	// GateProductionReady means a production-provider run met every threshold.
	GateProductionReady GateDecision = "production_ready"
	// GateLocalProofPassed means a local-deterministic run met the local bar but
	// is explicitly not production-ready on its own.
	GateLocalProofPassed GateDecision = "local_proof_passed"
	// GateDegraded means the vector path did not participate or vector coverage
	// was too low, so the run is keyword-degraded and not evaluable as semantic.
	GateDegraded GateDecision = "degraded"
	// GateRejected means the run was evaluable but failed a measured threshold or
	// emitted a false canonical claim.
	GateRejected GateDecision = "rejected"
)

// ProductionGateThresholds is the admission bar for one gate profile. Recall,
// Precision, and NDCG are minima in [0,1]; MaxP95 is the query p95 latency
// budget; MinVectorCoverage is the minimum fraction of the in-scope corpus that
// must carry a compatible vector before semantic retrieval is evaluable.
type ProductionGateThresholds struct {
	MinRecall         float64       `json:"min_recall"`
	MinPrecision      float64       `json:"min_precision"`
	MinNDCG           float64       `json:"min_ndcg"`
	MaxP95            time.Duration `json:"max_p95_ns"`
	MinVectorCoverage float64       `json:"min_vector_coverage"`
}

// ProductionGateThresholdsFor returns the published thresholds for a profile.
// These numbers are the source of truth quoted by the hybrid-retrieval
// production gate doc; TestProductionGateThresholdsAreDocumented pins them.
func ProductionGateThresholdsFor(profile GateProfile) ProductionGateThresholds {
	switch profile {
	case GateProfileProductionProvider:
		return ProductionGateThresholds{
			MinRecall:         0.80,
			MinPrecision:      0.70,
			MinNDCG:           0.80,
			MaxP95:            150 * time.Millisecond,
			MinVectorCoverage: 0.98,
		}
	default: // GateProfileLocalDeterministic
		return ProductionGateThresholds{
			MinRecall:         0.60,
			MinPrecision:      0.50,
			MinNDCG:           0.60,
			MaxP95:            50 * time.Millisecond,
			MinVectorCoverage: 0.95,
		}
	}
}

// EvaluateProductionGate classifies one measured retrieval run against a
// profile's thresholds and returns the decision plus the unmet thresholds. The
// order is deliberate:
//
//   - A false canonical claim rejects the run unconditionally, even when it
//     would otherwise be degraded: retrieval evidence must never be promoted to
//     canonical truth, and that signal must never be masked.
//   - A run that never exercised the vector path, or whose vector coverage is too
//     low, is degraded (keyword fallback) before any accuracy or latency
//     threshold is judged, because semantic quality is not evaluable without
//     vectors.
//
// The gate sees only the run's measured metrics, search flags, and the supplied
// vector coverage. It does not receive retrieval_state or failure classes, so
// the caller is responsible for translating stale, partial, or building index
// states into a reduced vectorCoverage before calling the gate.
func EvaluateProductionGate(
	profile GateProfile,
	run BackendRun,
	vectorCoverage float64,
) (GateDecision, []string) {
	if !knownGateProfile(profile) {
		return GateRejected, []string{fmt.Sprintf("unknown gate profile %q", profile)}
	}
	thresholds := ProductionGateThresholdsFor(profile)

	// The false-canonical-claim measurement is the gate's truth-safety stop, so a
	// missing measurement rejects rather than silently passing, and a positive
	// count rejects unconditionally even on an otherwise-degraded run.
	if run.Metrics.FalseCanonicalClaimCount == nil {
		return GateRejected, []string{"false canonical claim count is required"}
	}
	if claims := *run.Metrics.FalseCanonicalClaimCount; claims > 0 {
		return GateRejected, []string{fmt.Sprintf("false canonical claim count %d must be 0", claims)}
	}
	if !vectorPathParticipated(run) {
		return GateDegraded, []string{"vector path did not participate; run is keyword-degraded"}
	}
	if vectorCoverage < thresholds.MinVectorCoverage {
		return GateDegraded, []string{fmt.Sprintf(
			"vector coverage %.3f below minimum %.3f", vectorCoverage, thresholds.MinVectorCoverage,
		)}
	}

	var reasons []string
	if run.Metrics.Recall < thresholds.MinRecall {
		reasons = append(reasons, fmt.Sprintf("recall %.3f below minimum %.3f", run.Metrics.Recall, thresholds.MinRecall))
	}
	if run.Metrics.Precision < thresholds.MinPrecision {
		reasons = append(reasons, fmt.Sprintf("precision %.3f below minimum %.3f", run.Metrics.Precision, thresholds.MinPrecision))
	}
	if run.Metrics.NDCG < thresholds.MinNDCG {
		reasons = append(reasons, fmt.Sprintf("ndcg %.3f below minimum %.3f", run.Metrics.NDCG, thresholds.MinNDCG))
	}
	if run.Latency.P95 > thresholds.MaxP95 {
		reasons = append(reasons, fmt.Sprintf("p95 %s above budget %s", run.Latency.P95, thresholds.MaxP95))
	}
	if len(reasons) > 0 {
		return GateRejected, reasons
	}

	if profile == GateProfileProductionProvider {
		return GateProductionReady, nil
	}
	// A passing local-deterministic run is a proof, not production readiness.
	return GateLocalProofPassed, nil
}

// knownGateProfile reports whether profile is one of the two admitted gate
// profiles. An unknown or zero-value profile is rejected before evaluation so a
// typo or unset config can never be admitted as production quality on the
// lenient local thresholds.
func knownGateProfile(profile GateProfile) bool {
	switch profile {
	case GateProfileLocalDeterministic, GateProfileProductionProvider:
		return true
	default:
		return false
	}
}

// vectorPathParticipated reports whether the run actually exercised vector
// retrieval. Without a vector path the run is keyword-only and cannot be judged
// as semantic retrieval.
func vectorPathParticipated(run BackendRun) bool {
	if run.SearchFlags == nil {
		return false
	}
	return run.SearchFlags.VectorEnabled && run.SearchFlags.EmbeddingEnabled
}
