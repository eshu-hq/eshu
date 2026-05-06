package model

import "testing"

func TestCandidateStateValidate(t *testing.T) {
	t.Parallel()

	for _, state := range []CandidateState{
		CandidateStateRejected,
		CandidateStateProvisional,
		CandidateStateAdmitted,
	} {
		if err := state.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v, want nil", state, err)
		}
	}
}

func TestCandidateStateValidateRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	if err := CandidateState("mystery").Validate(); err == nil {
		t.Fatal("Validate(mystery) error = nil, want non-nil")
	}
}

func TestCandidateValidateRequiresConfidenceWithinUnitInterval(t *testing.T) {
	t.Parallel()

	candidate := Candidate{
		ID:             "candidate-1",
		Kind:           "deployable_unit",
		CorrelationKey: "sample-service",
		State:          CandidateStateProvisional,
		Confidence:     1.2,
	}

	if err := candidate.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestCandidateValidateRequiresCorrelationKey(t *testing.T) {
	t.Parallel()

	candidate := Candidate{
		ID:         "candidate-1",
		Kind:       "deployable_unit",
		State:      CandidateStateProvisional,
		Confidence: 0.8,
	}

	if err := candidate.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}
