// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// TestFunctionGraphIDSchemaIndexesUID proves program-input loaders can join
// CALLS graph endpoints back to durable FunctionIDs without scanning the whole
// mapping table.
func TestFunctionGraphIDSchemaIndexesUID(t *testing.T) {
	t.Parallel()

	if !strings.Contains(FunctionGraphIDSchemaSQL(), "function_graph_ids_uid_idx") {
		t.Fatalf("FunctionGraphIDSchemaSQL() missing uid index:\n%s", FunctionGraphIDSchemaSQL())
	}
}

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

// TestFunctionGraphIDStoreReplaceGraphIDsDeletesRepoSnapshot proves replacement
// clears stale repo mappings before inserting currently resolved graph uids.
func TestFunctionGraphIDStoreReplaceGraphIDsDeletesRepoSnapshot(t *testing.T) {
	t.Parallel()
	db := &recordingExecQueryer{}
	store := NewFunctionGraphIDStore(db)
	at := time.Date(2026, time.June, 18, 1, 0, 0, 0, time.UTC)

	err := store.ReplaceGraphIDs(context.Background(), "repo-1", map[summary.FunctionID]string{
		"repo-1\x1fpkg\x1f\x1fview":  "uid-view",
		"repo-1\x1fpkg\x1f\x1fempty": "",
	}, at)
	if err != nil {
		t.Fatalf("ReplaceGraphIDs error: %v", err)
	}

	if len(db.execs) != 2 {
		t.Fatalf("exec calls = %d, want delete plus upsert", len(db.execs))
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM function_graph_ids") {
		t.Fatalf("first query = %q, want repo delete", db.execs[0].query)
	}
	if got := db.execs[0].args[0]; got != "repo-1" {
		t.Fatalf("delete repo arg = %v, want repo-1", got)
	}
	if args := db.execs[1].args; len(args) != 4 || args[0] != "repo-1\x1fpkg\x1f\x1fview" || args[1] != "uid-view" {
		t.Fatalf("upsert args wrong: %+v", args)
	}
}

// TestFunctionGraphIDStoreReplaceGraphIDsAllowsEmptySnapshot proves a generation
// with no resolved graph uids still retracts stale rows for the repo.
func TestFunctionGraphIDStoreReplaceGraphIDsAllowsEmptySnapshot(t *testing.T) {
	t.Parallel()
	db := &recordingExecQueryer{}
	store := NewFunctionGraphIDStore(db)

	err := store.ReplaceGraphIDs(context.Background(), "repo-1", nil, time.Now())
	if err != nil {
		t.Fatalf("ReplaceGraphIDs error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want only repo delete", len(db.execs))
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM function_graph_ids") {
		t.Fatalf("query = %q, want repo delete", db.execs[0].query)
	}
}
