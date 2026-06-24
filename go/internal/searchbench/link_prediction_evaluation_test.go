// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScoreLinkPredictionEvaluationRecordsDiagnosticCandidateEvidence(t *testing.T) {
	t.Parallel()

	evaluation := validLinkPredictionEvaluation(t)
	if err := ValidateLinkPredictionEvaluation(evaluation); err != nil {
		t.Fatalf("ValidateLinkPredictionEvaluation() error = %v, want nil", err)
	}

	if got, want := evaluation.Metrics.CandidateCount, 3; got != want {
		t.Fatalf("candidate count = %d, want %d", got, want)
	}
	if got, want := evaluation.Metrics.PositiveCount, 1; got != want {
		t.Fatalf("positive count = %d, want %d", got, want)
	}
	if got, want := evaluation.Metrics.NegativeCount, 1; got != want {
		t.Fatalf("negative count = %d, want %d", got, want)
	}
	if got, want := evaluation.Metrics.AmbiguousCount, 1; got != want {
		t.Fatalf("ambiguous count = %d, want %d", got, want)
	}
	if math.Abs(evaluation.Metrics.PrecisionAtK-1.0/3.0) > 0.000001 {
		t.Fatalf("precision_at_k = %f, want %f", evaluation.Metrics.PrecisionAtK, 1.0/3.0)
	}
	if got, want := evaluation.GapDiscovery.Improvement, 1; got != want {
		t.Fatalf("gap discovery improvement = %d, want %d", got, want)
	}
	if got := evaluation.Metrics.FalseCanonicalClaimCount; got != 0 {
		t.Fatalf("false canonical claim count = %d, want 0", got)
	}

	for _, candidate := range evaluation.Candidates {
		if candidate.SourceHandle == "" || candidate.TargetHandle == "" {
			t.Fatalf("candidate handles must be present: %+v", candidate)
		}
		if candidate.EvidenceContext.Summary == "" {
			t.Fatalf("candidate evidence context summary is empty: %+v", candidate)
		}
		if candidate.Freshness.State == "" || candidate.Freshness.GraphGeneration == "" {
			t.Fatalf("candidate freshness is incomplete: %+v", candidate)
		}
		switch candidate.TruthLevel {
		case CandidateTruthLevelCandidate, CandidateTruthLevelSemanticCandidate:
		default:
			t.Fatalf("candidate truth level = %q, want candidate or semantic_candidate", candidate.TruthLevel)
		}
		if candidate.CanonicalRelationshipClaim {
			t.Fatalf("candidate made a canonical relationship claim: %+v", candidate)
		}
	}

	assertTelemetryCount(
		t,
		evaluation.TelemetryCounts,
		LinkPredictionAlgorithmCommonNeighbors,
		LinkPredictionDecisionPositive,
		1,
	)
	assertTelemetryCount(
		t,
		evaluation.TelemetryCounts,
		LinkPredictionAlgorithmAdamicAdar,
		LinkPredictionDecisionNegative,
		1,
	)
	assertTelemetryCount(
		t,
		evaluation.TelemetryCounts,
		LinkPredictionAlgorithmPredict,
		LinkPredictionDecisionAmbiguous,
		1,
	)
}

func TestValidateLinkPredictionEvaluationRejectsUnsafeCanonicalClaimsAndMissingShape(t *testing.T) {
	t.Parallel()

	evaluation := validLinkPredictionEvaluation(t)
	evaluation.Candidates[0].Algorithm = LinkPredictionAlgorithmAutoTLP
	evaluation.Candidates[0].TruthLevel = CandidateTruthLevel("exact")
	evaluation.Candidates[0].EvidenceContext = LinkPredictionEvidenceContext{}
	evaluation.Candidates[0].Freshness = LinkPredictionFreshness{}
	evaluation.Candidates[0].Reason = ""
	evaluation.Candidates[0].CanonicalRelationshipClaim = true
	evaluation.Metrics.FalseCanonicalClaimCount = 1

	err := ValidateLinkPredictionEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateLinkPredictionEvaluation() error = nil, want unsafe candidate failures")
	}
	for _, want := range []string{
		"candidates[0].algorithm auto_tlp is not diagnostic-only",
		"candidates[0].truth_level must be candidate or semantic_candidate",
		"candidates[0].evidence_context is required",
		"candidates[0].freshness is required",
		"candidates[0].reason is required",
		"candidates[0].canonical_relationship_claim must be false",
		"metrics.false_canonical_claim_count must be 0",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateLinkPredictionEvaluation() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateLinkPredictionEvaluationRequiresDecisionCoverageAndImprovement(t *testing.T) {
	t.Parallel()

	evaluation := validLinkPredictionEvaluation(t)
	evaluation.Candidates = evaluation.Candidates[:1]
	evaluation.Metrics = LinkPredictionMetrics{
		CandidateCount:            1,
		PositiveCount:             1,
		PrecisionAtK:              1,
		FalseCanonicalClaimCount:  0,
		CandidateTruthLevelCounts: map[CandidateTruthLevel]int{CandidateTruthLevelCandidate: 1},
	}
	evaluation.GapDiscovery = LinkPredictionGapDiscoverySummary{
		BaselineDiscovered:  1,
		CandidateDiscovered: 1,
		Improvement:         0,
	}
	evaluation.TelemetryCounts = []LinkPredictionTelemetryCount{{
		Algorithm: LinkPredictionAlgorithmCommonNeighbors,
		Decision:  LinkPredictionDecisionPositive,
		Count:     1,
	}}

	err := ValidateLinkPredictionEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateLinkPredictionEvaluation() error = nil, want coverage failure")
	}
	for _, want := range []string{
		"candidates must include positive, negative, and ambiguous decisions",
		"gap_discovery.improvement must be positive",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateLinkPredictionEvaluation() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateLinkPredictionEvaluationRejectsTelemetryMismatch(t *testing.T) {
	t.Parallel()

	evaluation := validLinkPredictionEvaluation(t)
	evaluation.TelemetryCounts[0].Count = 2

	err := ValidateLinkPredictionEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateLinkPredictionEvaluation() error = nil, want telemetry mismatch")
	}
	if want := "telemetry_counts[common_neighbors,positive] = 2, want 1"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateLinkPredictionEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestValidateLinkPredictionEvaluationRejectsUnexpectedTruthLevelMetric(t *testing.T) {
	t.Parallel()

	evaluation := validLinkPredictionEvaluation(t)
	evaluation.Metrics.CandidateTruthLevelCounts[CandidateTruthLevel("canonical")] = 1

	err := ValidateLinkPredictionEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateLinkPredictionEvaluation() error = nil, want unexpected truth-level count")
	}
	if want := "metrics.candidate_truth_level_counts[canonical] is unexpected"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateLinkPredictionEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestValidateLinkPredictionEvaluationRejectsDuplicateTelemetryCount(t *testing.T) {
	t.Parallel()

	evaluation := validLinkPredictionEvaluation(t)
	evaluation.TelemetryCounts = append(evaluation.TelemetryCounts, evaluation.TelemetryCounts[0])

	err := ValidateLinkPredictionEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateLinkPredictionEvaluation() error = nil, want duplicate telemetry count")
	}
	if want := "telemetry_counts[common_neighbors,positive] is duplicated"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateLinkPredictionEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestLinkPredictionEvaluationFixtureValidates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(
		"..",
		"..",
		"..",
		"docs",
		"public",
		"reference",
		"searchbench-evidence",
		"issue-420-link-prediction-evaluation-v1.json",
	)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	var evaluation LinkPredictionEvaluation
	if err := json.Unmarshal(data, &evaluation); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}
	if err := ValidateLinkPredictionEvaluation(evaluation); err != nil {
		t.Fatalf("ValidateLinkPredictionEvaluation(%q) error = %v", path, err)
	}
}

