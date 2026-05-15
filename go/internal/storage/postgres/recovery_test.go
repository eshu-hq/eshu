package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

func TestRecoveryStoreReplayFailedWorkItemsDefaultFilter(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"item-1"}, {"item-2"}}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage: recovery.StageProjector,
	}, now)
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if got, want := result.Replayed, 2; got != want {
		t.Fatalf("result.Replayed = %d, want %d", got, want)
	}
	if got, want := result.Stage, recovery.StageProjector; got != want {
		t.Fatalf("result.Stage = %q, want %q", got, want)
	}
	if got, want := len(result.WorkItemIDs), 2; got != want {
		t.Fatalf("result.WorkItemIDs len = %d, want %d", got, want)
	}
	if result.WorkItemIDs[0] != "item-1" || result.WorkItemIDs[1] != "item-2" {
		t.Fatalf("result.WorkItemIDs = %v, want [item-1, item-2]", result.WorkItemIDs)
	}

	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	if !strings.Contains(db.queries[0].query, "status IN ('dead_letter', 'failed')") {
		t.Fatalf("query missing terminal filter: %s", db.queries[0].query)
	}
	if strings.Contains(db.queries[0].query, "scope_id = ANY") {
		t.Fatal("default filter should not include scope_id clause")
	}
	if strings.Contains(db.queries[0].query, "AND failure_class = $") {
		t.Fatal("default filter should not include failure_class WHERE clause")
	}
}

func TestRecoveryStoreReplayFailedWorkItemsPreservesRetrySemantics(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		filter recovery.ReplayFilter
	}{
		{
			name: "default",
			filter: recovery.ReplayFilter{
				Stage: recovery.StageReducer,
			},
		},
		{
			name: "by scope",
			filter: recovery.ReplayFilter{
				Stage:    recovery.StageReducer,
				ScopeIDs: []string{"scope-1"},
			},
		},
		{
			name: "by class",
			filter: recovery.ReplayFilter{
				Stage:        recovery.StageReducer,
				FailureClass: "transient_db",
			},
		},
		{
			name: "by scope and class",
			filter: recovery.ReplayFilter{
				Stage:        recovery.StageReducer,
				ScopeIDs:     []string{"scope-1"},
				FailureClass: "transient_db",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			db := &fakeExecQueryer{
				queryResponses: []queueFakeRows{
					{rows: [][]any{{"item-1"}}},
				},
			}

			store := NewRecoveryStore(db)
			_, err := store.ReplayFailedWorkItems(
				context.Background(),
				testCase.filter,
				time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC),
			)
			if err != nil {
				t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
			}

			assertReplayQueryPreservesRetrySemantics(t, db.queries[0].query)
		})
	}
}

func TestRecoveryStoreReplayFailedWorkItemsByScopeFilter(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"item-3"}}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage:    recovery.StageReducer,
		ScopeIDs: []string{"scope-1", "scope-2"},
	}, now)
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if got, want := result.Replayed, 1; got != want {
		t.Fatalf("result.Replayed = %d, want %d", got, want)
	}
	if got, want := result.Stage, recovery.StageReducer; got != want {
		t.Fatalf("result.Stage = %q, want %q", got, want)
	}
	if !strings.Contains(db.queries[0].query, "scope_id = ANY") {
		t.Fatal("scope filter query missing scope_id clause")
	}
}

func assertReplayQueryPreservesRetrySemantics(t *testing.T, query string) {
	t.Helper()

	if strings.Contains(query, "attempt_count = 0") {
		t.Fatalf("recovery replay query resets retry evidence:\n%s", query)
	}
	if !strings.Contains(query, "attempt_count = GREATEST(attempt_count, 1)") {
		t.Fatalf("recovery replay query missing retry-preserving attempt_count:\n%s", query)
	}
}

func TestRecoveryStoreReplayFailedWorkItemsByClassFilter(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"item-4"}, {"item-5"}}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage:        recovery.StageProjector,
		FailureClass: "transient_db",
	}, now)
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if got, want := result.Replayed, 2; got != want {
		t.Fatalf("result.Replayed = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "failure_class =") {
		t.Fatal("class filter query missing failure_class clause")
	}
}

