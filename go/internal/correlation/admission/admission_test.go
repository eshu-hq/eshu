package admission

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
)

func TestEvaluatePromotesCandidateAtOrAboveThreshold(t *testing.T) {
	t.Parallel()

	candidate := model.Candidate{
		ID:             "candidate-1",
		Kind:           "deployable_unit",
		CorrelationKey: "sample-service",
		State:          model.CandidateStateProvisional,
		Confidence:     0.8,
	}

	got, outcome, err := Evaluate(candidate, 0.75, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	if got.State != model.CandidateStateAdmitted {
		t.Fatalf("State = %q, want %q", got.State, model.CandidateStateAdmitted)
	}
	if !outcome.MeetsConfidence || !outcome.MeetsStructure {
		t.Fatalf("outcome = %#v, want confidence and structure gates satisfied", outcome)
	}
}

func TestEvaluateRejectsCandidateBelowThresholdButPreservesQueryability(t *testing.T) {
	t.Parallel()

	candidate := model.Candidate{
		ID:             "candidate-2",
		Kind:           "deployable_unit",
		CorrelationKey: "sample-service",
		State:          model.CandidateStateProvisional,
		Confidence:     0.42,
	}

	got, outcome, err := Evaluate(candidate, 0.75, nil)
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	if got.State != model.CandidateStateRejected {
		t.Fatalf("State = %q, want %q", got.State, model.CandidateStateRejected)
	}
	if outcome.MeetsConfidence {
		t.Fatalf("outcome.MeetsConfidence = true, want false")
	}
}

func TestEvaluateRejectsOutOfRangeThreshold(t *testing.T) {
	t.Parallel()

	candidate := model.Candidate{
		ID:             "candidate-3",
		Kind:           "deployable_unit",
		CorrelationKey: "sample-service",
		State:          model.CandidateStateProvisional,
		Confidence:     0.9,
	}

	if _, _, err := Evaluate(candidate, 1.25, nil); err == nil {
		t.Fatal("Evaluate() error = nil, want non-nil")
	}
}

func TestEvaluateRejectsCandidateThatMissesRequiredEvidenceStructure(t *testing.T) {
	t.Parallel()

	candidate := model.Candidate{
		ID:             "candidate-4",
		Kind:           "deployable_unit",
		CorrelationKey: "sample-service",
		State:          model.CandidateStateProvisional,
		Confidence:     0.92,
		Evidence: []model.EvidenceAtom{
			{
				ID:           "ev-1",
				SourceSystem: "git",
				EvidenceType: "dockerfile",
				ScopeID:      "repo:sample-service",
				Key:          "repository",
				Value:        "sample-service",
				Confidence:   0.92,
			},
		},
	}

	requirements := []rules.EvidenceRequirement{
		{
			Name:     "runtime-image",
			MinCount: 1,
			MatchAll: []rules.EvidenceSelector{
				{Field: rules.EvidenceFieldEvidenceType, Value: "dockerfile"},
				{Field: rules.EvidenceFieldKey, Value: "image"},
			},
		},
	}

	got, outcome, err := Evaluate(candidate, 0.75, requirements)
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	if got.State != model.CandidateStateRejected {
		t.Fatalf("State = %q, want %q", got.State, model.CandidateStateRejected)
	}
	if !outcome.MeetsConfidence {
		t.Fatalf("outcome.MeetsConfidence = false, want true")
	}
	if outcome.MeetsStructure {
		t.Fatalf("outcome.MeetsStructure = true, want false")
	}
}
