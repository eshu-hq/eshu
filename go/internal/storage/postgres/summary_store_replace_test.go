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

func TestFunctionSummaryStoreReplaceSnapshotDeletesRepoRows(t *testing.T) {
	t.Parallel()
	db := &recordingExecQueryer{}
	store := NewFunctionSummaryStore(db)
	at := time.Date(2026, time.June, 18, 1, 0, 0, 0, time.UTC)

	err := store.ReplaceSnapshot(context.Background(), "repo-1", summary.Snapshot{Functions: []summary.SnapshotFunction{
		{
			ID:      "repo-1\x1fpkg\x1f\x1fhandle",
			Effects: summary.Effects{ParamToReturn: []int{0}},
			Version: "version-1",
		},
	}}, at)
	if err != nil {
		t.Fatalf("ReplaceSnapshot error: %v", err)
	}
	if db.beginCalls != 1 {
		t.Fatalf("begin calls = %d, want transaction", db.beginCalls)
	}
	if len(db.execs) != 2 {
		t.Fatalf("exec calls = %d, want delete plus upsert", len(db.execs))
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM function_summaries") {
		t.Fatalf("first query = %q, want repo delete", db.execs[0].query)
	}
	if !strings.Contains(db.execs[0].query, "updated_at <= $2") {
		t.Fatalf("delete query missing stale timestamp guard:\n%s", db.execs[0].query)
	}
	if got := db.execs[0].args[0]; got != "repo-1" {
		t.Fatalf("delete repo arg = %v, want repo-1", got)
	}
	if args := db.execs[1].args; args[0] != "repo-1\x1fpkg\x1f\x1fhandle" || args[4] != "repo-1" {
		t.Fatalf("upsert args wrong: %+v", args)
	}
}

func TestFunctionSummaryStoreReplaceSnapshotAllowsEmptySnapshot(t *testing.T) {
	t.Parallel()
	db := &recordingExecQueryer{}
	store := NewFunctionSummaryStore(db)

	err := store.ReplaceSnapshot(context.Background(), "repo-1", summary.Snapshot{}, time.Now())
	if err != nil {
		t.Fatalf("ReplaceSnapshot error: %v", err)
	}
	if db.beginCalls != 1 {
		t.Fatalf("begin calls = %d, want transaction", db.beginCalls)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want only repo delete", len(db.execs))
	}
	if !strings.Contains(db.execs[0].query, "DELETE FROM function_summaries") {
		t.Fatalf("query = %q, want repo delete", db.execs[0].query)
	}
}

func TestFunctionSummaryStoreReplaceSnapshotRejectsCrossRepoRows(t *testing.T) {
	t.Parallel()
	db := &recordingExecQueryer{}
	store := NewFunctionSummaryStore(db)

	err := store.ReplaceSnapshot(context.Background(), "repo-1", summary.Snapshot{Functions: []summary.SnapshotFunction{
		{
			ID:      "repo-2\x1fpkg\x1f\x1fhandle",
			Effects: summary.Effects{ParamToReturn: []int{0}},
			Version: "version-1",
		},
	}}, time.Now())
	if err == nil {
		t.Fatal("ReplaceSnapshot error = nil, want repo mismatch")
	}
	if !strings.Contains(err.Error(), "does not match replacement repo") {
		t.Fatalf("ReplaceSnapshot error = %v, want repo mismatch", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec calls = %d, want validation before write", len(db.execs))
	}
}
