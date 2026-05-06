package tags

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestNormalizationResultPhaseStatesBuildsCanonicalCloudStates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 21, 0, 0, 0, time.UTC)
	result := NormalizationResult{
		Resources: []NormalizedResource{
			{
				CanonicalResourceID: "cloud:ecs-service:sample-service-api",
				NormalizedTags: map[string]string{
					"service":     "sample-service-api",
					"environment": "prod",
				},
			},
		},
	}

	got, err := result.PhaseStates("scope-1", "generation-1", now)
	if err != nil {
		t.Fatalf("PhaseStates() error = %v, want nil", err)
	}
	if gotLen, want := len(got), 1; gotLen != want {
		t.Fatalf("len(PhaseStates()) = %d, want %d", gotLen, want)
	}
	if gotID, want := got[0].Key.AcceptanceUnitID, "cloud:ecs-service:sample-service-api"; gotID != want {
		t.Fatalf("AcceptanceUnitID = %q, want %q", gotID, want)
	}
	if gotPhase, want := got[0].Phase, reducer.GraphProjectionPhaseCanonicalNodesCommitted; gotPhase != want {
		t.Fatalf("Phase = %q, want %q", gotPhase, want)
	}
}

func TestNormalizationResultPhaseStatesDedupesCanonicalResources(t *testing.T) {
	t.Parallel()

	result := NormalizationResult{
		Resources: []NormalizedResource{
			{CanonicalResourceID: "cloud:alb:sample-service", NormalizedTags: map[string]string{"service": "sample-service"}},
			{CanonicalResourceID: "cloud:alb:sample-service", NormalizedTags: map[string]string{"environment": "qa"}},
		},
	}

	got, err := result.PhaseStates("scope-1", "generation-1", time.Date(2026, time.April, 20, 21, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("PhaseStates() error = %v, want nil", err)
	}
	if gotLen, want := len(got), 1; gotLen != want {
		t.Fatalf("len(PhaseStates()) = %d, want %d", gotLen, want)
	}
}

func TestPublishNormalizationResultPublishesCanonicalStates(t *testing.T) {
	t.Parallel()

	publisher := &recordingTagPhasePublisher{}
	result := NormalizationResult{
		Resources: []NormalizedResource{
			{CanonicalResourceID: "cloud:lambda:sample-service", NormalizedTags: map[string]string{"service": "sample-service"}},
		},
	}

	err := PublishNormalizationResult(
		context.Background(),
		publisher,
		"scope-1",
		"generation-1",
		result,
		time.Date(2026, time.April, 20, 21, 10, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("PublishNormalizationResult() error = %v, want nil", err)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("publisher calls = %d, want %d", got, want)
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, reducer.GraphProjectionKeyspaceCloudResourceUID; got != want {
		t.Fatalf("published keyspace = %q, want %q", got, want)
	}
}

func TestPublishNormalizationResultRejectsInvalidResource(t *testing.T) {
	t.Parallel()

	err := PublishNormalizationResult(
		context.Background(),
		&recordingTagPhasePublisher{},
		"scope-1",
		"generation-1",
		NormalizationResult{
			Resources: []NormalizedResource{{CanonicalResourceID: ""}},
		},
		time.Date(2026, time.April, 20, 21, 15, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("PublishNormalizationResult() error = nil, want non-nil")
	}
}

type recordingTagPhasePublisher struct {
	calls [][]reducer.GraphProjectionPhaseState
}

func (r *recordingTagPhasePublisher) PublishGraphProjectionPhases(_ context.Context, rows []reducer.GraphProjectionPhaseState) error {
	cloned := make([]reducer.GraphProjectionPhaseState, len(rows))
	copy(cloned, rows)
	r.calls = append(r.calls, cloned)
	return nil
}
