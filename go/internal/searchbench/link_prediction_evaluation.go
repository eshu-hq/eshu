// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// LinkPredictionEvaluationVersion is the first issue #420 evidence schema.
const LinkPredictionEvaluationVersion = "link-prediction-evaluation/v1"

// LinkPredictionAlgorithm names one NornicDB link-prediction algorithm.
type LinkPredictionAlgorithm string

const (
	// LinkPredictionAlgorithmCommonNeighbors counts shared graph neighbors.
	LinkPredictionAlgorithmCommonNeighbors LinkPredictionAlgorithm = "common_neighbors"
	// LinkPredictionAlgorithmJaccard normalizes shared neighbors by union size.
	LinkPredictionAlgorithmJaccard LinkPredictionAlgorithm = "jaccard"
	// LinkPredictionAlgorithmAdamicAdar weights rare shared neighbors higher.
	LinkPredictionAlgorithmAdamicAdar LinkPredictionAlgorithm = "adamic_adar"
	// LinkPredictionAlgorithmResourceAllocation weights shared neighbors by inverse degree.
	LinkPredictionAlgorithmResourceAllocation LinkPredictionAlgorithm = "resource_allocation"
	// LinkPredictionAlgorithmPreferentialAttachment scores degree-product candidates.
	LinkPredictionAlgorithmPreferentialAttachment LinkPredictionAlgorithm = "preferential_attachment"
	// LinkPredictionAlgorithmPredict is NornicDB's hybrid stream procedure.
	LinkPredictionAlgorithmPredict LinkPredictionAlgorithm = "predict"
	// LinkPredictionAlgorithmAutoTLP is rejected for Eshu issue #420 because it materializes edges.
	LinkPredictionAlgorithmAutoTLP LinkPredictionAlgorithm = "auto_tlp"
)

// LinkPredictionProcedureMode names how candidate procedures were invoked.
type LinkPredictionProcedureMode string

const (
	// LinkPredictionProcedureModeStream records GDS-style stream procedures with no graph mutation.
	LinkPredictionProcedureModeStream LinkPredictionProcedureMode = "gds_stream"
)

// CandidateTruthLevel is the internal issue #420 candidate authority class.
type CandidateTruthLevel string

const (
	// CandidateTruthLevelCandidate is non-semantic diagnostic candidate evidence.
	CandidateTruthLevelCandidate CandidateTruthLevel = "candidate"
	// CandidateTruthLevelSemanticCandidate is semantic or hybrid diagnostic candidate evidence.
	CandidateTruthLevelSemanticCandidate CandidateTruthLevel = "semantic_candidate"
)

// LinkPredictionDecision classifies one candidate for evaluation.
type LinkPredictionDecision string

const (
	// LinkPredictionDecisionPositive means the candidate exposed a relationship gap.
	LinkPredictionDecisionPositive LinkPredictionDecision = "positive"
	// LinkPredictionDecisionNegative means the candidate did not survive evaluation.
	LinkPredictionDecisionNegative LinkPredictionDecision = "negative"
	// LinkPredictionDecisionAmbiguous means the candidate remains provenance-only.
	LinkPredictionDecisionAmbiguous LinkPredictionDecision = "ambiguous"
)

// LinkPredictionFreshnessState records graph freshness for one candidate.
type LinkPredictionFreshnessState string

const (
	// LinkPredictionFreshnessFresh means the candidate was scored against current graph evidence.
	LinkPredictionFreshnessFresh LinkPredictionFreshnessState = "fresh"
	// LinkPredictionFreshnessStale means the candidate was scored against stale graph evidence.
	LinkPredictionFreshnessStale LinkPredictionFreshnessState = "stale"
	// LinkPredictionFreshnessBuilding means graph evidence was still converging.
	LinkPredictionFreshnessBuilding LinkPredictionFreshnessState = "building"
	// LinkPredictionFreshnessUnavailable means freshness could not be established.
	LinkPredictionFreshnessUnavailable LinkPredictionFreshnessState = "unavailable"
)

// LinkPredictionEvaluationInput is the scoring input for one issue #420 record.
type LinkPredictionEvaluationInput struct {
	EshuCommit              string
	BackendCommit           string
	ProcedureMode           LinkPredictionProcedureMode
	NornicDBProcedureSource string
	BaselineDiscoveredGaps  int
	TopK                    int
	Candidates              []LinkPredictionCandidate
}

