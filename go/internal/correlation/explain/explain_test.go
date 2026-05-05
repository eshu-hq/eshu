package explain

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
)

func TestRenderProducesStableOrderedOutput(t *testing.T) {
	t.Parallel()

	result := engine.Result{
		Candidate: model.Candidate{
			ID:             "candidate-a",
			Kind:           "deployable_unit",
			CorrelationKey: "sample-service",
			State:          model.CandidateStateAdmitted,
			Confidence:     0.91,
			Evidence: []model.EvidenceAtom{
				{ID: "ev-2", SourceSystem: "jenkins", EvidenceType: "pipeline", ScopeID: "repo:deploy", Key: "image", Value: "sample-service", Confidence: 0.8},
				{ID: "ev-1", SourceSystem: "git", EvidenceType: "dockerfile", ScopeID: "repo:sample-service", Key: "image", Value: "sample-service", Confidence: 0.9},
			},
		},
		MatchCounts: map[string]int{"match-image": 2},
	}

	rendered := Render(result)
	lines := strings.Split(rendered, "\n")
	want := []string{
		"candidate=candidate-a kind=deployable_unit key=sample-service state=admitted confidence=0.91",
		"match_count rule=match-image count=2",
		"evidence id=ev-1 source=git type=dockerfile key=image value=sample-service confidence=0.90",
		"evidence id=ev-2 source=jenkins type=pipeline key=image value=sample-service confidence=0.80",
	}
	if len(lines) != len(want) {
		t.Fatalf("len(lines) = %d, want %d\nrendered:\n%s", len(lines), len(want), rendered)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestRenderIncludesStableRejectionReasonsForOperators(t *testing.T) {
	t.Parallel()

	result := engine.Result{
		Candidate: model.Candidate{
			ID:               "candidate-b",
			Kind:             "deployable_unit",
			CorrelationKey:   "sample-service",
			State:            model.CandidateStateRejected,
			Confidence:       0.42,
			RejectionReasons: []model.RejectionReason{model.RejectionReasonStructuralMismatch, model.RejectionReasonLowConfidence},
		},
	}

	rendered := Render(result)
	lines := strings.Split(rendered, "\n")
	want := []string{
		"candidate=candidate-b kind=deployable_unit key=sample-service state=rejected confidence=0.42",
		"rejection_reason=low_confidence",
		"rejection_reason=structural_mismatch",
	}
	if len(lines) != len(want) {
		t.Fatalf("len(lines) = %d, want %d\nrendered:\n%s", len(lines), len(want), rendered)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}