func TestRecoveryStoreReplayFailedWorkItemsByScopeAndClassFilter(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"item-6"}}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage:        recovery.StageProjector,
		ScopeIDs:     []string{"scope-1"},
		FailureClass: "transient_db",
	}, now)
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if got, want := result.Replayed, 1; got != want {
		t.Fatalf("result.Replayed = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "scope_id = ANY") {
		t.Fatal("combined filter query missing scope_id clause")
	}
	if !strings.Contains(db.queries[0].query, "failure_class =") {
		t.Fatal("combined filter query missing failure_class clause")
	}
}

func TestRecoveryStoreReplayFailedWorkItemsWithLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"item-1"}, {"item-2"}, {"item-3"}, {"item-4"}}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage: recovery.StageProjector,
		Limit: 2,
	}, now)
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if got, want := result.Replayed, 2; got != want {
		t.Fatalf("result.Replayed = %d, want %d (limit should cap)", got, want)
	}
}

func TestRecoveryStoreReplayFailedWorkItemsReturnsEmptyOnNoMatches(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage: recovery.StageProjector,
	}, now)
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if got, want := result.Replayed, 0; got != want {
		t.Fatalf("result.Replayed = %d, want %d", got, want)
	}
}

func TestRecoveryStoreReplayFailedWorkItemsPropagatesQueryError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{err: errors.New("connection refused")},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	_, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage: recovery.StageProjector,
	}, now)
	if err == nil {
		t.Fatal("ReplayFailedWorkItems() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "replay failed work items") {
		t.Fatalf("error = %q, want 'replay failed work items' context", err.Error())
	}
}

func TestRecoveryStoreReplayFailedWorkItemsRequiresDB(t *testing.T) {
	t.Parallel()

	store := NewRecoveryStore(nil)
	_, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage: recovery.StageProjector,
	}, time.Now())
	if err == nil {
		t.Fatal("ReplayFailedWorkItems() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("error = %q, want 'database is required'", err.Error())
	}
}

func TestRecoveryStoreRefinalizeScopeProjections(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"scope-1"}, {"scope-2"}}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result, err := store.RefinalizeScopeProjections(context.Background(), recovery.RefinalizeFilter{
		ScopeIDs: []string{"scope-1", "scope-2", "scope-3"},
	}, now)
	if err != nil {
		t.Fatalf("RefinalizeScopeProjections() error = %v, want nil", err)
	}
	if got, want := result.Enqueued, 2; got != want {
		t.Fatalf("result.Enqueued = %d, want %d", got, want)
	}
	if got, want := len(result.ScopeIDs), 2; got != want {
		t.Fatalf("result.ScopeIDs len = %d, want %d", got, want)
	}
	if result.ScopeIDs[0] != "scope-1" || result.ScopeIDs[1] != "scope-2" {
		t.Fatalf("result.ScopeIDs = %v, want [scope-1, scope-2]", result.ScopeIDs)
	}

	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	if !strings.Contains(db.queries[0].query, "INSERT INTO fact_work_items") {
		t.Fatalf("refinalize query missing INSERT: %s", db.queries[0].query)
	}
}

func TestRecoveryStoreRefinalizeScopeProjectionsPropagatesQueryError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{err: errors.New("connection refused")},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	_, err := store.RefinalizeScopeProjections(context.Background(), recovery.RefinalizeFilter{
		ScopeIDs: []string{"scope-1"},
	}, now)
	if err == nil {
		t.Fatal("RefinalizeScopeProjections() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "refinalize scope projections") {
		t.Fatalf("error = %q, want 'refinalize scope projections' context", err.Error())
	}
}

func TestRecoveryStoreRefinalizeScopeProjectionsRequiresDB(t *testing.T) {
	t.Parallel()

	store := NewRecoveryStore(nil)
	_, err := store.RefinalizeScopeProjections(context.Background(), recovery.RefinalizeFilter{
		ScopeIDs: []string{"scope-1"},
	}, time.Now())
	if err == nil {
		t.Fatal("RefinalizeScopeProjections() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("error = %q, want 'database is required'", err.Error())
	}
}