// LinkPredictionEvaluation is one versioned issue #420 evidence record.
type LinkPredictionEvaluation struct {
	Version                 string                            `json:"version"`
	EshuCommit              string                            `json:"eshu_commit"`
	BackendCommit           string                            `json:"backend_commit"`
	ProcedureMode           LinkPredictionProcedureMode       `json:"procedure_mode"`
	NornicDBProcedureSource string                            `json:"nornicdb_procedure_source"`
	TopK                    int                               `json:"top_k"`
	Candidates              []LinkPredictionCandidate         `json:"candidates"`
	Metrics                 LinkPredictionMetrics             `json:"metrics"`
	GapDiscovery            LinkPredictionGapDiscoverySummary `json:"gap_discovery"`
	TelemetryCounts         []LinkPredictionTelemetryCount    `json:"telemetry_counts"`
}

// LinkPredictionCandidate is one non-canonical relationship suggestion.
type LinkPredictionCandidate struct {
	Algorithm                  LinkPredictionAlgorithm       `json:"algorithm"`
	Score                      float64                       `json:"score"`
	SourceHandle               string                        `json:"source_handle"`
	TargetHandle               string                        `json:"target_handle"`
	EvidenceContext            LinkPredictionEvidenceContext `json:"evidence_context"`
	Freshness                  LinkPredictionFreshness       `json:"freshness"`
	Reason                     string                        `json:"reason"`
	TruthLevel                 CandidateTruthLevel           `json:"truth_level"`
	Decision                   LinkPredictionDecision        `json:"decision"`
	CanonicalRelationshipClaim bool                          `json:"canonical_relationship_claim"`
}

// LinkPredictionEvidenceContext explains why a candidate was suggested.
type LinkPredictionEvidenceContext struct {
	Summary               string   `json:"summary"`
	SharedNeighborHandles []string `json:"shared_neighbor_handles,omitempty"`
}

// LinkPredictionFreshness ties a candidate to graph generation evidence.
type LinkPredictionFreshness struct {
	State           LinkPredictionFreshnessState `json:"state"`
	GraphGeneration string                       `json:"graph_generation"`
	ObservedAt      time.Time                    `json:"observed_at"`
}

// LinkPredictionMetrics records accuracy and safety for one candidate set.
type LinkPredictionMetrics struct {
	CandidateCount                 int                         `json:"candidate_count"`
	PositiveCount                  int                         `json:"positive_count"`
	NegativeCount                  int                         `json:"negative_count"`
	AmbiguousCount                 int                         `json:"ambiguous_count"`
	PrecisionAtK                   float64                     `json:"precision_at_k"`
	FalseCanonicalClaimCount       int                         `json:"false_canonical_claim_count"`
	CandidateTruthLevelCounts      map[CandidateTruthLevel]int `json:"candidate_truth_level_counts"`
	SemanticCandidateTruthCount    int                         `json:"semantic_candidate_truth_count"`
	NonSemanticCandidateTruthCount int                         `json:"non_semantic_candidate_truth_count"`
}

// LinkPredictionGapDiscoverySummary compares candidate discovery to a baseline.
type LinkPredictionGapDiscoverySummary struct {
	BaselineDiscovered  int `json:"baseline_discovered"`
	CandidateDiscovered int `json:"candidate_discovered"`
	Improvement         int `json:"improvement"`
}

// LinkPredictionTelemetryCount is the low-cardinality generation-count signal.
type LinkPredictionTelemetryCount struct {
	Algorithm LinkPredictionAlgorithm `json:"algorithm"`
	Decision  LinkPredictionDecision  `json:"decision"`
	Count     int                     `json:"count"`
}

// ScoreLinkPredictionEvaluation computes issue #420 metrics from recorded candidates.
func ScoreLinkPredictionEvaluation(input LinkPredictionEvaluationInput) (LinkPredictionEvaluation, error) {
	candidates := append([]LinkPredictionCandidate(nil), input.Candidates...)
	topK := input.TopK
	if topK <= 0 || topK > len(candidates) {
		topK = len(candidates)
	}

	metrics := scoreLinkPredictionCandidates(candidates, topK)
	gapDiscovery := LinkPredictionGapDiscoverySummary{
		BaselineDiscovered:  input.BaselineDiscoveredGaps,
		CandidateDiscovered: metrics.PositiveCount,
		Improvement:         metrics.PositiveCount - input.BaselineDiscoveredGaps,
	}

	return LinkPredictionEvaluation{
		Version:                 LinkPredictionEvaluationVersion,
		EshuCommit:              input.EshuCommit,
		BackendCommit:           input.BackendCommit,
		ProcedureMode:           input.ProcedureMode,
		NornicDBProcedureSource: input.NornicDBProcedureSource,
		TopK:                    topK,
		Candidates:              candidates,
		Metrics:                 metrics,
		GapDiscovery:            gapDiscovery,
		TelemetryCounts:         linkPredictionTelemetryCounts(candidates),
	}, nil
}

