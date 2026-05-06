package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGraphProjectionPhaseRepairerRunOnceRepublishesMissingReadiness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 16, 0, 0, 0, time.UTC)
	queue := &fakeGraphProjectionPhaseRepairQueue{
		due: []GraphProjectionPhaseRepair{
			newGraphProjectionPhaseRepairForTest(now),
		},
	}
	publisher := &recordingGraphProjectionPhasePublisher{}
	repairer := GraphProjectionPhaseRepairer{
		Queue: queue,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			if key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1" {
				return "gen-1", true
			}
			return "", false
		},
		StateLookup: graphProjectionPhaseLookupFixed(false, false, nil),
		Publisher:   publisher,
		Config:      GraphProjectionPhaseRepairerConfig{BatchLimit: 10, RetryDelay: time.Minute},
	}

	repaired, err := repairer.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if got, want := repaired, 1; got != want {
		t.Fatalf("repaired = %d, want %d", got, want)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("len(publisher.calls) = %d, want 1", len(publisher.calls))
	}
	if len(queue.deleted) != 1 {
		t.Fatalf("len(queue.deleted) = %d, want 1", len(queue.deleted))
	}
	if len(queue.failed) != 0 {
		t.Fatalf("len(queue.failed) = %d, want 0", len(queue.failed))
	}
}

func TestGraphProjectionPhaseRepairerRunOnceSkipsStaleGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 16, 0, 0, 0, time.UTC)
	queue := &fakeGraphProjectionPhaseRepairQueue{
		due: []GraphProjectionPhaseRepair{
			newGraphProjectionPhaseRepairForTest(now),
		},
	}
	publisher := &recordingGraphProjectionPhasePublisher{}
	repairer := GraphProjectionPhaseRepairer{
		Queue: queue,
		AcceptedGen: func(SharedProjectionAcceptanceKey) (string, bool) {
			return "gen-new", true
		},
		StateLookup: graphProjectionPhaseLookupFixed(false, false, nil),
		Publisher:   publisher,
		Config:      GraphProjectionPhaseRepairerConfig{BatchLimit: 10, RetryDelay: time.Minute},
	}

	repaired, err := repairer.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if got, want := repaired, 0; got != want {
		t.Fatalf("repaired = %d, want %d", got, want)
	}
	if len(publisher.calls) != 0 {
		t.Fatalf("len(publisher.calls) = %d, want 0", len(publisher.calls))
	}
	if len(queue.deleted) != 1 {
		t.Fatalf("len(queue.deleted) = %d, want 1", len(queue.deleted))
	}
}

func TestGraphProjectionPhaseRepairerRunOnceMarksFailedPublicationForRetry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 16, 0, 0, 0, time.UTC)
	queue := &fakeGraphProjectionPhaseRepairQueue{
		due: []GraphProjectionPhaseRepair{
			newGraphProjectionPhaseRepairForTest(now),
		},
	}
	publisher := &recordingGraphProjectionPhasePublisher{
		err: errors.New("publish failed"),
	}
	repairer := GraphProjectionPhaseRepairer{
		Queue: queue,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			if key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1" {
				return "gen-1", true
			}
			return "", false
		},
		StateLookup: graphProjectionPhaseLookupFixed(false, false, nil),
		Publisher:   publisher,
		Config:      GraphProjectionPhaseRepairerConfig{BatchLimit: 10, RetryDelay: 2 * time.Minute},
	}

	repaired, err := repairer.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil after repair backoff", err)
	}
	if got, want := repaired, 0; got != want {
		t.Fatalf("repaired = %d, want %d", got, want)
	}
	if len(queue.failed) != 1 {
		t.Fatalf("len(queue.failed) = %d, want 1", len(queue.failed))
	}
	if got, want := queue.failed[0].nextAttemptAt, now.Add(2*time.Minute); !got.Equal(want) {
		t.Fatalf("nextAttemptAt = %v, want %v", got, want)
	}
	if len(queue.deleted) != 0 {
		t.Fatalf("len(queue.deleted) = %d, want 0", len(queue.deleted))
	}
}

func newGraphProjectionPhaseRepairForTest(now time.Time) GraphProjectionPhaseRepair {
	return GraphProjectionPhaseRepair{
		Key: GraphProjectionPhaseKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
		},
		Phase:         GraphProjectionPhaseSemanticNodesCommitted,
		CommittedAt:   now.Add(-time.Minute),
		EnqueuedAt:    now.Add(-30 * time.Second),
		NextAttemptAt: now,
	}
}

type fakeGraphProjectionPhaseRepairQueue struct {
	due     []GraphProjectionPhaseRepair
	deleted []GraphProjectionPhaseRepair
	failed  []failedGraphProjectionRepair
}

type failedGraphProjectionRepair struct {
	repair        GraphProjectionPhaseRepair
	nextAttemptAt time.Time
	lastError     string
}

func (f *fakeGraphProjectionPhaseRepairQueue) Enqueue(context.Context, []GraphProjectionPhaseRepair) error {
	return nil
}

func (f *fakeGraphProjectionPhaseRepairQueue) ListDue(context.Context, time.Time, int) ([]GraphProjectionPhaseRepair, error) {
	cloned := make([]GraphProjectionPhaseRepair, len(f.due))
	copy(cloned, f.due)
	return cloned, nil
}

func (f *fakeGraphProjectionPhaseRepairQueue) Delete(_ context.Context, repairs []GraphProjectionPhaseRepair) error {
	f.deleted = append(f.deleted, repairs...)
	return nil
}

func (f *fakeGraphProjectionPhaseRepairQueue) MarkFailed(_ context.Context, repair GraphProjectionPhaseRepair, nextAttemptAt time.Time, lastError string) error {
	f.failed = append(f.failed, failedGraphProjectionRepair{
		repair:        repair,
		nextAttemptAt: nextAttemptAt,
		lastError:     lastError,
	})
	return nil
}

type recordingGraphProjectionPhasePublisher struct {
	calls [][]GraphProjectionPhaseState
	err   error
}

func (r *recordingGraphProjectionPhasePublisher) PublishGraphProjectionPhases(_ context.Context, rows []GraphProjectionPhaseState) error {
	cloned := make([]GraphProjectionPhaseState, len(rows))
	copy(cloned, rows)
	r.calls = append(r.calls, cloned)
	return r.err
}

type fakeGraphProjectionPhaseStateLookup struct {
	ready bool
	found bool
	err   error
}

func (f fakeGraphProjectionPhaseStateLookup) Lookup(_ context.Context, _ GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool, error) {
	return f.ready, f.found, f.err
}

func graphProjectionPhaseLookupFixed(ready bool, found bool, err error) fakeGraphProjectionPhaseStateLookup {
	return fakeGraphProjectionPhaseStateLookup{ready: ready, found: found, err: err}
}
