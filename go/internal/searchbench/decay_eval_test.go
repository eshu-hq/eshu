// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdecay"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestScoreDecayEvaluationRecordsRankingImprovement(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 7, 0, 0, 0, time.UTC)
	evaluation, err := ScoreDecayEvaluation(context.Background(), DecayEvaluationInput{
		Query: decayQuery(1),
		Candidates: []DecayCandidate{
			decayCandidate("service:payments", 1, searchdecay.Evidence{
				ID:         "ci-run:old-payments",
				Class:      searchdecay.EvidenceClassCIRun,
				TruthLevel: searchdocs.TruthLevelDerived,
				ObservedAt: now.Add(-72 * time.Hour),
				Score:      0.95,
			}),
			decayCandidate("service:checkout", 2, searchdecay.Evidence{
				ID:         "deploy:fresh-checkout",
				Class:      searchdecay.EvidenceClassDeploymentEvent,
				TruthLevel: searchdocs.TruthLevelDerived,
				ObservedAt: now.Add(-1 * time.Hour),
				Score:      0.70,
			}),
		},
		Scorer: decayScorer(now),
	})
	if err != nil {
		t.Fatalf("ScoreDecayEvaluation() error = %v, want nil", err)
	}

	if got, want := evaluation.QueryID, "q-decay"; got != want {
		t.Fatalf("evaluation.QueryID = %q, want %q", got, want)
	}
	if got, want := evaluation.PolicyID, "decay-eval-v1"; got != want {
		t.Fatalf("evaluation.PolicyID = %q, want %q", got, want)
	}
	if evaluation.RequiredEvidenceVisibleBefore {
		t.Fatal("RequiredEvidenceVisibleBefore = true, want false baseline miss")
	}
	if !evaluation.RequiredEvidenceVisibleAfter {
		t.Fatal("RequiredEvidenceVisibleAfter = false, want true after decay")
	}
	if got, want := evaluation.MetricsBefore.NDCG, 0.0; got != want {
		t.Fatalf("MetricsBefore.NDCG = %v, want %v", got, want)
	}
	if got, want := evaluation.MetricsAfter.NDCG, 1.0; got != want {
		t.Fatalf("MetricsAfter.NDCG = %v, want %v", got, want)
	}
	if evaluation.NDCGDelta <= 0 {
		t.Fatalf("NDCGDelta = %v, want ranking improvement", evaluation.NDCGDelta)
	}
	checkout := evaluationResultFor(t, evaluation, "searchdoc:service:checkout")
	if got, want := checkout.OriginalRank, 2; got != want {
		t.Fatalf("checkout.OriginalRank = %d, want %d", got, want)
	}
	if got, want := checkout.DecayedRank, 1; got != want {
		t.Fatalf("checkout.DecayedRank = %d, want %d", got, want)
	}
	if got, want := checkout.DecayOutcome, searchdecay.OutcomeApplied; got != want {
		t.Fatalf("checkout.DecayOutcome = %q, want %q", got, want)
	}
	if err := ValidateDecayEvaluation(evaluation); err != nil {
		t.Fatalf("ValidateDecayEvaluation() error = %v, want nil", err)
	}
}

func TestValidateDecayEvaluationRejectsHiddenRequiredEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 7, 0, 0, 0, time.UTC)
	evaluation, err := ScoreDecayEvaluation(context.Background(), DecayEvaluationInput{
		Query: decayQuery(1),
		Candidates: []DecayCandidate{
			decayCandidate("service:checkout", 1, searchdecay.Evidence{
				ID:         "deploy:old-checkout",
				Class:      searchdecay.EvidenceClassDeploymentEvent,
				TruthLevel: searchdocs.TruthLevelDerived,
				ObservedAt: now.Add(-96 * time.Hour),
				Score:      0.80,
			}),
			decayCandidate("service:payments", 2, searchdecay.Evidence{
				ID:         "deploy:fresh-payments",
				Class:      searchdecay.EvidenceClassDeploymentEvent,
				TruthLevel: searchdocs.TruthLevelDerived,
				ObservedAt: now.Add(-1 * time.Hour),
				Score:      0.70,
			}),
		},
		Scorer: decayScorer(now),
	})
	if err != nil {
		t.Fatalf("ScoreDecayEvaluation() error = %v, want nil", err)
	}
	if !evaluation.RequiredEvidenceVisibleBefore {
		t.Fatal("RequiredEvidenceVisibleBefore = false, want baseline visibility")
	}
	if evaluation.RequiredEvidenceVisibleAfter {
		t.Fatal("RequiredEvidenceVisibleAfter = true, want hidden after decay")
	}

	err = ValidateDecayEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateDecayEvaluation() error = nil, want hidden evidence failure")
	}
	if want := "required evidence was hidden after decay"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateDecayEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestValidateDecayEvaluationRejectsMissingRequiredEvidenceAfterDecay(t *testing.T) {
	t.Parallel()

	falseCanonicalClaims := 0
	err := ValidateDecayEvaluation(DecayEvaluation{
		QueryID:  "q-decay",
		PolicyID: "decay-eval-v1",
		MetricsBefore: RetrievalMetrics{
			FalseCanonicalClaimCount: &falseCanonicalClaims,
		},
		MetricsAfter: RetrievalMetrics{
			FalseCanonicalClaimCount: &falseCanonicalClaims,
		},
		Results: []DecayEvaluationResult{{
			DocumentID:   "searchdoc:service:payments",
			OriginalRank: 1,
			DecayedRank:  1,
		}},
	})
	if err == nil {
		t.Fatal("ValidateDecayEvaluation() error = nil, want missing required evidence failure")
	}
	if want := "required evidence was not visible after decay"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateDecayEvaluation() error = %q, want substring %q", err, want)
	}
}

