package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// TestRecoveryStoreReplayFailedWorkItemsLimitBoundsUpdateNotJustScan is the
// regression for issue #3652 P3: DrainFilter.Limit must bound the UPDATE itself
// via a subquery, not just the Go scan loop. When Limit=2 and the DB returns 2
// rows, the query sent to the DB must contain the bounded subquery with LIMIT
// so only Limit rows are reset to pending — not all matching rows reset and
// only 2 returned.
func TestRecoveryStoreReplayFailedWorkItemsLimitBoundsUpdateNotJustScan(t *testing.T) {
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
		Limit: 2,
	}, now)
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if got, want := result.Replayed, 2; got != want {
		t.Fatalf("result.Replayed = %d, want %d", got, want)
	}
	// The UPDATE query must carry the LIMIT bound in SQL so only Limit rows are
	// reset to pending, not all matching rows.
	if !strings.Contains(db.queries[0].query, "LIMIT $2") {
		t.Fatalf("drain UPDATE query missing SQL LIMIT bound:\n%s\nQuery must use bounded subquery so Limit caps the mutation, not just the Go scan", db.queries[0].query)
	}
	// The limit must be the second positional arg ($2).
	if len(db.queries[0].args) < 2 {
		t.Fatalf("query args len = %d, want >= 2", len(db.queries[0].args))
	}
	if got, want := db.queries[0].args[1], 2; got != want {
		t.Fatalf("query args[1] (limit) = %v, want %d", got, want)
	}
}

// TestRecoveryStoreReplayFailedWorkItemsUnlimitedUsesSimpleTemplate proves that
// a zero Limit does not inject a bounded subquery (which would be a regression
// for the normal replay path).
func TestRecoveryStoreReplayFailedWorkItemsUnlimitedUsesSimpleTemplate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"item-1"}, {"item-2"}, {"item-3"}}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	result, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage: recovery.StageProjector,
		Limit: 0,
	}, now)
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if got, want := result.Replayed, 3; got != want {
		t.Fatalf("result.Replayed = %d, want %d", got, want)
	}
	if strings.Contains(db.queries[0].query, "LIMIT $2") {
		t.Fatal("unlimited replay query must not contain bounded subquery")
	}
}

// TestRecoveryStoreReplayFailedWorkItemsBoundedDrainPlacesPredicateAfterLimit is
// the regression for the full drain path under #3652 P3: when a bounded replay
// also carries scope and manual-review exclusion predicates, the placeholders
// must start at $3 ($1 timestamp, $2 limit) and the exclusion array must remain
// the last positional arg so a manual-review row can never be drained even with
// a broad selector. It also asserts FOR UPDATE SKIP LOCKED is present so two
// concurrent drains never fight over the same rows (the bound stays a true cap,
// not a serialization point).
func TestRecoveryStoreReplayFailedWorkItemsBoundedDrainPlacesPredicateAfterLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"item-1"}}},
		},
	}

	store := NewRecoveryStore(db)
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	_, err := store.ReplayFailedWorkItems(context.Background(), recovery.ReplayFilter{
		Stage:                 recovery.StageProjector,
		ScopeIDs:              []string{"scope-1"},
		FailureClass:          "retry_exhausted",
		ExcludeFailureClasses: []string{"projection_bug", "resource_exhausted"},
		Limit:                 100,
	}, now)
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}

	query := db.queries[0].query
	if !strings.Contains(query, "LIMIT $2") {
		t.Fatalf("bounded drain query missing LIMIT $2:\n%s", query)
	}
	if !strings.Contains(query, "FOR UPDATE SKIP LOCKED") {
		t.Fatalf("bounded drain query missing FOR UPDATE SKIP LOCKED (concurrent drains would fight):\n%s", query)
	}
	// Predicate placeholders must start at $3 (stage), not $2 (which is the limit).
	if !strings.Contains(query, "stage = $3") {
		t.Fatalf("bounded drain predicate must start at $3 after the limit arg:\n%s", query)
	}
	if strings.Contains(query, "stage = $2") {
		t.Fatalf("bounded drain predicate collides with the limit placeholder $2:\n%s", query)
	}
	// args: $1 now, $2 limit, then stage, scope, failure class, exclusion array.
	args := db.queries[0].args
	if got, want := args[1], 100; got != want {
		t.Fatalf("args[1] (limit) = %v, want %d", got, want)
	}
	last, ok := args[len(args)-1].([]string)
	if !ok {
		t.Fatalf("last arg type = %T, want []string exclusion", args[len(args)-1])
	}
	if len(last) != 2 || last[0] != "projection_bug" || last[1] != "resource_exhausted" {
		t.Fatalf("exclusion arg = %v, want [projection_bug resource_exhausted]", last)
	}
}