// ValidateLinkPredictionEvaluation checks the issue #420 evidence contract.
func ValidateLinkPredictionEvaluation(evaluation LinkPredictionEvaluation) error {
	var problems []string
	if evaluation.Version != LinkPredictionEvaluationVersion {
		problems = append(problems, "version must be "+LinkPredictionEvaluationVersion)
	}
	if strings.TrimSpace(evaluation.EshuCommit) == "" {
		problems = append(problems, "eshu_commit is required")
	}
	if strings.TrimSpace(evaluation.BackendCommit) == "" {
		problems = append(problems, "backend_commit is required")
	}
	if evaluation.ProcedureMode != LinkPredictionProcedureModeStream {
		problems = append(problems, "procedure_mode must be gds_stream")
	}
	if strings.TrimSpace(evaluation.NornicDBProcedureSource) == "" {
		problems = append(problems, "nornicdb_procedure_source is required")
	}
	if len(evaluation.Candidates) == 0 {
		problems = append(problems, "candidates are required")
	}
	if evaluation.TopK <= 0 {
		problems = append(problems, "top_k is required")
	}

	coverage := map[LinkPredictionDecision]int{}
	for i, candidate := range evaluation.Candidates {
		problems = append(problems, validateLinkPredictionCandidate(i, candidate)...)
		if validLinkPredictionDecision(candidate.Decision) {
			coverage[candidate.Decision]++
		}
	}
	if coverage[LinkPredictionDecisionPositive] == 0 ||
		coverage[LinkPredictionDecisionNegative] == 0 ||
		coverage[LinkPredictionDecisionAmbiguous] == 0 {
		problems = append(problems, "candidates must include positive, negative, and ambiguous decisions")
	}

	expectedMetrics := scoreLinkPredictionCandidates(evaluation.Candidates, evaluation.TopK)
	problems = append(problems, validateLinkPredictionMetrics(evaluation.Metrics, expectedMetrics)...)
	if evaluation.Metrics.FalseCanonicalClaimCount != 0 {
		problems = append(problems, "metrics.false_canonical_claim_count must be 0")
	}
	if evaluation.GapDiscovery.Improvement <= 0 {
		problems = append(problems, "gap_discovery.improvement must be positive")
	}
	if evaluation.GapDiscovery.CandidateDiscovered != expectedMetrics.PositiveCount {
		problems = append(problems, "gap_discovery.candidate_discovered must equal positive candidate count")
	}
	if evaluation.GapDiscovery.Improvement !=
		evaluation.GapDiscovery.CandidateDiscovered-evaluation.GapDiscovery.BaselineDiscovered {
		problems = append(problems, "gap_discovery.improvement must equal candidate_discovered minus baseline_discovered")
	}
	problems = append(problems, validateLinkPredictionTelemetryCounts(
		evaluation.TelemetryCounts,
		linkPredictionTelemetryCounts(evaluation.Candidates),
	)...)
	return joinedValidationError(problems)
}

func scoreLinkPredictionCandidates(candidates []LinkPredictionCandidate, topK int) LinkPredictionMetrics {
	metrics := LinkPredictionMetrics{
		CandidateCount:            len(candidates),
		CandidateTruthLevelCounts: make(map[CandidateTruthLevel]int),
	}
	limit := topK
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	topKPositive := 0
	for i, candidate := range candidates {
		switch candidate.Decision {
		case LinkPredictionDecisionPositive:
			metrics.PositiveCount++
			if i < limit {
				topKPositive++
			}
		case LinkPredictionDecisionNegative:
			metrics.NegativeCount++
		case LinkPredictionDecisionAmbiguous:
			metrics.AmbiguousCount++
		}
		if candidate.CanonicalRelationshipClaim || !validCandidateTruthLevel(candidate.TruthLevel) {
			metrics.FalseCanonicalClaimCount++
		}
		metrics.CandidateTruthLevelCounts[candidate.TruthLevel]++
	}
	metrics.SemanticCandidateTruthCount = metrics.CandidateTruthLevelCounts[CandidateTruthLevelSemanticCandidate]
	metrics.NonSemanticCandidateTruthCount = metrics.CandidateTruthLevelCounts[CandidateTruthLevelCandidate]
	if limit > 0 {
		metrics.PrecisionAtK = float64(topKPositive) / float64(limit)
	}
	return metrics
}

