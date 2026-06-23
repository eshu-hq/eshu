package recovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewHandlerRequiresStore(t *testing.T) {
	t.Parallel()

	_, err := NewHandler(nil)
	if err == nil {
		t.Fatal("NewHandler(nil) error = nil, want non-nil")
	}
}

func TestReplayFilterValidateRequiresStage(t *testing.T) {
	t.Parallel()

	if err := (ReplayFilter{}).Validate(); err == nil {
		t.Fatal("ReplayFilter{}.Validate() = nil, want error for missing stage")
	}
}

func TestReplayFilterValidateAcceptsProjector(t *testing.T) {
	t.Parallel()

	if err := (ReplayFilter{Stage: StageProjector}).Validate(); err != nil {
		t.Fatalf("ReplayFilter{Stage: StageProjector}.Validate() = %v, want nil", err)
	}
}

func TestReplayFilterValidateAcceptsReducer(t *testing.T) {
	t.Parallel()

	if err := (ReplayFilter{Stage: StageReducer}).Validate(); err != nil {
		t.Fatalf("ReplayFilter{Stage: StageReducer}.Validate() = %v, want nil", err)
	}
}

func TestRefinalizeFilterValidateRequiresScopeIDs(t *testing.T) {
	t.Parallel()

	if err := (RefinalizeFilter{}).Validate(); err == nil {
		t.Fatal("RefinalizeFilter{}.Validate() = nil, want error for empty scope_ids")
	}
}

func TestCollectorGenerationReplayFilterValidateRequiresNonBlankCollectorKind(t *testing.T) {
	t.Parallel()

	for _, collectorKind := range []string{"", "   "} {
		err := (CollectorGenerationReplayFilter{CollectorKind: collectorKind}).Validate()
		if err == nil {
			t.Fatalf("CollectorGenerationReplayFilter{CollectorKind: %q}.Validate() = nil, want error", collectorKind)
		}
	}
}

func TestHandlerReplayFailedDelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{
		replayResult: ReplayResult{
			Stage:       StageProjector,
			Replayed:    2,
			WorkItemIDs: []string{"item-1", "item-2"},
		},
	}
	handler := mustNewHandler(t, store)

	result, err := handler.ReplayFailed(context.Background(), ReplayFilter{
		Stage:    StageProjector,
		ScopeIDs: []string{"scope-1"},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ReplayFailed() error = %v, want nil", err)
	}
	if result.Replayed != 2 {
		t.Fatalf("ReplayFailed() Replayed = %d, want 2", result.Replayed)
	}
	if len(result.WorkItemIDs) != 2 {
		t.Fatalf("ReplayFailed() WorkItemIDs = %d, want 2", len(result.WorkItemIDs))
	}
	if store.replayFilter.Stage != StageProjector {
		t.Fatalf("store received stage = %q, want %q", store.replayFilter.Stage, StageProjector)
	}
	if len(store.replayFilter.ScopeIDs) != 1 || store.replayFilter.ScopeIDs[0] != "scope-1" {
		t.Fatalf("store received scope_ids = %v, want [scope-1]", store.replayFilter.ScopeIDs)
	}
}

func TestHandlerReplayFailedRejectsInvalidFilter(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)

	_, err := handler.ReplayFailed(context.Background(), ReplayFilter{})
	if err == nil {
		t.Fatal("ReplayFailed() with empty filter error = nil, want non-nil")
	}
}

func TestHandlerReplayFailedPropagatesStoreError(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("database unavailable")
	store := &fakeReplayStore{replayErr: storeErr}
	handler := mustNewHandler(t, store)

	_, err := handler.ReplayFailed(context.Background(), ReplayFilter{Stage: StageReducer})
	if !errors.Is(err, storeErr) {
		t.Fatalf("ReplayFailed() error = %v, want %v", err, storeErr)
	}
}

func TestHandlerRefinalizeDelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{
		refinalizeResult: RefinalizeResult{
			Enqueued: 3,
			ScopeIDs: []string{"s1", "s2", "s3"},
		},
	}
	handler := mustNewHandler(t, store)

	result, err := handler.Refinalize(context.Background(), RefinalizeFilter{
		ScopeIDs: []string{"s1", "s2", "s3"},
	})
	if err != nil {
		t.Fatalf("Refinalize() error = %v, want nil", err)
	}
	if result.Enqueued != 3 {
		t.Fatalf("Refinalize() Enqueued = %d, want 3", result.Enqueued)
	}
	if len(result.ScopeIDs) != 3 {
		t.Fatalf("Refinalize() ScopeIDs = %d, want 3", len(result.ScopeIDs))
	}
}

