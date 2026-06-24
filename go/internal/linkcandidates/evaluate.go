// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linkcandidates

import (
	"fmt"
	"sort"
	"strings"
)

// ExpectedGap is one relationship gap the candidate set should discover.
type ExpectedGap struct {
	Source GraphHandle `json:"source"`
	Target GraphHandle `json:"target"`
}

// EvaluationInput is one fixture-testable candidate evaluation.
type EvaluationInput struct {
	ExpectedGaps []ExpectedGap `json:"expected_gaps"`
	Candidates   []Candidate   `json:"candidates"`
}

// Evaluation records relationship-gap discovery and telemetry-ready counts.
type Evaluation struct {
	CandidateCount          int             `json:"candidate_count"`
	GeneratedCount          int             `json:"generated_count"`
	SuppressedCount         int             `json:"suppressed_count"`
	AmbiguousCount          int             `json:"ambiguous_count"`
	MatchedExpectedGapCount int             `json:"matched_expected_gap_count"`
	FalsePositiveCount      int             `json:"false_positive_count"`
	Precision               float64         `json:"precision"`
	Recall                  float64         `json:"recall"`
	DecisionCounts          []DecisionCount `json:"decision_counts"`
}

// DecisionCount is one low-cardinality algorithm and decision aggregate.
type DecisionCount struct {
	Algorithm string   `json:"algorithm"`
	Decision  Decision `json:"decision"`
	Count     int      `json:"count"`
}

// EvaluateCandidates scores diagnostic candidates against expected gaps.
func EvaluateCandidates(input EvaluationInput) (Evaluation, error) {
	expected, err := expectedGapSet(input.ExpectedGaps)
	if err != nil {
		return Evaluation{}, err
	}

	evaluation := Evaluation{CandidateCount: len(input.Candidates)}
	matchedExpected := make(map[string]struct{}, len(expected))
	decisionCounts := make(map[decisionCountKey]int)
	generatedMatches := 0

	for i, candidate := range input.Candidates {
		if err := ValidateCandidate(candidate); err != nil {
			return evaluation, fmt.Errorf("candidates[%d]: %w", i, err)
		}
		observation := ObservationFor(candidate)
		decisionCounts[decisionCountKey{
			algorithm: observation.Algorithm,
			decision:  observation.Decision,
		}]++

		switch candidate.Decision {
		case DecisionGenerated:
			evaluation.GeneratedCount++
			gapKey := candidateGapKey(candidate)
			if _, ok := expected[gapKey]; ok {
				if _, matched := matchedExpected[gapKey]; matched {
					evaluation.FalsePositiveCount++
				} else {
					generatedMatches++
					matchedExpected[gapKey] = struct{}{}
				}
			} else {
				evaluation.FalsePositiveCount++
			}
		case DecisionSuppressed:
			evaluation.SuppressedCount++
		case DecisionAmbiguous:
			evaluation.AmbiguousCount++
		}
	}

	evaluation.MatchedExpectedGapCount = len(matchedExpected)
	if evaluation.GeneratedCount > 0 {
		evaluation.Precision = float64(generatedMatches) / float64(evaluation.GeneratedCount)
	}
	if len(expected) > 0 {
		evaluation.Recall = float64(evaluation.MatchedExpectedGapCount) / float64(len(expected))
	}
	evaluation.DecisionCounts = sortedDecisionCounts(decisionCounts)
	return evaluation, nil
}

type decisionCountKey struct {
	algorithm string
	decision  Decision
}

func expectedGapSet(gaps []ExpectedGap) (map[string]struct{}, error) {
	if len(gaps) == 0 {
		return nil, fmt.Errorf("expected_gaps are required")
	}
	out := make(map[string]struct{}, len(gaps))
	for i, gap := range gaps {
		if !validHandle(gap.Source) {
			return nil, fmt.Errorf("expected_gaps[%d].source handle is required", i)
		}
		if !validHandle(gap.Target) {
			return nil, fmt.Errorf("expected_gaps[%d].target handle is required", i)
		}
		out[gapKey(gap.Source, gap.Target)] = struct{}{}
	}
	return out, nil
}

func sortedDecisionCounts(counts map[decisionCountKey]int) []DecisionCount {
	out := make([]DecisionCount, 0, len(counts))
	for key, count := range counts {
		out = append(out, DecisionCount{
			Algorithm: key.algorithm,
			Decision:  key.decision,
			Count:     count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Algorithm == out[j].Algorithm {
			return out[i].Decision < out[j].Decision
		}
		return out[i].Algorithm < out[j].Algorithm
	})
	return out
}

func candidateGapKey(candidate Candidate) string {
	return gapKey(candidate.Source, candidate.Target)
}

func gapKey(source GraphHandle, target GraphHandle) string {
	return handleKey(source) + "->" + handleKey(target)
}

func handleKey(handle GraphHandle) string {
	return strings.TrimSpace(handle.Kind) + ":" + strings.TrimSpace(handle.ID)
}