func validateLinkPredictionCandidate(index int, candidate LinkPredictionCandidate) []string {
	var problems []string
	prefix := fmt.Sprintf("candidates[%d]", index)
	problems = append(problems, validateLinkPredictionAlgorithm(prefix, candidate.Algorithm)...)
	if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) || candidate.Score < 0 {
		problems = append(problems, prefix+".score must be a finite non-negative value")
	}
	if strings.TrimSpace(candidate.SourceHandle) == "" {
		problems = append(problems, prefix+".source_handle is required")
	}
	if strings.TrimSpace(candidate.TargetHandle) == "" {
		problems = append(problems, prefix+".target_handle is required")
	}
	if candidate.SourceHandle != "" && candidate.SourceHandle == candidate.TargetHandle {
		problems = append(problems, prefix+".source_handle and target_handle must differ")
	}
	if strings.TrimSpace(candidate.EvidenceContext.Summary) == "" &&
		len(candidate.EvidenceContext.SharedNeighborHandles) == 0 {
		problems = append(problems, prefix+".evidence_context is required")
	}
	if candidate.Freshness == (LinkPredictionFreshness{}) {
		problems = append(problems, prefix+".freshness is required")
	} else {
		problems = append(problems, validateLinkPredictionFreshness(prefix+".freshness", candidate.Freshness)...)
	}
	if strings.TrimSpace(candidate.Reason) == "" {
		problems = append(problems, prefix+".reason is required")
	}
	if !validCandidateTruthLevel(candidate.TruthLevel) {
		problems = append(problems, prefix+".truth_level must be candidate or semantic_candidate")
	}
	if !validLinkPredictionDecision(candidate.Decision) {
		problems = append(problems, prefix+".decision must be positive, negative, or ambiguous")
	}
	if candidate.CanonicalRelationshipClaim {
		problems = append(problems, prefix+".canonical_relationship_claim must be false")
	}
	return problems
}

func validateLinkPredictionAlgorithm(prefix string, algorithm LinkPredictionAlgorithm) []string {
	switch algorithm {
	case LinkPredictionAlgorithmCommonNeighbors,
		LinkPredictionAlgorithmJaccard,
		LinkPredictionAlgorithmAdamicAdar,
		LinkPredictionAlgorithmResourceAllocation,
		LinkPredictionAlgorithmPreferentialAttachment,
		LinkPredictionAlgorithmPredict:
		return nil
	case LinkPredictionAlgorithmAutoTLP:
		return []string{fmt.Sprintf("%s.algorithm auto_tlp is not diagnostic-only", prefix)}
	case "":
		return []string{prefix + ".algorithm is required"}
	default:
		return []string{prefix + ".algorithm is invalid"}
	}
}

func validateLinkPredictionFreshness(prefix string, freshness LinkPredictionFreshness) []string {
	var problems []string
	if !validLinkPredictionFreshnessState(freshness.State) {
		problems = append(problems, prefix+".state must be fresh, stale, building, or unavailable")
	}
	if strings.TrimSpace(freshness.GraphGeneration) == "" {
		problems = append(problems, prefix+".graph_generation is required")
	}
	if freshness.ObservedAt.IsZero() {
		problems = append(problems, prefix+".observed_at is required")
	}
	return problems
}

