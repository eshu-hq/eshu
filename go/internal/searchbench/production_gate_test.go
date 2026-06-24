// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"testing"
	"time"
)

func intPtr(v int) *int { return &v }

func vectorRun(recall, precision, ndcg float64, p95 time.Duration, claims int) BackendRun {
	return BackendRun{
		Backend:     BackendNornicDBHybrid,
		Mode:        ModeHybrid,
		QueryCount:  20,
		Latency:     LatencySummary{P95: p95},
		SearchFlags: &NornicDBSearchFlags{BM25Enabled: true, VectorEnabled: true, EmbeddingEnabled: true},
		Metrics: RetrievalMetrics{
			Recall:                   recall,
			Precision:                precision,
			NDCG:                     ndcg,
			FalseCanonicalClaimCount: intPtr(claims),
		},
	}
}

func TestProductionGateProductionProviderReady(t *testing.T) {
	run := vectorRun(0.85, 0.75, 0.82, 120*time.Millisecond, 0)
	decision, reasons := EvaluateProductionGate(GateProfileProductionProvider, run, 0.99)
	if decision != GateProductionReady {
		t.Fatalf("decision = %q, reasons=%v; want production_ready", decision, reasons)
	}
	if len(reasons) != 0 {
		t.Fatalf("production-ready run must have no unmet thresholds, got %v", reasons)
	}
}

func TestProductionGateLocalDeterministicNeverProductionReady(t *testing.T) {
	// A local hash-embedder run can meet the local bar but is, by policy, never
	// production-ready on its own.
	run := vectorRun(0.95, 0.90, 0.95, 5*time.Millisecond, 0)
	decision, reasons := EvaluateProductionGate(GateProfileLocalDeterministic, run, 1.0)
	if decision != GateLocalProofPassed {
		t.Fatalf("decision = %q, reasons=%v; want local_proof_passed", decision, reasons)
	}
}

func TestProductionGateRejectsLowRecall(t *testing.T) {
	run := vectorRun(0.40, 0.75, 0.82, 120*time.Millisecond, 0)
	decision, reasons := EvaluateProductionGate(GateProfileProductionProvider, run, 0.99)
	if decision != GateRejected {
		t.Fatalf("decision = %q; want rejected for low recall", decision)
	}
	if len(reasons) == 0 {
		t.Fatal("rejected decision must explain the unmet threshold")
	}
}

func TestProductionGateRejectsHighP95(t *testing.T) {
	run := vectorRun(0.85, 0.75, 0.82, 900*time.Millisecond, 0)
	decision, _ := EvaluateProductionGate(GateProfileProductionProvider, run, 0.99)
	if decision != GateRejected {
		t.Fatalf("decision = %q; want rejected for high p95", decision)
	}
}

func TestProductionGateRejectsTruthViolation(t *testing.T) {
	run := vectorRun(0.85, 0.75, 0.82, 120*time.Millisecond, 3)
	decision, reasons := EvaluateProductionGate(GateProfileProductionProvider, run, 0.99)
	if decision != GateRejected {
		t.Fatalf("decision = %q, reasons=%v; want rejected for false canonical claims", decision, reasons)
	}
}

func TestProductionGateDegradedWhenNoVectorPath(t *testing.T) {
	// Keyword-only fallback: the vector path did not participate, so the run is
	// degraded (not evaluable as semantic), not rejected.
	run := vectorRun(0.85, 0.75, 0.82, 120*time.Millisecond, 0)
	run.SearchFlags.VectorEnabled = false
	decision, reasons := EvaluateProductionGate(GateProfileProductionProvider, run, 0.99)
	if decision != GateDegraded {
		t.Fatalf("decision = %q, reasons=%v; want degraded for missing vector path", decision, reasons)
	}
}

func TestProductionGateDegradedWhenVectorCoverageLow(t *testing.T) {
	run := vectorRun(0.85, 0.75, 0.82, 120*time.Millisecond, 0)
	decision, _ := EvaluateProductionGate(GateProfileProductionProvider, run, 0.50)
	if decision != GateDegraded {
		t.Fatalf("decision = %q; want degraded for low vector coverage", decision)
	}
}

