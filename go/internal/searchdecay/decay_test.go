// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchdecay

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestScoreAppliesDecayToEligibleNonCanonicalEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 6, 30, 0, 0, time.UTC)
	decision, err := Scorer{Policy: Policy{
		ID:       "freshness-v1",
		Now:      now,
		HalfLife: 24 * time.Hour,
		MinScore: 0.1,
	}}.Score(context.Background(), Evidence{
		ID:         "ci-run:123",
		Class:      EvidenceClassCIRun,
		TruthLevel: searchdocs.TruthLevelDerived,
		ObservedAt: now.Add(-48 * time.Hour),
		Score:      0.8,
	})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	if got, want := decision.Outcome, OutcomeApplied; got != want {
		t.Fatalf("decision.Outcome = %q, want %q", got, want)
	}
	if got, want := decision.Score, 0.2; got != want {
		t.Fatalf("decision.Score = %v, want %v", got, want)
	}
	if got, want := decision.Age, 48*time.Hour; got != want {
		t.Fatalf("decision.Age = %s, want %s", got, want)
	}
}

func TestScoreMinScoreDoesNotIncreaseOriginalScore(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 6, 30, 0, 0, time.UTC)
	decision, err := Scorer{Policy: Policy{
		ID:       "freshness-v1",
		Now:      now,
		HalfLife: 24 * time.Hour,
		MinScore: 0.1,
	}}.Score(context.Background(), Evidence{
		ID:         "ci-run:low-score",
		Class:      EvidenceClassCIRun,
		TruthLevel: searchdocs.TruthLevelDerived,
		ObservedAt: now.Add(-72 * time.Hour),
		Score:      0.05,
	})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	if got, want := decision.Score, 0.05; got != want {
		t.Fatalf("decision.Score = %v, want %v", got, want)
	}
	if decision.Score > decision.OriginalScore {
		t.Fatalf("decision.Score = %v, original = %v; decay must not increase score", decision.Score, decision.OriginalScore)
	}
}

func TestScoreSkipsCanonicalEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 6, 30, 0, 0, time.UTC)
	decision, err := Scorer{Policy: validPolicy(now)}.Score(context.Background(), Evidence{
		ID:         "service:checkout",
		Class:      EvidenceClassCanonicalGraph,
		TruthLevel: searchdocs.TruthLevel("canonical"),
		ObservedAt: now.Add(-720 * time.Hour),
		Score:      0.9,
	})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	if got, want := decision.Outcome, OutcomeSkippedCanonical; got != want {
		t.Fatalf("decision.Outcome = %q, want %q", got, want)
	}
	if got, want := decision.Score, 0.9; got != want {
		t.Fatalf("decision.Score = %v, want %v", got, want)
	}
}

func TestScoreSkipsDurableRelationships(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 6, 30, 0, 0, time.UTC)
	decision, err := Scorer{Policy: validPolicy(now)}.Score(context.Background(), Evidence{
		ID:         "rel:service-owns-repo",
		Class:      EvidenceClassDurableRelationship,
		TruthLevel: searchdocs.TruthLevelDerived,
		ObservedAt: now.Add(-720 * time.Hour),
		Score:      0.7,
	})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	if got, want := decision.Outcome, OutcomeSkippedCanonical; got != want {
		t.Fatalf("decision.Outcome = %q, want %q", got, want)
	}
	if got, want := decision.Score, 0.7; got != want {
		t.Fatalf("decision.Score = %v, want %v", got, want)
	}
}

func TestScoreSkipsIneligibleEvidenceClass(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 6, 30, 0, 0, time.UTC)
	decision, err := Scorer{Policy: validPolicy(now)}.Score(context.Background(), Evidence{
		ID:         "log-line:1",
		Class:      EvidenceClass("log_line"),
		TruthLevel: searchdocs.TruthLevelDerived,
		ObservedAt: now.Add(-24 * time.Hour),
		Score:      0.6,
	})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	if got, want := decision.Outcome, OutcomeSkippedIneligible; got != want {
		t.Fatalf("decision.Outcome = %q, want %q", got, want)
	}
	if got, want := decision.Score, 0.6; got != want {
		t.Fatalf("decision.Score = %v, want %v", got, want)
	}
}

func TestScoreRejectsInvalidPolicyAndInput(t *testing.T) {
	t.Parallel()

	observer := &recordingObserver{}
	_, err := Scorer{
		Policy:   Policy{ID: "bad-policy"},
		Observer: observer,
	}.Score(context.Background(), Evidence{
		ID:         "ci-run:bad",
		Class:      EvidenceClassCIRun,
		TruthLevel: searchdocs.TruthLevelDerived,
		Score:      1.2,
	})

	if err == nil {
		t.Fatal("Score() error = nil, want validation error")
	}
	for _, want := range []string{
		"policy.half_life is required",
		"evidence.observed_at is required",
		"evidence.score must be between 0 and 1",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Score() error = %q, want substring %q", err, want)
		}
	}
	obs := observer.only(t)
	if got, want := obs.Outcome, OutcomeRejectedInvalid; got != want {
		t.Fatalf("obs.Outcome = %q, want %q", got, want)
	}
}

func TestScoreRejectsMissingTruthLevel(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 6, 30, 0, 0, time.UTC)
	_, err := Scorer{
		Policy: validPolicy(now),
	}.Score(context.Background(), Evidence{
		ID:         "ci-run:missing-truth",
		Class:      EvidenceClassCIRun,
		ObservedAt: now.Add(-12 * time.Hour),
		Score:      0.8,
	})

	if err == nil {
		t.Fatal("Score() error = nil, want validation error")
	}
	if want := "evidence.truth_level is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("Score() error = %q, want substring %q", err, want)
	}
}

func TestScoreRecordsObservationByPolicyClassAndOutcome(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 6, 30, 0, 0, time.UTC)
	observer := &recordingObserver{}
	_, err := Scorer{
		Policy:   validPolicy(now),
		Observer: observer,
	}.Score(context.Background(), Evidence{
		ID:         "vuln:1",
		Class:      EvidenceClassVulnerabilityObservation,
		TruthLevel: searchdocs.TruthLevelDerived,
		ObservedAt: now.Add(-12 * time.Hour),
		Score:      0.8,
	})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	obs := observer.only(t)
	if got, want := obs.PolicyID, "freshness-v1"; got != want {
		t.Fatalf("obs.PolicyID = %q, want %q", got, want)
	}
	if got, want := obs.EvidenceClass, EvidenceClassVulnerabilityObservation; got != want {
		t.Fatalf("obs.EvidenceClass = %q, want %q", got, want)
	}
	if got, want := obs.Outcome, OutcomeApplied; got != want {
		t.Fatalf("obs.Outcome = %q, want %q", got, want)
	}
}

func validPolicy(now time.Time) Policy {
	return Policy{
		ID:       "freshness-v1",
		Now:      now,
		HalfLife: 24 * time.Hour,
		MinScore: 0.1,
	}
}

type recordingObserver struct {
	observations []Observation
}

func (observer *recordingObserver) ObserveDecay(ctx context.Context, observation Observation) {
	observer.observations = append(observer.observations, observation)
}

func (observer *recordingObserver) only(t *testing.T) Observation {
	t.Helper()
	if got, want := len(observer.observations), 1; got != want {
		t.Fatalf("observations = %d, want %d", got, want)
	}
	return observer.observations[0]
}
