// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// ScoreRerankEvaluation computes before/after ranking evidence for one query.
func ScoreRerankEvaluation(input RerankEvaluationInput) (RerankEvaluation, error) {
	if err := validateRerankEvaluationInput(input); err != nil {
		return RerankEvaluation{}, err
	}

	query := input.Query
	query.ID = strings.TrimSpace(query.ID)
	baseline := rankedResults(input.Baseline)
	reranked := rankedResults(input.Reranked)
	baselineMetrics := ScoreQueryResults(query, boundedResults(query, baseline))
	rerankedMetrics := ScoreQueryResults(query, boundedResults(query, reranked))

	evaluation := RerankEvaluation{
		QueryID:                      query.ID,
		BaselineHybridEvidence:       input.BaselineHybridEvidence,
		BaselineMetrics:              baselineMetrics,
		RerankedMetrics:              rerankedMetrics,
		RecallDelta:                  rerankedMetrics.Recall - baselineMetrics.Recall,
		PrecisionDelta:               rerankedMetrics.Precision - baselineMetrics.Precision,
		NDCGDelta:                    rerankedMetrics.NDCG - baselineMetrics.NDCG,
		FalseCanonicalClaimDelta:     claimCount(rerankedMetrics) - claimCount(baselineMetrics),
		FalseCanonicalCandidateCount: falseCanonicalCandidateCount(baseline, reranked),
		BaselineLatency:              input.BaselineLatency,
		RerankedLatency:              input.RerankedLatency,
		LatencyDelta:                 input.RerankedLatency - input.BaselineLatency,
		BaselineCostMicrosUSD:        input.BaselineCostMicrosUSD,
		RerankedCostMicrosUSD:        input.RerankedCostMicrosUSD,
		CostDeltaMicrosUSD:           input.RerankedCostMicrosUSD - input.BaselineCostMicrosUSD,
		Results:                      rerankEvaluationResults(query, baseline, reranked),
	}
	return evaluation, nil
}

// ValidateRerankEvaluation checks guardrails for a reranking eval gate.
func ValidateRerankEvaluation(evaluation RerankEvaluation) error {
	var problems []string
	if strings.TrimSpace(evaluation.QueryID) == "" {
		problems = append(problems, "query_id is required")
	}
	problems = append(problems, validateRerankBaselineEvidence(evaluation.BaselineHybridEvidence)...)
	problems = append(problems, validateEvaluationMetrics("baseline_metrics", evaluation.BaselineMetrics)...)
	problems = append(problems, validateEvaluationMetrics("reranked_metrics", evaluation.RerankedMetrics)...)
	if evaluation.FalseCanonicalCandidateCount > 0 ||
		claimCount(evaluation.BaselineMetrics) > 0 ||
		claimCount(evaluation.RerankedMetrics) > 0 {
		problems = append(problems, "rerank evaluation has false canonical claims")
	}
	if evaluation.BaselineLatency < 0 {
		problems = append(problems, "baseline_latency must be non-negative")
	}
	if evaluation.RerankedLatency < 0 {
		problems = append(problems, "reranked_latency must be non-negative")
	}
	if evaluation.BaselineCostMicrosUSD < 0 {
		problems = append(problems, "baseline_cost_micros_usd must be non-negative")
	}
	if evaluation.RerankedCostMicrosUSD < 0 {
		problems = append(problems, "reranked_cost_micros_usd must be non-negative")
	}
	if len(evaluation.Results) == 0 {
		problems = append(problems, "results are required")
	}
	problems = append(problems, validateRerankEvaluationResults(evaluation.Results)...)
	return joinedValidationError(problems)
}

func validateRerankEvaluationInput(input RerankEvaluationInput) error {
	var problems []string
	problems = append(problems, validateRerankQuery(input.Query)...)
	problems = append(problems, validateRerankBaselineEvidence(input.BaselineHybridEvidence)...)
	if len(input.Baseline) == 0 {
		problems = append(problems, "baseline results are required")
	}
	if len(input.Reranked) == 0 {
		problems = append(problems, "reranked results are required")
	}
	baselineIdentityProblems := validateResultIdentities("baseline", input.Baseline)
	rerankedIdentityProblems := validateResultIdentities("reranked", input.Reranked)
	problems = append(problems, baselineIdentityProblems...)
	problems = append(problems, rerankedIdentityProblems...)
	if len(input.Baseline) > 0 && len(input.Reranked) > 0 &&
		len(baselineIdentityProblems) == 0 &&
		len(rerankedIdentityProblems) == 0 &&
		!sameResultSet(input.Baseline, input.Reranked) {
		problems = append(problems, "reranked results must contain the same document set as baseline")
	}
	if input.BaselineLatency < 0 {
		problems = append(problems, "baseline_latency must be non-negative")
	}
	if input.RerankedLatency < 0 {
		problems = append(problems, "reranked_latency must be non-negative")
	}
	if input.BaselineCostMicrosUSD < 0 {
		problems = append(problems, "baseline_cost_micros_usd must be non-negative")
	}
	if input.RerankedCostMicrosUSD < 0 {
		problems = append(problems, "reranked_cost_micros_usd must be non-negative")
	}
	return joinedValidationError(problems)
}

func validateRerankQuery(query Query) []string {
	var problems []string
	if strings.TrimSpace(query.ID) == "" {
		problems = append(problems, "query.id is required")
	}
	if strings.TrimSpace(query.Text) == "" {
		problems = append(problems, "query.text is required")
	}
	if !validMode(query.Mode) {
		problems = append(problems, "query.mode is invalid")
	}
	if query.Limit <= 0 {
		problems = append(problems, "query.limit is required")
	} else if query.Limit > MaximumQueryLimit {
		problems = append(problems, fmt.Sprintf("query.limit exceeds maximum of %d", MaximumQueryLimit))
	}
	if !queryHasScope(query) {
		problems = append(problems, "query.scope is required")
	}
	if len(nonEmptyHandles(query.ExpectedHandles)) == 0 {
		problems = append(problems, "query.expected_handles are required")
	}
	return problems
}

