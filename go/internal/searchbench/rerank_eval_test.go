// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestScoreRerankEvaluationRecordsRankingImprovement(t *testing.T) {
	t.Parallel()

	evaluation, err := ScoreRerankEvaluation(RerankEvaluationInput{
		Query: rerankQuery(1),
		Baseline: []Result{
			rerankResult("service:payments", 1),
			rerankResult("service:checkout", 2),
		},
		Reranked: []Result{
			rerankResult("service:checkout", 1),
			rerankResult("service:payments", 2),
		},
		BaselineHybridEvidence: rerankBaselineHybridEvidence(),
		BaselineLatency:        60 * time.Millisecond,
		RerankedLatency:        85 * time.Millisecond,
		BaselineCostMicrosUSD:  120,
		RerankedCostMicrosUSD:  180,
	})
	if err != nil {
		t.Fatalf("ScoreRerankEvaluation() error = %v, want nil", err)
	}

	if got, want := evaluation.QueryID, "q-rerank"; got != want {
		t.Fatalf("evaluation.QueryID = %q, want %q", got, want)
	}
	if got, want := evaluation.BaselineMetrics.NDCG, 0.0; got != want {
		t.Fatalf("BaselineMetrics.NDCG = %v, want %v", got, want)
	}
	if got, want := evaluation.RerankedMetrics.NDCG, 1.0; got != want {
		t.Fatalf("RerankedMetrics.NDCG = %v, want %v", got, want)
	}
	if evaluation.NDCGDelta <= 0 {
		t.Fatalf("NDCGDelta = %v, want ranking improvement", evaluation.NDCGDelta)
	}
	if got, want := evaluation.LatencyDelta, 25*time.Millisecond; got != want {
		t.Fatalf("LatencyDelta = %v, want %v", got, want)
	}
	if got, want := evaluation.CostDeltaMicrosUSD, int64(60); got != want {
		t.Fatalf("CostDeltaMicrosUSD = %d, want %d", got, want)
	}
	if got, want := evaluation.FalseCanonicalCandidateCount, 0; got != want {
		t.Fatalf("FalseCanonicalCandidateCount = %d, want %d", got, want)
	}
	if err := ValidateRerankEvaluation(evaluation); err != nil {
		t.Fatalf("ValidateRerankEvaluation() error = %v, want nil", err)
	}
}

