package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSharedIntentStoreHasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{completed: map[string]bool{"partition-a": true}}
	store := NewSharedIntentStore(db)
	ctx := context.Background()

	got, err := store.HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(ctx, reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-new",
	}, "partition-a", reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents: %v", err)
	}
	if !got {
		t.Fatal("HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents = false, want true")
	}

	got, err = store.HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(ctx, reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-new",
	}, "partition-b", reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents other partition: %v", err)
	}
	if got {
		t.Fatal("HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents for pending partition = true, want false")
	}
	if got, want := db.queryArgs, []any{
		"scope-a",
		"repo-a",
		"run-new",
		"partition-b",
		reducer.DomainCodeCalls,
	}; !equalPartitionHistoryArgs(got, want) {
		t.Fatalf("query args = %#v, want %#v", got, want)
	}
	if !strings.Contains(db.query, "partition_key = $4") {
		t.Fatalf("query = %q, want partition scoped lookup", db.query)
	}
}

func TestSharedIntentStoreHasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{refreshCompleted: true}
	store := NewSharedIntentStore(db)
	ctx := context.Background()

	got, err := store.HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(ctx, reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-new",
	}, []string{"/repo/src/models.go"}, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents: %v", err)
	}
	if !got {
		t.Fatal("HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents = false, want true")
	}
	if got, want := len(db.queryArgs), 5; got != want {
		t.Fatalf("query arg count = %d, want %d", got, want)
	}
	if !strings.Contains(db.query, "payload->>'intent_type' = 'repo_refresh'") {
		t.Fatalf("query = %q, want repo_refresh filter", db.query)
	}
	if !strings.Contains(db.query, "jsonb_array_elements_text(payload->'delta_file_paths')") {
		t.Fatalf("query = %q, want delta_file_paths coverage check", db.query)
	}

	got, err = store.HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(ctx, reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-new",
	}, nil, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents empty files: %v", err)
	}
	if got {
		t.Fatal("HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents empty files = true, want false")
	}
}

type partitionHistoryTestDB struct {
	completed        map[string]bool
	refreshCompleted bool
	query            string
	queryArgs        []any
}

func (db *partitionHistoryTestDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected exec")
}

func (db *partitionHistoryTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.query = query
	db.queryArgs = append([]any(nil), args...)
	if strings.Contains(query, "payload->>'intent_type' = 'repo_refresh'") {
		return &partitionHistoryRows{exists: db.refreshCompleted, idx: -1}, nil
	}
	partitionKey := args[3].(string)
	return &partitionHistoryRows{exists: db.completed[partitionKey], idx: -1}, nil
}

type partitionHistoryRows struct {
	exists bool
	idx    int
}

func (r *partitionHistoryRows) Next() bool {
	r.idx++
	return r.idx == 0
}

func (r *partitionHistoryRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("scan destinations = %d, want 1", len(dest))
	}
	exists, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("scan destination type = %T, want *bool", dest[0])
	}
	*exists = r.exists
	return nil
}

func (r *partitionHistoryRows) Err() error {
	return nil
}

func (r *partitionHistoryRows) Close() error {
	return nil
}

func equalPartitionHistoryArgs(got []any, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
