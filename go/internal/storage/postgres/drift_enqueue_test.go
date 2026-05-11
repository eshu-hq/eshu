package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestEnqueueConfigStateDriftIntentsEnqueuesOnePerActiveScope(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// listActiveStateSnapshotScopes returns two state_snapshot scopes
			// with active generations.
			{rows: [][]any{
				{"state_snapshot:s3:hash-1", "gen-state-1"},
				{"state_snapshot:s3:hash-2", "gen-state-2"},
			}},
		},
	}
	store := NewIngestionStore(db)

	if err := store.EnqueueConfigStateDriftIntents(context.Background(), nil, nil); err != nil {
		t.Fatalf("EnqueueConfigStateDriftIntents() error = %v, want nil", err)
	}

	// One QUERY for the scope scan + one EXEC (batch INSERT for both intents).
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "FROM ingestion_scopes") {
		t.Fatalf("query missing FROM ingestion_scopes: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "state_snapshot:%") {
		t.Fatalf("query missing state_snapshot prefix: %s", db.queries[0].query)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (single batch INSERT)", got, want)
	}
	insert := db.execs[0].query
	if !strings.Contains(insert, "INSERT INTO fact_work_items") {
		t.Fatalf("exec query missing fact_work_items insert: %s", insert)
	}
	// The reducer queue carries the domain string as one of the bound args
	// per row; assert it shows up in the argument slice.
	foundDomain := false
	for _, arg := range db.execs[0].args {
		if s, ok := arg.(string); ok && s == "config_state_drift" {
			foundDomain = true
			break
		}
	}
	if !foundDomain {
		t.Fatalf("config_state_drift domain not present in INSERT args: %#v", db.execs[0].args)
	}
}

func TestEnqueueConfigStateDriftIntentsNoOpWhenNoScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	store := NewIngestionStore(db)

	if err := store.EnqueueConfigStateDriftIntents(context.Background(), nil, nil); err != nil {
		t.Fatalf("EnqueueConfigStateDriftIntents() error = %v, want nil", err)
	}

	// Scope scan ran (1 query); no exec because there were no intents.
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 0; got != want {
		t.Fatalf("exec count = %d, want %d (no intents to enqueue)", got, want)
	}
}

func TestEnqueueConfigStateDriftIntentsRequiresDatabase(t *testing.T) {
	t.Parallel()

	var store IngestionStore
	if err := store.EnqueueConfigStateDriftIntents(context.Background(), nil, nil); err == nil {
		t.Fatal("nil DB: error = nil, want non-nil")
	}
}
