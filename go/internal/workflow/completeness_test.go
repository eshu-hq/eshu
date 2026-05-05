package workflow

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestCompletenessStateValidateAcceptsValidCheckpoint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 18, 30, 0, 0, time.UTC)
	state := CompletenessState{
		RunID:         "run-1",
		CollectorKind: scope.CollectorGit,
		Keyspace:      reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		PhaseName:     "canonical_nodes_committed",
		Required:      true,
		Status:        "ready",
		ObservedAt:    now,
		UpdatedAt:     now,
	}

	if err := state.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestCompletenessStateValidateRejectsBlankStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 18, 30, 0, 0, time.UTC)
	state := CompletenessState{
		RunID:         "run-1",
		CollectorKind: scope.CollectorGit,
		Keyspace:      reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		PhaseName:     "canonical_nodes_committed",
		ObservedAt:    now,
		UpdatedAt:     now,
	}

	if err := state.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}
