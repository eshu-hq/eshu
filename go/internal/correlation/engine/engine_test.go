package engine

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
)

func TestEvaluateOrdersRulesDeterministicallyByPriorityThenName(t *testing.T) {
	t.Parallel()

	pack := rules.RulePack{
		Name:                   "container_core",
		MinAdmissionConfidence: 0.75,
		Rules: []rules.Rule{
			{Name: "z-last", Kind: rules.RuleKindExplain, Priority: 20},
			{Name: "b-second", Kind: rules.RuleKindMatch, Priority: 10},
			{Name: "a-first", Kind: rules.RuleKindExtractKey, Priority: 10},
		},
	}

	evaluation, err := Evaluate(pack, []model.Candidate{newCandidate("candidate-1", "sample-service", 0.9, nil)})
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}

	want := []string{"a-first", "b-second", "z-last"}
	if len(evaluation.OrderedRuleNames) != len(want) {
		t.Fatalf("len(OrderedRuleNames) = %d, want %d", len(evaluation.OrderedRuleNames), len(want))
	}
	for i := range want {
		if evaluation.OrderedRuleNames[i] != want[i] {
			t.Fatalf("OrderedRuleNames[%d] = %q, want %q", i, evaluation.OrderedRuleNames[i], want[i])
		}
	}
}

func TestEvaluateBoundsMatchFanOut(t *testing.T) {
	t.Parallel()

	pack := rules.RulePack{
		Name:                   "container_core",
		MinAdmissionConfidence: 0.75,
		Rules: []rules.Rule{
			{Name: "match-image", Kind: rules.RuleKindMatch, Priority: 1, MaxMatches: 2},
		},
	}

	candidate := newCandidate("candidate-1", "sample-service", 0.9, []model.EvidenceAtom{
		newEvidence("ev-1"),
		newEvidence("ev-2"),
		newEvidence("ev-3"),
	})

	evaluation, err := Evaluate(pack, []model.Candidate{candidate})
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	if len(evaluation.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1", len(evaluation.Results))
	}

	got := evaluation.Results[0].MatchCounts["match-image"]
	if got != 2 {
		t.Fatalf("MatchCounts[match-image] = %d, want 2", got)
	}
}

func TestEvaluatePreservesLowConfidenceRejectionReason(t *testing.T) {
	t.Parallel()

	pack := rules.RulePack{
		Name:                   "container_core",
		MinAdmissionConfidence: 0.75,
		Rules: []rules.Rule{
			{Name: "admit-workload", Kind: rules.RuleKindAdmit, Priority: 1},
		},
	}

	evaluation, err := Evaluate(pack, []model.Candidate{newCandidate("candidate-1", "sample-service", 0.42, []model.EvidenceAtom{newEvidence("ev-1")})})
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}

	got := evaluation.Results[0].Candidate
	if got.State != model.CandidateStateRejected {
		t.Fatalf("State = %q, want %q", got.State, model.CandidateStateRejected)
	}
	if len(got.RejectionReasons) != 1 || got.RejectionReasons[0] != model.RejectionReasonLowConfidence {
		t.Fatalf("RejectionReasons = %#v, want [%q]", got.RejectionReasons, model.RejectionReasonLowConfidence)
	}
	if len(got.Evidence) != 1 || got.Evidence[0].ID != "ev-1" {
		t.Fatalf("Evidence = %#v, want preserved evidence metadata", got.Evidence)
	}
}

func TestEvaluateUsesDeterministicTieBreakForSameCorrelationKey(t *testing.T) {
	t.Parallel()

	pack := rules.RulePack{
		Name:                   "container_core",
		MinAdmissionConfidence: 0.75,
		Rules: []rules.Rule{
			{Name: "admit-workload", Kind: rules.RuleKindAdmit, Priority: 1},
		},
	}

	winnerA := newCandidate("candidate-a", "sample-service", 0.91, []model.EvidenceAtom{newEvidence("ev-a")})
	winnerB := newCandidate("candidate-b", "sample-service", 0.91, []model.EvidenceAtom{newEvidence("ev-b")})

	evaluation, err := Evaluate(pack, []model.Candidate{winnerB, winnerA})
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}

	if evaluation.Results[0].Candidate.ID != "candidate-a" {
		t.Fatalf("winner ID = %q, want %q", evaluation.Results[0].Candidate.ID, "candidate-a")
	}
	if evaluation.Results[0].Candidate.State != model.CandidateStateAdmitted {
		t.Fatalf("winner state = %q, want %q", evaluation.Results[0].Candidate.State, model.CandidateStateAdmitted)
	}
	if evaluation.Results[1].Candidate.State != model.CandidateStateRejected {
		t.Fatalf("loser state = %q, want %q", evaluation.Results[1].Candidate.State, model.CandidateStateRejected)
	}
	if evaluation.Results[1].Candidate.RejectionReasons[0] != model.RejectionReasonLostTieBreak {
		t.Fatalf("loser reason = %#v, want %q", evaluation.Results[1].Candidate.RejectionReasons, model.RejectionReasonLostTieBreak)
	}
}