func validateLinkPredictionMetrics(got, want LinkPredictionMetrics) []string {
	var problems []string
	if got.CandidateCount != want.CandidateCount {
		problems = append(problems, fmt.Sprintf("metrics.candidate_count = %d, want %d", got.CandidateCount, want.CandidateCount))
	}
	if got.PositiveCount != want.PositiveCount {
		problems = append(problems, fmt.Sprintf("metrics.positive_count = %d, want %d", got.PositiveCount, want.PositiveCount))
	}
	if got.NegativeCount != want.NegativeCount {
		problems = append(problems, fmt.Sprintf("metrics.negative_count = %d, want %d", got.NegativeCount, want.NegativeCount))
	}
	if got.AmbiguousCount != want.AmbiguousCount {
		problems = append(problems, fmt.Sprintf("metrics.ambiguous_count = %d, want %d", got.AmbiguousCount, want.AmbiguousCount))
	}
	if math.Abs(got.PrecisionAtK-want.PrecisionAtK) > 0.000001 {
		problems = append(problems, fmt.Sprintf("metrics.precision_at_k = %f, want %f", got.PrecisionAtK, want.PrecisionAtK))
	}
	if got.FalseCanonicalClaimCount != want.FalseCanonicalClaimCount {
		problems = append(problems, fmt.Sprintf(
			"metrics.false_canonical_claim_count = %d, want %d",
			got.FalseCanonicalClaimCount,
			want.FalseCanonicalClaimCount,
		))
	}
	for level, count := range want.CandidateTruthLevelCounts {
		if got.CandidateTruthLevelCounts[level] != count {
			problems = append(problems, fmt.Sprintf(
				"metrics.candidate_truth_level_counts[%s] = %d, want %d",
				level,
				got.CandidateTruthLevelCounts[level],
				count,
			))
		}
	}
	for level := range got.CandidateTruthLevelCounts {
		if _, ok := want.CandidateTruthLevelCounts[level]; !ok {
			problems = append(problems, fmt.Sprintf("metrics.candidate_truth_level_counts[%s] is unexpected", level))
		}
	}
	return problems
}

func linkPredictionTelemetryCounts(candidates []LinkPredictionCandidate) []LinkPredictionTelemetryCount {
	type key struct {
		algorithm LinkPredictionAlgorithm
		decision  LinkPredictionDecision
	}
	seen := make(map[key]int)
	order := make([]key, 0)
	for _, candidate := range candidates {
		k := key{algorithm: candidate.Algorithm, decision: candidate.Decision}
		if _, ok := seen[k]; !ok {
			order = append(order, k)
		}
		seen[k]++
	}
	counts := make([]LinkPredictionTelemetryCount, 0, len(order))
	for _, k := range order {
		counts = append(counts, LinkPredictionTelemetryCount{
			Algorithm: k.algorithm,
			Decision:  k.decision,
			Count:     seen[k],
		})
	}
	return counts
}

func validateLinkPredictionTelemetryCounts(
	got []LinkPredictionTelemetryCount,
	want []LinkPredictionTelemetryCount,
) []string {
	var problems []string
	if len(got) == 0 {
		return []string{"telemetry_counts are required"}
	}
	wantByKey := make(map[string]int, len(want))
	for _, count := range want {
		wantByKey[telemetryCountKey(count.Algorithm, count.Decision)] = count.Count
	}
	seen := make(map[string]struct{}, len(got))
	for _, count := range got {
		key := telemetryCountKey(count.Algorithm, count.Decision)
		if _, ok := seen[key]; ok {
			problems = append(problems, fmt.Sprintf("telemetry_counts[%s,%s] is duplicated", count.Algorithm, count.Decision))
			continue
		}
		seen[key] = struct{}{}
		wantCount, ok := wantByKey[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("telemetry_counts[%s,%s] is unexpected", count.Algorithm, count.Decision))
			continue
		}
		if count.Count != wantCount {
			problems = append(problems, fmt.Sprintf(
				"telemetry_counts[%s,%s] = %d, want %d",
				count.Algorithm,
				count.Decision,
				count.Count,
				wantCount,
			))
		}
	}
	for _, count := range want {
		key := telemetryCountKey(count.Algorithm, count.Decision)
		if _, ok := seen[key]; !ok {
			problems = append(problems, fmt.Sprintf("telemetry_counts[%s,%s] is required", count.Algorithm, count.Decision))
		}
	}
	return problems
}

func telemetryCountKey(algorithm LinkPredictionAlgorithm, decision LinkPredictionDecision) string {
	return string(algorithm) + "\x00" + string(decision)
}

func validCandidateTruthLevel(level CandidateTruthLevel) bool {
	return level == CandidateTruthLevelCandidate || level == CandidateTruthLevelSemanticCandidate
}

func validLinkPredictionDecision(decision LinkPredictionDecision) bool {
	return decision == LinkPredictionDecisionPositive ||
		decision == LinkPredictionDecisionNegative ||
		decision == LinkPredictionDecisionAmbiguous
}

func validLinkPredictionFreshnessState(state LinkPredictionFreshnessState) bool {
	return state == LinkPredictionFreshnessFresh ||
		state == LinkPredictionFreshnessStale ||
		state == LinkPredictionFreshnessBuilding ||
		state == LinkPredictionFreshnessUnavailable
}
