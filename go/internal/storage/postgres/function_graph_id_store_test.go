package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// TestFunctionGraphIDStoreUpsertBuildsRows proves UpsertGraphIDs writes one row
// per resolved mapping (with derived repo) and skips empty uids.
func TestFunctionGraphIDStoreUpsertBuildsRows(t *testing.T) {
	t.Parallel()
	db := &recordingExecQueryer{}
	store := NewFunctionGraphIDStore(db)
	at := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	err := store.UpsertGraphIDs(context.Background(), map[summary.FunctionID]string{
		"repo-1\x1fpkg\x1f\x1fview":  "uid-view",
		"repo-1\x1fpkg\x1f\x1fempty": "",
	}, at)
	if err != nil {
		t.Fatalf("UpsertGraphIDs error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	args := db.execs[0].args
	// only the resolved mapping: function_id, uid, repo, updated_at
	if len(args) != 4 || args[0] != "repo-1\x1fpkg\x1f\x1fview" || args[1] != "uid-view" || args[2] != "repo-1" {
		t.Fatalf("row args wrong: %+v", args)
	}
}

// TestFunctionGraphIDStoreUpsertEmptyIsNoOp proves no write occurs when nothing
// resolves.
func TestFunctionGraphIDStoreUpsertEmptyIsNoOp(t *testing.T) {
	t.Parallel()
	db := &recordingExecQueryer{}
	store := NewFunctionGraphIDStore(db)
	if err := store.UpsertGraphIDs(context.Background(), map[summary.FunctionID]string{"x": ""}, time.Now()); err != nil {
		t.Fatalf("UpsertGraphIDs error: %v", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec calls = %d, want 0", len(db.execs))
	}
}