func TestHandlerRefinalizeRejectsEmptyScopeIDs(t *testing.T) {
	t.Parallel()

	store := &fakeReplayStore{}
	handler := mustNewHandler(t, store)

	_, err := handler.Refinalize(context.Background(), RefinalizeFilter{})
	if err == nil {
		t.Fatal("Refinalize() with empty scope_ids error = nil, want non-nil")
	}
}

func TestHandlerRefinalizePropagatesStoreError(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("database unavailable")
	store := &fakeReplayStore{refinalizeErr: storeErr}
	handler := mustNewHandler(t, store)

	_, err := handler.Refinalize(context.Background(), RefinalizeFilter{
		ScopeIDs: []string{"s1"},
	})
	if !errors.Is(err, storeErr) {
		t.Fatalf("Refinalize() error = %v, want %v", err, storeErr)
	}
}

func TestHandlerReplayCollectorGenerationsDelegatesToStore(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 18, 30, 0, 0, time.UTC)
	store := &fakeReplayStore{
		collectorGenerationReplayResult: CollectorGenerationReplayResult{
			Replayed:      1,
			GenerationIDs: []string{"generation-456"},
		},
	}
	handler := mustNewHandler(t, store)
	handler.now = func() time.Time { return now.In(time.FixedZone("offset", -4*60*60)) }

	result, err := handler.ReplayCollectorGenerations(context.Background(), CollectorGenerationReplayFilter{
		ScopeIDs:      []string{"scope-123"},
		FailureClass:  "commit_failure",
		CollectorKind: "git",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ReplayCollectorGenerations() error = %v, want nil", err)
	}
	if result.Replayed != 1 || len(result.GenerationIDs) != 1 || result.GenerationIDs[0] != "generation-456" {
		t.Fatalf("ReplayCollectorGenerations() result = %#v, want generation replay", result)
	}
	if got := store.collectorGenerationReplayFilter.CollectorKind; got != "git" {
		t.Fatalf("store collector kind = %q, want git", got)
	}
	if !store.collectorGenerationReplayAt.Equal(now) {
		t.Fatalf("store replay time = %s, want %s", store.collectorGenerationReplayAt, now)
	}
}

func TestCollectorGenerationReplayFilterRejectsBlankCollectorKind(t *testing.T) {
	t.Parallel()

	if err := (CollectorGenerationReplayFilter{ScopeIDs: []string{"scope-123"}}).Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

// --- helpers ---

func mustNewHandler(t *testing.T, store ReplayStore) *Handler {
	t.Helper()

	handler, err := NewHandler(store)
	if err != nil {
		t.Fatalf("NewHandler() error = %v, want nil", err)
	}

	return handler
}

// --- fakes ---

type fakeReplayStore struct {
	replayFilter                    ReplayFilter
	replayResult                    ReplayResult
	replayErr                       error
	replayCalled                    bool
	refinalizeFilter                RefinalizeFilter
	refinalizeResult                RefinalizeResult
	refinalizeErr                   error
	collectorGenerationReplayFilter CollectorGenerationReplayFilter
	collectorGenerationReplayAt     time.Time
	collectorGenerationReplayResult CollectorGenerationReplayResult
	collectorGenerationReplayErr    error
	drainDepth                      int
	drainDepthErr                   error
	drainDepthCalled                bool
	drainDepthFilter                ReplayFilter
}

func (f *fakeReplayStore) ReplayFailedWorkItems(
	_ context.Context,
	filter ReplayFilter,
	_ time.Time,
) (ReplayResult, error) {
	f.replayFilter = filter
	f.replayCalled = true
	return f.replayResult, f.replayErr
}

func (f *fakeReplayStore) CountDeadLetterBacklog(
	_ context.Context,
	filter ReplayFilter,
) (int, error) {
	f.drainDepthCalled = true
	f.drainDepthFilter = filter
	return f.drainDepth, f.drainDepthErr
}

func (f *fakeReplayStore) RefinalizeScopeProjections(
	_ context.Context,
	filter RefinalizeFilter,
	_ time.Time,
) (RefinalizeResult, error) {
	f.refinalizeFilter = filter
	return f.refinalizeResult, f.refinalizeErr
}

func (f *fakeReplayStore) ReplayCollectorGenerations(
	_ context.Context,
	filter CollectorGenerationReplayFilter,
	now time.Time,
) (CollectorGenerationReplayResult, error) {
	f.collectorGenerationReplayFilter = filter
	f.collectorGenerationReplayAt = now
	return f.collectorGenerationReplayResult, f.collectorGenerationReplayErr
}
