// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linkcandidates

import (
	"strings"
	"testing"
)

func TestEvaluateCandidatesRecordsGapDiscoveryMetrics(t *testing.T) {
	t.Parallel()

	match := validCandidate()
	match.Source = GraphHandle{Kind: "service", ID: "checkout"}
	match.Target = GraphHandle{Kind: "service", ID: "payments"}

	falsePositive := validCandidate()
	falsePositive.ID = "candidate:false-positive"
	falsePositive.Target = GraphHandle{Kind: "service", ID: "billing"}

	evaluation, err := EvaluateCandidates(EvaluationInput{
		ExpectedGaps: []ExpectedGap{
			{
				Source: GraphHandle{Kind: "service", ID: "checkout"},
				Target: GraphHandle{Kind: "service", ID: "payments"},
			},
			{
				Source: GraphHandle{Kind: "service", ID: "checkout"},
				Target: GraphHandle{Kind: "service", ID: "inventory"},
			},
		},
		Candidates: []Candidate{match, falsePositive},
	})
	if err != nil {
		t.Fatalf("EvaluateCandidates() error = %v, want nil", err)
	}

	if got, want := evaluation.CandidateCount, 2; got != want {
		t.Fatalf("CandidateCount = %d, want %d", got, want)
	}
	if got, want := evaluation.GeneratedCount, 2; got != want {
		t.Fatalf("GeneratedCount = %d, want %d", got, want)
	}
	if got, want := evaluation.MatchedExpectedGapCount, 1; got != want {
		t.Fatalf("MatchedExpectedGapCount = %d, want %d", got, want)
	}
	if got, want := evaluation.FalsePositiveCount, 1; got != want {
		t.Fatalf("FalsePositiveCount = %d, want %d", got, want)
	}
	if got, want := evaluation.Precision, 0.5; got != want {
		t.Fatalf("Precision = %v, want %v", got, want)
	}
	if got, want := evaluation.Recall, 0.5; got != want {
		t.Fatalf("Recall = %v, want %v", got, want)
	}
	if got, want := decisionCountFor(evaluation, "nornicdb.adamic_adar", DecisionGenerated), 2; got != want {
		t.Fatalf("generated count = %d, want %d", got, want)
	}
}

func TestEvaluateCandidatesCountsDuplicateGeneratedMatchesOnce(t *testing.T) {
	t.Parallel()

	first := validCandidate()
	first.ID = "candidate:first"
	duplicate := validCandidate()
	duplicate.ID = "candidate:duplicate"

	evaluation, err := EvaluateCandidates(EvaluationInput{
		ExpectedGaps: []ExpectedGap{{
			Source: first.Source,
			Target: first.Target,
		}},
		Candidates: []Candidate{first, duplicate},
	})
	if err != nil {
		t.Fatalf("EvaluateCandidates() error = %v, want nil", err)
	}
	if got, want := evaluation.MatchedExpectedGapCount, 1; got != want {
		t.Fatalf("MatchedExpectedGapCount = %d, want %d", got, want)
	}
	if got, want := evaluation.FalsePositiveCount, 1; got != want {
		t.Fatalf("FalsePositiveCount = %d, want duplicate false positive", got)
	}
	if got, want := evaluation.Precision, 0.5; got != want {
		t.Fatalf("Precision = %v, want %v", got, want)
	}
}

func TestEvaluateCandidatesCountsSuppressedAndAmbiguousCandidates(t *testing.T) {
	t.Parallel()

	suppressed := validCandidate()
	suppressed.ID = "candidate:suppressed"
	suppressed.Decision = DecisionSuppressed

	ambiguous := validCandidate()
	ambiguous.ID = "candidate:ambiguous"
	ambiguous.Decision = DecisionAmbiguous

	evaluation, err := EvaluateCandidates(EvaluationInput{
		ExpectedGaps: []ExpectedGap{{
			Source: GraphHandle{Kind: "service", ID: "checkout"},
			Target: GraphHandle{Kind: "service", ID: "payments"},
		}},
		Candidates: []Candidate{suppressed, ambiguous},
	})
	if err != nil {
		t.Fatalf("EvaluateCandidates() error = %v, want nil", err)
	}

	if got, want := evaluation.CandidateCount, 2; got != want {
		t.Fatalf("CandidateCount = %d, want %d", got, want)
	}
	if got, want := evaluation.SuppressedCount, 1; got != want {
		t.Fatalf("SuppressedCount = %d, want %d", got, want)
	}
	if got, want := evaluation.AmbiguousCount, 1; got != want {
		t.Fatalf("AmbiguousCount = %d, want %d", got, want)
	}
	if got, want := evaluation.GeneratedCount, 0; got != want {
		t.Fatalf("GeneratedCount = %d, want %d", got, want)
	}
	if got, want := decisionCountFor(evaluation, "nornicdb.adamic_adar", DecisionSuppressed), 1; got != want {
		t.Fatalf("suppressed count = %d, want %d", got, want)
	}
	if got, want := decisionCountFor(evaluation, "nornicdb.adamic_adar", DecisionAmbiguous), 1; got != want {
		t.Fatalf("ambiguous count = %d, want %d", got, want)
	}
}

func TestEvaluateCandidatesRejectsMissingExpectedGaps(t *testing.T) {
	t.Parallel()

	_, err := EvaluateCandidates(EvaluationInput{
		Candidates: []Candidate{validCandidate()},
	})
	if err == nil {
		t.Fatal("EvaluateCandidates() error = nil, want expected gap error")
	}
	if want := "expected_gaps are required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("EvaluateCandidates() error = %q, want substring %q", err, want)
	}
}

func TestEvaluateCandidatesRejectsInvalidCandidate(t *testing.T) {
	t.Parallel()

	candidate := validCandidate()
	candidate.TruthLevel = TruthLevel("canonical")

	_, err := EvaluateCandidates(EvaluationInput{
		ExpectedGaps: []ExpectedGap{{
			Source: GraphHandle{Kind: "service", ID: "checkout"},
			Target: GraphHandle{Kind: "service", ID: "payments"},
		}},
		Candidates: []Candidate{candidate},
	})
	if err == nil {
		t.Fatal("EvaluateCandidates() error = nil, want candidate validation error")
	}
	for _, want := range []string{"candidates[0]", "truth_level must be candidate or semantic_candidate"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("EvaluateCandidates() error = %q, want substring %q", err, want)
		}
	}
}

func decisionCountFor(evaluation Evaluation, algorithm string, decision Decision) int {
	for _, count := range evaluation.DecisionCounts {
		if count.Algorithm == algorithm && count.Decision == decision {
			return count.Count
		}
	}
	return 0
}