func TestValidateDecayEvaluationRejectsFalseCanonicalClaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 7, 0, 0, 0, time.UTC)
	evaluation, err := ScoreDecayEvaluation(context.Background(), DecayEvaluationInput{
		Query: decayQuery(1),
		Candidates: []DecayCandidate{
			decayCandidateWithTruth("service:payments", 1, searchdocs.TruthLevel("canonical"), searchdecay.Evidence{
				ID:         "canonical:payments",
				Class:      searchdecay.EvidenceClassCanonicalGraph,
				TruthLevel: searchdocs.TruthLevel("canonical"),
				ObservedAt: now.Add(-96 * time.Hour),
				Score:      0.99,
			}),
			decayCandidate("service:checkout", 2, searchdecay.Evidence{
				ID:         "deploy:fresh-checkout",
				Class:      searchdecay.EvidenceClassDeploymentEvent,
				TruthLevel: searchdocs.TruthLevelDerived,
				ObservedAt: now.Add(-1 * time.Hour),
				Score:      0.70,
			}),
		},
		Scorer: decayScorer(now),
	})
	if err != nil {
		t.Fatalf("ScoreDecayEvaluation() error = %v, want nil", err)
	}
	if got, want := evaluation.FalseCanonicalCandidateCount, 1; got != want {
		t.Fatalf("FalseCanonicalCandidateCount = %d, want %d", got, want)
	}
	canonical := evaluationResultFor(t, evaluation, "searchdoc:service:payments")
	if got, want := canonical.DecayOutcome, searchdecay.OutcomeSkippedCanonical; got != want {
		t.Fatalf("canonical.DecayOutcome = %q, want %q", got, want)
	}

	err = ValidateDecayEvaluation(evaluation)
	if err == nil {
		t.Fatal("ValidateDecayEvaluation() error = nil, want false canonical failure")
	}
	if want := "false canonical claims"; !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateDecayEvaluation() error = %q, want substring %q", err, want)
	}
}

func decayQuery(limit int) Query {
	return Query{
		ID:              "q-decay",
		Text:            "Which service owns checkout?",
		RepoID:          "repo-checkout",
		Mode:            ModeHybrid,
		Limit:           limit,
		ExpectedHandles: []string{"service:checkout"},
	}
}

func decayScorer(now time.Time) searchdecay.Scorer {
	return searchdecay.Scorer{
		Policy: searchdecay.Policy{
			ID:       "decay-eval-v1",
			Now:      now,
			HalfLife: 24 * time.Hour,
			MinScore: 0,
		},
	}
}

func decayCandidate(handle string, rank int, evidence searchdecay.Evidence) DecayCandidate {
	return decayCandidateWithTruth(handle, rank, searchdocs.TruthLevelDerived, evidence)
}

func decayCandidateWithTruth(
	handle string,
	rank int,
	truthLevel searchdocs.TruthLevel,
	evidence searchdecay.Evidence,
) DecayCandidate {
	kind, id, _ := strings.Cut(handle, ":")
	return DecayCandidate{
		Result: Result{
			Document: searchdocs.Document{
				ID:           "searchdoc:" + handle,
				GraphHandles: []searchdocs.GraphHandle{{Kind: kind, ID: id}},
				TruthScope:   searchdocs.TruthScope{Level: truthLevel, Basis: searchdocs.TruthBasisContentIndex},
			},
			Rank: rank,
		},
		Evidence: evidence,
	}
}

func evaluationResultFor(t *testing.T, evaluation DecayEvaluation, documentID string) DecayEvaluationResult {
	t.Helper()
	for _, result := range evaluation.Results {
		if result.DocumentID == documentID {
			return result
		}
	}
	t.Fatalf("DecayEvaluationResult for %q missing", documentID)
	return DecayEvaluationResult{}
}