func TestValidateRerankEvaluationRejectsMissingBaselineHybridEvidence(t *testing.T) {
	t.Parallel()

	evaluation, err := ScoreRerankEvaluation(validRerankEvaluationInput())
	if err != nil {
		t.Fatalf("ScoreRerankEvaluation() error = %v, want nil", err)
	}
	evaluation.BaselineHybridEvidence = RerankBaselineEvidence{}

	err = ValidateRerankEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateRerankEvaluation() error = nil, want missing baseline evidence failure")
	}
	if want := "baseline hybrid evidence is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateRerankEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestValidateRerankEvaluationRejectsFalseCanonicalClaims(t *testing.T) {
	t.Parallel()

	input := validRerankEvaluationInput()
	input.Baseline = append(input.Baseline, rerankResultWithTruth(
		"service:ledger",
		3,
		searchdocs.TruthLevel("canonical"),
	))
	input.Reranked = append(input.Reranked, rerankResultWithTruth(
		"service:ledger",
		3,
		searchdocs.TruthLevel("canonical"),
	))

	evaluation, err := ScoreRerankEvaluation(input)
	if err != nil {
		t.Fatalf("ScoreRerankEvaluation() error = %v, want nil", err)
	}
	if got, want := evaluation.FalseCanonicalCandidateCount, 1; got != want {
		t.Fatalf("FalseCanonicalCandidateCount = %d, want %d", got, want)
	}

	err = ValidateRerankEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateRerankEvaluation() error = nil, want false canonical failure")
	}
	if want := "rerank evaluation has false canonical claims"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateRerankEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestScoreRerankEvaluationRejectsCandidateSetDrift(t *testing.T) {
	t.Parallel()

	input := validRerankEvaluationInput()
	input.Reranked = input.Reranked[:1]

	_, err := ScoreRerankEvaluation(input)
	if err == nil {
		t.Fatal("ScoreRerankEvaluation() error = nil, want candidate-set drift failure")
	}
	if want := "reranked results must contain the same document set as baseline"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ScoreRerankEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestScoreRerankEvaluationRejectsDuplicateCandidateIdentity(t *testing.T) {
	t.Parallel()

	input := validRerankEvaluationInput()
	input.Baseline = append(input.Baseline, rerankResult("service:payments", 3))

	_, err := ScoreRerankEvaluation(input)
	if err == nil {
		t.Fatal("ScoreRerankEvaluation() error = nil, want duplicate identity failure")
	}
	if want := "baseline[2].document_id duplicates searchdoc:service:payments from baseline[0]"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ScoreRerankEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestScoreRerankEvaluationRejectsInvalidLatencyAndCost(t *testing.T) {
	t.Parallel()

	input := validRerankEvaluationInput()
	input.BaselineLatency = -1
	input.RerankedCostMicrosUSD = -1

	_, err := ScoreRerankEvaluation(input)
	if err == nil {
		t.Fatal("ScoreRerankEvaluation() error = nil, want invalid latency and cost failure")
	}
	for _, want := range []string{
		"baseline_latency must be non-negative",
		"reranked_cost_micros_usd must be non-negative",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ScoreRerankEvaluation() error = %q, want substring %q", err, want)
		}
	}
}

func TestScoreRerankEvaluationRejectsQueryLimitAboveMaximum(t *testing.T) {
	t.Parallel()

	input := validRerankEvaluationInput()
	input.Query.Limit = MaximumQueryLimit + 1

	_, err := ScoreRerankEvaluation(input)
	if err == nil {
		t.Fatal("ScoreRerankEvaluation() error = nil, want max-limit error")
	}
	if want := "query.limit exceeds maximum of 100"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ScoreRerankEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestValidateRerankEvaluationRejectsIncompleteResultRanks(t *testing.T) {
	t.Parallel()

	evaluation, err := ScoreRerankEvaluation(validRerankEvaluationInput())
	if err != nil {
		t.Fatalf("ScoreRerankEvaluation() error = %v, want nil", err)
	}
	evaluation.Results[0].RerankedRank = 0

	err = ValidateRerankEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateRerankEvaluation() error = nil, want missing reranked rank failure")
	}
	if want := "results[0].reranked_rank is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateRerankEvaluation() error = %q, want substring %q", err, want)
	}
}

func validRerankEvaluationInput() RerankEvaluationInput {
	return RerankEvaluationInput{
		Query: rerankQuery(1),
		Baseline: []Result{
			rerankResult("service:payments", 1),
			rerankResult("service:checkout", 2),
		},
		Reranked: []Result{
			rerankResult("service:checkout", 1),
			rerankResult("service:payments", 2),
		},
		BaselineHybridEvidence: rerankBaselineHybridEvidence(),
		BaselineLatency:        60 * time.Millisecond,
		RerankedLatency:        85 * time.Millisecond,
		BaselineCostMicrosUSD:  120,
		RerankedCostMicrosUSD:  180,
	}
}

func rerankQuery(limit int) Query {
	return Query{
		ID:              "q-rerank",
		Text:            "Which service owns checkout?",
		RepoID:          "repo-checkout",
		Mode:            ModeHybrid,
		Limit:           limit,
		ExpectedHandles: []string{"service:checkout"},
	}
}

func rerankBaselineHybridEvidence() RerankBaselineEvidence {
	return RerankBaselineEvidence{
		EvidenceID: "search-benchmark:hybrid:v1",
		Backend:    BackendNornicDBHybrid,
		Mode:       ModeHybrid,
	}
}

func rerankResult(handle string, rank int) Result {
	return rerankResultWithTruth(handle, rank, searchdocs.TruthLevelDerived)
}

func rerankResultWithTruth(handle string, rank int, truthLevel searchdocs.TruthLevel) Result {
	kind, id, _ := strings.Cut(handle, ":")
	return Result{
		Document: searchdocs.Document{
			ID:           "searchdoc:" + handle,
			GraphHandles: []searchdocs.GraphHandle{{Kind: kind, ID: id}},
			TruthScope: searchdocs.TruthScope{
				Level: truthLevel,
				Basis: searchdocs.TruthBasisContentIndex,
			},
		},
		Rank: rank,
	}
}