func TestProductionGateRejectsTruthViolationEvenWhenDegraded(t *testing.T) {
	// A truth violation is unconditional: it must reject even a run that would
	// otherwise be degraded (no vector path), never masking the signal.
	run := vectorRun(0.85, 0.75, 0.82, 120*time.Millisecond, 2)
	run.SearchFlags.VectorEnabled = false
	decision, reasons := EvaluateProductionGate(GateProfileProductionProvider, run, 0.10)
	if decision != GateRejected {
		t.Fatalf("decision = %q, reasons=%v; want rejected for false canonical claim even when degraded", decision, reasons)
	}
}

func TestProductionGateDegradedWhenEmbeddingDisabled(t *testing.T) {
	// VectorEnabled without EmbeddingEnabled is not a real vector path.
	run := vectorRun(0.85, 0.75, 0.82, 120*time.Millisecond, 0)
	run.SearchFlags.EmbeddingEnabled = false
	decision, _ := EvaluateProductionGate(GateProfileProductionProvider, run, 0.99)
	if decision != GateDegraded {
		t.Fatalf("decision = %q; want degraded when embedding is disabled", decision)
	}
}

func TestProductionGateRejectsUnknownProfile(t *testing.T) {
	// An unknown or zero-value profile must reject, never be admitted as
	// production quality on the lenient local thresholds.
	run := vectorRun(0.99, 0.99, 0.99, 1*time.Millisecond, 0)
	for _, profile := range []GateProfile{"", "typo_profile"} {
		decision, reasons := EvaluateProductionGate(profile, run, 1.0)
		if decision != GateRejected {
			t.Fatalf("profile %q decision = %q, reasons=%v; want rejected", profile, decision, reasons)
		}
	}
}

func TestProductionGateRejectsMissingFalseCanonicalMeasurement(t *testing.T) {
	// A run assembled without truth-claim scoring must reject: the missing
	// safety measurement cannot pass as production-ready.
	run := vectorRun(0.85, 0.75, 0.82, 120*time.Millisecond, 0)
	run.Metrics.FalseCanonicalClaimCount = nil
	decision, reasons := EvaluateProductionGate(GateProfileProductionProvider, run, 0.99)
	if decision != GateRejected {
		t.Fatalf("decision = %q, reasons=%v; want rejected for missing false-canonical measurement", decision, reasons)
	}
}

func TestProductionGateThresholdsAreDocumented(t *testing.T) {
	// The published gate doc quotes these exact numbers; pin them so the doc and
	// code cannot drift on any threshold.
	local := ProductionGateThresholdsFor(GateProfileLocalDeterministic)
	prod := ProductionGateThresholdsFor(GateProfileProductionProvider)

	wantLocal := ProductionGateThresholds{
		MinRecall: 0.60, MinPrecision: 0.50, MinNDCG: 0.60,
		MaxP95: 50 * time.Millisecond, MinVectorCoverage: 0.95,
	}
	wantProd := ProductionGateThresholds{
		MinRecall: 0.80, MinPrecision: 0.70, MinNDCG: 0.80,
		MaxP95: 150 * time.Millisecond, MinVectorCoverage: 0.98,
	}
	if local != wantLocal {
		t.Fatalf("local thresholds = %+v, want %+v", local, wantLocal)
	}
	if prod != wantProd {
		t.Fatalf("production thresholds = %+v, want %+v", prod, wantProd)
	}

	// Production must be a strictly stricter quality regime than local.
	if !(prod.MinRecall > local.MinRecall &&
		prod.MinPrecision > local.MinPrecision &&
		prod.MinNDCG > local.MinNDCG &&
		prod.MinVectorCoverage > local.MinVectorCoverage) {
		t.Fatalf("production quality thresholds must exceed local: local=%+v prod=%+v", local, prod)
	}
	// Provider latency budget is intentionally looser than the local in-process
	// path (network-backed), but both must be positive.
	if prod.MaxP95 <= local.MaxP95 || local.MaxP95 <= 0 {
		t.Fatalf("provider p95 budget %s must exceed local %s and both be positive", prod.MaxP95, local.MaxP95)
	}
}
