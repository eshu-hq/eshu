package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
)

// TestFunctionSourceStoreUpsertBuildsRows proves UpsertSources writes one row per
// source with the function_id, param index, kind, and derived repo.
func TestFunctionSourceStoreUpsertBuildsRows(t *testing.T) {
	t.Parallel()
	db := &recordingExecQueryer{}
	store := NewFunctionSourceStore(db)
	at := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	err := store.UpsertSources(context.Background(), []interproc.Source{
		{Port: interproc.Port{Func: "repo-1\x1fpkg\x1f\x1fhandle", Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 2}}, Kind: "http_request"},
	}, at)
	if err != nil {
		t.Fatalf("UpsertSources error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	args := db.execs[0].args
	// function_id, param_index, kind, repo, updated_at
	if args[0] != "repo-1\x1fpkg\x1f\x1fhandle" || args[1] != 2 || args[2] != "http_request" || args[3] != "repo-1" {
		t.Fatalf("row args wrong: %+v", args[:4])
	}
}

// TestFunctionSourceStoreUpsertEmptyIsNoOp proves no write occurs for an empty
// source set.
func TestFunctionSourceStoreUpsertEmptyIsNoOp(t *testing.T) {
	t.Parallel()
	db := &recordingExecQueryer{}
	store := NewFunctionSourceStore(db)
	if err := store.UpsertSources(context.Background(), nil, time.Now()); err != nil {
		t.Fatalf("UpsertSources error: %v", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec calls = %d, want 0", len(db.execs))
	}
}
