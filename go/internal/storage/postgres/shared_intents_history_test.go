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

type partitionHistoryTestDB struct {
	completed map[string]bool
	query     string
	queryArgs []any
}

func (db *partitionHistoryTestDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected exec")
}

func (db *partitionHistoryTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.query = query
	db.queryArgs = append([]any(nil), args...)
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