func validateEvaluationMetrics(prefix string, metrics RetrievalMetrics) []string {
	var problems []string
	for name, value := range map[string]float64{
		"recall":    metrics.Recall,
		"precision": metrics.Precision,
		"ndcg":      metrics.NDCG,
	} {
		if value < 0 || value > 1 {
			problems = append(problems, fmt.Sprintf("%s.%s must be between 0 and 1", prefix, name))
		}
	}
	if metrics.FalseCanonicalClaimCount == nil {
		problems = append(problems, prefix+".false_canonical_claim_count is required")
	} else if *metrics.FalseCanonicalClaimCount < 0 {
		problems = append(problems, prefix+".false_canonical_claim_count must be non-negative")
	}
	return problems
}

func validateResultIdentities(label string, results []Result) []string {
	var problems []string
	seen := make(map[string]int, len(results))
	for i, result := range results {
		key := resultKey(result)
		if key == "" {
			problems = append(problems, fmt.Sprintf("%s[%d].document_id or graph handle is required", label, i))
			continue
		}
		if previous, ok := seen[key]; ok {
			problems = append(problems, fmt.Sprintf("%s[%d].document_id duplicates %s from %s[%d]", label, i, key, label, previous))
			continue
		}
		seen[key] = i
	}
	return problems
}

func sameResultSet(left []Result, right []Result) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, result := range left {
		counts[resultKey(result)]++
	}
	for _, result := range right {
		key := resultKey(result)
		counts[key]--
		if counts[key] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func validateRerankBaselineEvidence(evidence RerankBaselineEvidence) []string {
	var problems []string
	if strings.TrimSpace(evidence.EvidenceID) == "" || evidence.Backend == "" || evidence.Mode == "" {
		return []string{"baseline hybrid evidence is required"}
	}
	if evidence.Backend != BackendNornicDBHybrid {
		problems = append(problems, "baseline_hybrid_evidence.backend must be nornicdb_hybrid")
	}
	if evidence.Mode != ModeHybrid {
		problems = append(problems, "baseline_hybrid_evidence.mode must be hybrid")
	}
	return problems
}

func validateRerankEvaluationResults(results []RerankEvaluationResult) []string {
	var problems []string
	seen := make(map[string]int, len(results))
	for i, result := range results {
		prefix := fmt.Sprintf("results[%d]", i)
		documentID := strings.TrimSpace(result.DocumentID)
		if documentID == "" {
			problems = append(problems, prefix+".document_id is required")
		} else if previous, ok := seen[documentID]; ok {
			problems = append(
				problems,
				fmt.Sprintf("%s.document_id duplicates %s from results[%d]", prefix, documentID, previous),
			)
		} else {
			seen[documentID] = i
		}
		if result.BaselineRank <= 0 {
			problems = append(problems, prefix+".baseline_rank is required")
		}
		if result.RerankedRank <= 0 {
			problems = append(problems, prefix+".reranked_rank is required")
		}
	}
	return problems
}

func rankedResults(results []Result) []Result {
	ranked := append([]Result(nil), results...)
	for i := range ranked {
		if ranked[i].Rank <= 0 {
			ranked[i].Rank = i + 1
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Rank < ranked[j].Rank
	})
	for i := range ranked {
		ranked[i].Rank = i + 1
	}
	return ranked
}

func falseCanonicalCandidateCount(resultSets ...[]Result) int {
	seen := make(map[string]struct{})
	count := 0
	for _, results := range resultSets {
		for _, result := range results {
			if result.Document.TruthScope.Level == searchdocs.TruthLevelDerived {
				continue
			}
			key := resultKey(result)
			if key == "" {
				key = fmt.Sprintf("rank:%d", result.Rank)
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			count++
		}
	}
	return count
}

func rerankEvaluationResults(
	query Query,
	baseline []Result,
	reranked []Result,
) []RerankEvaluationResult {
	expected := expectedHandleSet(query)
	byDocumentID := make(map[string]*RerankEvaluationResult, len(baseline)+len(reranked))
	results := make([]RerankEvaluationResult, 0, len(baseline)+len(reranked))

	for _, result := range baseline {
		key := resultKey(result)
		if key == "" {
			continue
		}
		entry := &RerankEvaluationResult{
			DocumentID:   key,
			BaselineRank: result.Rank,
			Required:     resultContainsExpected(result, expected),
		}
		byDocumentID[key] = entry
		results = append(results, *entry)
	}
	for _, result := range reranked {
		key := resultKey(result)
		if key == "" {
			continue
		}
		entry, ok := byDocumentID[key]
		if !ok {
			entry = &RerankEvaluationResult{
				DocumentID: key,
				Required:   resultContainsExpected(result, expected),
			}
			byDocumentID[key] = entry
			results = append(results, *entry)
		}
		entry.RerankedRank = result.Rank
	}

	for i := range results {
		if entry, ok := byDocumentID[results[i].DocumentID]; ok {
			results[i] = *entry
		}
	}
	return results
}

func resultKey(result Result) string {
	if id := strings.TrimSpace(result.Document.ID); id != "" {
		return id
	}
	for _, handle := range result.Document.GraphHandles {
		if key := handleKey(handle); key != "" {
			return key
		}
	}
	return ""
}

func claimCount(metrics RetrievalMetrics) int {
	if metrics.FalseCanonicalClaimCount == nil {
		return 0
	}
	return *metrics.FalseCanonicalClaimCount
}