func TestEvaluatePreservesStructuralMismatchRejectionReason(t *testing.T) {
	t.Parallel()

	pack := rules.RulePack{
		Name:                   "dockerfile",
		MinAdmissionConfidence: 0.75,
		RequiredEvidence: []rules.EvidenceRequirement{
			{
				Name:     "runtime-image",
				MinCount: 1,
				MatchAll: []rules.EvidenceSelector{
					{Field: rules.EvidenceFieldEvidenceType, Value: "dockerfile"},
					{Field: rules.EvidenceFieldKey, Value: "image"},
				},
			},
		},
		Rules: []rules.Rule{
			{Name: "admit-runtime-image-candidate", Kind: rules.RuleKindAdmit, Priority: 1},
		},
	}

	candidate := newCandidate("candidate-1", "sample-service", 0.9, []model.EvidenceAtom{
		{
			ID:           "ev-1",
			SourceSystem: "git",
			EvidenceType: "dockerfile",
			ScopeID:      "repo:sample-service",
			Key:          "repository",
			Value:        "sample-service",
			Confidence:   0.9,
		},
	})

	evaluation, err := Evaluate(pack, []model.Candidate{candidate})
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}

	got := evaluation.Results[0].Candidate
	if got.State != model.CandidateStateRejected {
		t.Fatalf("State = %q, want %q", got.State, model.CandidateStateRejected)
	}
	if len(got.RejectionReasons) != 1 || got.RejectionReasons[0] != model.RejectionReasonStructuralMismatch {
		t.Fatalf("RejectionReasons = %#v, want [%q]", got.RejectionReasons, model.RejectionReasonStructuralMismatch)
	}
}

func TestEvaluateAccumulatesAllFailedAdmissionReasonsInStableOrder(t *testing.T) {
	t.Parallel()

	pack := rules.RulePack{
		Name:                   "dockerfile",
		MinAdmissionConfidence: 0.95,
		RequiredEvidence: []rules.EvidenceRequirement{
			{
				Name:     "runtime-image",
				MinCount: 1,
				MatchAll: []rules.EvidenceSelector{
					{Field: rules.EvidenceFieldEvidenceType, Value: "dockerfile"},
					{Field: rules.EvidenceFieldKey, Value: "image"},
				},
			},
		},
		Rules: []rules.Rule{
			{Name: "admit-runtime-image-candidate", Kind: rules.RuleKindAdmit, Priority: 1},
		},
	}

	candidate := newCandidate("candidate-1", "sample-service", 0.42, []model.EvidenceAtom{
		{
			ID:           "ev-1",
			SourceSystem: "git",
			EvidenceType: "dockerfile",
			ScopeID:      "repo:sample-service",
			Key:          "repository",
			Value:        "sample-service",
			Confidence:   0.42,
		},
	})

	evaluation, err := Evaluate(pack, []model.Candidate{candidate})
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}

	got := evaluation.Results[0].Candidate.RejectionReasons
	want := []model.RejectionReason{
		model.RejectionReasonLowConfidence,
		model.RejectionReasonStructuralMismatch,
	}
	if len(got) != len(want) {
		t.Fatalf("len(RejectionReasons) = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RejectionReasons[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func newCandidate(id string, key string, confidence float64, evidence []model.EvidenceAtom) model.Candidate {
	return model.Candidate{
		ID:             id,
		Kind:           "deployable_unit",
		CorrelationKey: key,
		State:          model.CandidateStateProvisional,
		Confidence:     confidence,
		Evidence:       evidence,
	}
}

func newEvidence(id string) model.EvidenceAtom {
	return model.EvidenceAtom{
		ID:           id,
		SourceSystem: "git",
		EvidenceType: "dockerfile",
		ScopeID:      "repo:sample-service",
		Key:          "image",
		Value:        "sample-service",
		Confidence:   0.8,
	}
}
