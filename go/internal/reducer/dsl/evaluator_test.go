package dsl

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEvaluationResultPhaseStatesBuildsStableStates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 20, 30, 0, 0, time.UTC)
	result := EvaluationResult{
		Publications: []Publication{
			{
				AcceptanceUnitID: "service:sample-service-api",
				Keyspace:         reducer.GraphProjectionKeyspaceServiceUID,
				Phase:            reducer.GraphProjectionPhaseDeploymentMapping,
				OutputKind:       OutputKindResolvedRelationship,
			},
			{
				AcceptanceUnitID: "terraform:stack/app",
				Keyspace:         reducer.GraphProjectionKeyspaceTerraformResourceUID,
				Phase:            reducer.GraphProjectionPhaseCrossSourceAnchorReady,
				OutputKind:       OutputKindResolvedRelationship,
			},
		},
	}

	got, err := result.PhaseStates("scope-1", "generation-1", now)
	if err != nil {
		t.Fatalf("PhaseStates() error = %v, want nil", err)
	}
	if gotLen, want := len(got), 2; gotLen != want {
		t.Fatalf("len(PhaseStates()) = %d, want %d", gotLen, want)
	}
	if gotKey, want := got[0].Key.AcceptanceUnitID, "service:sample-service-api"; gotKey != want {
		t.Fatalf("first AcceptanceUnitID = %q, want %q", gotKey, want)
	}
	if gotPhase, want := got[0].Phase, reducer.GraphProjectionPhaseDeploymentMapping; gotPhase != want {
		t.Fatalf("first Phase = %q, want %q", gotPhase, want)
	}
	if gotKey, want := got[1].Key.AcceptanceUnitID, "terraform:stack/app"; gotKey != want {
		t.Fatalf("second AcceptanceUnitID = %q, want %q", gotKey, want)
	}
	if gotPhase, want := got[1].Phase, reducer.GraphProjectionPhaseCrossSourceAnchorReady; gotPhase != want {
		t.Fatalf("second Phase = %q, want %q", gotPhase, want)
	}
}

func TestEvaluationResultPhaseStatesDedupesDuplicatePublications(t *testing.T) {
	t.Parallel()

	result := EvaluationResult{
		Publications: []Publication{
			{
				AcceptanceUnitID: "terraform:stack/app",
				Keyspace:         reducer.GraphProjectionKeyspaceTerraformResourceUID,
				Phase:            reducer.GraphProjectionPhaseCrossSourceAnchorReady,
				OutputKind:       OutputKindResolvedRelationship,
			},
			{
				AcceptanceUnitID: "terraform:stack/app",
				Keyspace:         reducer.GraphProjectionKeyspaceTerraformResourceUID,
				Phase:            reducer.GraphProjectionPhaseCrossSourceAnchorReady,
				OutputKind:       OutputKindDriftObservation,
			},
		},
	}

	got, err := result.PhaseStates("scope-1", "generation-1", time.Date(2026, time.April, 20, 20, 35, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("PhaseStates() error = %v, want nil", err)
	}
	if gotLen, want := len(got), 1; gotLen != want {
		t.Fatalf("len(PhaseStates()) = %d, want %d", gotLen, want)
	}
}

func TestPublishEvaluationResultPublishesStates(t *testing.T) {
	t.Parallel()

	publisher := &recordingPhasePublisher{}
	result := EvaluationResult{
		Publications: []Publication{
			{
				AcceptanceUnitID: "cloud:lambda:sample-service-api",
				Keyspace:         reducer.GraphProjectionKeyspaceCloudResourceUID,
				Phase:            reducer.GraphProjectionPhaseCrossSourceAnchorReady,
				OutputKind:       OutputKindDriftObservation,
			},
		},
	}

	err := PublishEvaluationResult(
		context.Background(),
		publisher,
		"scope-1",
		"generation-1",
		result,
		time.Date(2026, time.April, 20, 20, 40, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("PublishEvaluationResult() error = %v, want nil", err)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("publisher calls = %d, want %d", got, want)
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, reducer.GraphProjectionKeyspaceCloudResourceUID; got != want {
		t.Fatalf("published keyspace = %q, want %q", got, want)
	}
	if got, want := publisher.calls[0][0].Phase, reducer.GraphProjectionPhaseCrossSourceAnchorReady; got != want {
		t.Fatalf("published phase = %q, want %q", got, want)
	}
}

func TestPublishEvaluationResultRejectsInvalidPublication(t *testing.T) {
	t.Parallel()

	err := PublishEvaluationResult(
		context.Background(),
		&recordingPhasePublisher{},
		"scope-1",
		"generation-1",
		EvaluationResult{
			Publications: []Publication{{
				AcceptanceUnitID: "",
				Keyspace:         reducer.GraphProjectionKeyspaceCloudResourceUID,
				Phase:            reducer.GraphProjectionPhaseCrossSourceAnchorReady,
				OutputKind:       OutputKindResolvedRelationship,
			}},
		},
		time.Date(2026, time.April, 20, 20, 45, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("PublishEvaluationResult() error = nil, want non-nil")
	}
}

type recordingPhasePublisher struct {
	calls [][]reducer.GraphProjectionPhaseState
}

func (r *recordingPhasePublisher) PublishGraphProjectionPhases(_ context.Context, rows []reducer.GraphProjectionPhaseState) error {
	cloned := make([]reducer.GraphProjectionPhaseState, len(rows))
	copy(cloned, rows)
	r.calls = append(r.calls, cloned)
	return nil
}