func validLinkPredictionEvaluation(t *testing.T) LinkPredictionEvaluation {
	t.Helper()

	observedAt := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	evaluation, err := ScoreLinkPredictionEvaluation(LinkPredictionEvaluationInput{
		EshuCommit:              "5b085c9f",
		BackendCommit:           "2ff4e099c5aa1263c1655523f15564db243c00d9",
		ProcedureMode:           LinkPredictionProcedureModeStream,
		BaselineDiscoveredGaps:  0,
		TopK:                    3,
		NornicDBProcedureSource: "pkg/cypher/linkprediction_test.go",
		Candidates: []LinkPredictionCandidate{
			{
				Algorithm:    LinkPredictionAlgorithmCommonNeighbors,
				Score:        0.92,
				SourceHandle: "repository:checkout-api",
				TargetHandle: "service:checkout",
				EvidenceContext: LinkPredictionEvidenceContext{
					Summary:               "shared workload and image evidence",
					SharedNeighborHandles: []string{"workload:checkout-api", "image:sha256:abc"},
				},
				Freshness: LinkPredictionFreshness{
					State:           LinkPredictionFreshnessFresh,
					GraphGeneration: "generation-1",
					ObservedAt:      observedAt,
				},
				Reason:     "common neighbors identify a missing repository-to-service relationship",
				TruthLevel: CandidateTruthLevelCandidate,
				Decision:   LinkPredictionDecisionPositive,
			},
			{
				Algorithm:    LinkPredictionAlgorithmAdamicAdar,
				Score:        0.61,
				SourceHandle: "repository:docs",
				TargetHandle: "service:checkout",
				EvidenceContext: LinkPredictionEvidenceContext{
					Summary:               "weak shared documentation neighbor only",
					SharedNeighborHandles: []string{"repository:platform-docs"},
				},
				Freshness: LinkPredictionFreshness{
					State:           LinkPredictionFreshnessFresh,
					GraphGeneration: "generation-1",
					ObservedAt:      observedAt,
				},
				Reason:     "candidate did not correspond to a later canonical edge",
				TruthLevel: CandidateTruthLevelCandidate,
				Decision:   LinkPredictionDecisionNegative,
			},
			{
				Algorithm:    LinkPredictionAlgorithmPredict,
				Score:        0.54,
				SourceHandle: "repository:billing-worker",
				TargetHandle: "service:checkout",
				EvidenceContext: LinkPredictionEvidenceContext{
					Summary:               "topology and semantic signals disagree",
					SharedNeighborHandles: []string{"queue:payments", "topic:orders"},
				},
				Freshness: LinkPredictionFreshness{
					State:           LinkPredictionFreshnessFresh,
					GraphGeneration: "generation-1",
					ObservedAt:      observedAt,
				},
				Reason:     "mixed algorithm evidence remains provenance-only",
				TruthLevel: CandidateTruthLevelSemanticCandidate,
				Decision:   LinkPredictionDecisionAmbiguous,
			},
		},
	})
	if err != nil {
		t.Fatalf("ScoreLinkPredictionEvaluation() error = %v, want nil", err)
	}
	return evaluation
}

func assertTelemetryCount(
	t *testing.T,
	counts []LinkPredictionTelemetryCount,
	algorithm LinkPredictionAlgorithm,
	decision LinkPredictionDecision,
	want int,
) {
	t.Helper()

	for _, count := range counts {
		if count.Algorithm == algorithm && count.Decision == decision {
			if count.Count != want {
				t.Fatalf("telemetry count for %s/%s = %d, want %d", algorithm, decision, count.Count, want)
			}
			return
		}
	}
	t.Fatalf("missing telemetry count for %s/%s", algorithm, decision)
}
