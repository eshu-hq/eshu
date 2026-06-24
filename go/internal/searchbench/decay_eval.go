// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/searchdecay"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// ScoreDecayEvaluation computes before/after ranking evidence for one query.
func ScoreDecayEvaluation(
	ctx context.Context,
	input DecayEvaluationInput,
) (DecayEvaluation, error) {
	query := input.Query
	query.ID = strings.TrimSpace(query.ID)
	entries := rankedDecayEntries(input.Candidates)
	beforeResults := decayEntryResults(entries)
	beforeTopK := boundedResults(query, beforeResults)

	evaluation := DecayEvaluation{
		QueryID:                       query.ID,
		MetricsBefore:                 ScoreQueryResults(query, beforeTopK),
		RequiredEvidenceVisibleBefore: requiredEvidenceVisible(query, beforeTopK),
		Results:                       make([]DecayEvaluationResult, 0, len(entries)),
	}

	expected := expectedHandleSet(query)
	for _, entry := range entries {
		decision, err := input.Scorer.Score(ctx, entry.candidate.Evidence)
		if err != nil {
			return evaluation, err
		}
		if evaluation.PolicyID == "" {
			evaluation.PolicyID = decision.PolicyID
		}
		entry.decision = decision
		if entry.candidate.Result.Document.TruthScope.Level != searchdocs.TruthLevelDerived {
			evaluation.FalseCanonicalCandidateCount++
		}
		entry.required = resultContainsExpected(entry.candidate.Result, expected)
	}

	decayed := append([]*decayEvaluationEntry(nil), entries...)
	sort.SliceStable(decayed, func(i, j int) bool {
		if decayed[i].decision.Score == decayed[j].decision.Score {
			return decayed[i].originalRank < decayed[j].originalRank
		}
		return decayed[i].decision.Score > decayed[j].decision.Score
	})
	for i, entry := range decayed {
		entry.decayedRank = i + 1
	}

	afterResults := decayEntryResults(decayed)
	afterTopK := boundedResults(query, afterResults)
	evaluation.MetricsAfter = ScoreQueryResults(query, afterTopK)
	evaluation.RequiredEvidenceVisibleAfter = requiredEvidenceVisible(query, afterTopK)
	evaluation.RecallDelta = evaluation.MetricsAfter.Recall - evaluation.MetricsBefore.Recall
	evaluation.PrecisionDelta = evaluation.MetricsAfter.Precision - evaluation.MetricsBefore.Precision
	evaluation.NDCGDelta = evaluation.MetricsAfter.NDCG - evaluation.MetricsBefore.NDCG
	for _, entry := range entries {
		evaluation.Results = append(evaluation.Results, DecayEvaluationResult{
			DocumentID:    entry.candidate.Result.Document.ID,
			OriginalRank:  entry.originalRank,
			DecayedRank:   entry.decayedRank,
			OriginalScore: entry.decision.OriginalScore,
			DecayedScore:  entry.decision.Score,
			DecayOutcome:  entry.decision.Outcome,
			Required:      entry.required,
		})
	}
	return evaluation, nil
}

// ValidateDecayEvaluation checks guardrails for a decay-scoring eval gate.
func ValidateDecayEvaluation(evaluation DecayEvaluation) error {
	var problems []string
	if strings.TrimSpace(evaluation.QueryID) == "" {
		problems = append(problems, "query_id is required")
	}
	if strings.TrimSpace(evaluation.PolicyID) == "" {
		problems = append(problems, "policy_id is required")
	}
	if evaluation.MetricsBefore.FalseCanonicalClaimCount == nil {
		problems = append(problems, "metrics_before.false_canonical_claim_count is required")
	}
	if evaluation.MetricsAfter.FalseCanonicalClaimCount == nil {
		problems = append(problems, "metrics_after.false_canonical_claim_count is required")
	}
	if len(evaluation.Results) == 0 {
		problems = append(problems, "results are required")
	}
	if evaluation.RequiredEvidenceVisibleBefore && !evaluation.RequiredEvidenceVisibleAfter {
		problems = append(problems, "required evidence was hidden after decay")
	} else if !evaluation.RequiredEvidenceVisibleAfter {
		problems = append(problems, "required evidence was not visible after decay")
	}
	if evaluation.FalseCanonicalCandidateCount > 0 {
		problems = append(problems, "decay evaluation has false canonical claims")
	}
	return joinedValidationError(problems)
}

type decayEvaluationEntry struct {
	candidate    DecayCandidate
	decision     searchdecay.Decision
	originalRank int
	decayedRank  int
	required     bool
}

func rankedDecayEntries(candidates []DecayCandidate) []*decayEvaluationEntry {
	entries := make([]*decayEvaluationEntry, 0, len(candidates))
	for i, candidate := range candidates {
		rank := candidate.Result.Rank
		if rank <= 0 {
			rank = i + 1
		}
		entries = append(entries, &decayEvaluationEntry{
			candidate:    candidate,
			originalRank: rank,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].originalRank < entries[j].originalRank
	})
	for i, entry := range entries {
		entry.originalRank = i + 1
		entry.candidate.Result.Rank = i + 1
	}
	return entries
}

func decayEntryResults(entries []*decayEvaluationEntry) []Result {
	results := make([]Result, 0, len(entries))
	for i, entry := range entries {
		result := entry.candidate.Result
		result.Rank = i + 1
		results = append(results, result)
	}
	return results
}

func boundedResults(query Query, results []Result) []Result {
	if query.Limit <= 0 || query.Limit >= len(results) {
		return results
	}
	return results[:query.Limit]
}

func requiredEvidenceVisible(query Query, results []Result) bool {
	expected := expectedHandleSet(query)
	if len(expected) == 0 {
		return false
	}
	matched := make(map[string]struct{}, len(expected))
	for _, result := range results {
		for _, handle := range result.Document.GraphHandles {
			key := handleKey(handle)
			if _, ok := expected[key]; ok {
				matched[key] = struct{}{}
			}
		}
	}
	return len(matched) == len(expected)
}

func expectedHandleSet(query Query) map[string]struct{} {
	expected := make(map[string]struct{}, len(query.ExpectedHandles))
	for _, handle := range query.ExpectedHandles {
		if handle = strings.TrimSpace(handle); handle != "" {
			expected[handle] = struct{}{}
		}
	}
	return expected
}

func resultContainsExpected(result Result, expected map[string]struct{}) bool {
	for _, handle := range result.Document.GraphHandles {
		if _, ok := expected[handleKey(handle)]; ok {
			return true
		}
	}
	return false
}
