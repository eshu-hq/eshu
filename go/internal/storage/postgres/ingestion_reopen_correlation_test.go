// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestReopenSucceededReducerWorkItemsReplaysListedItems proves the generic
// correlation reopen lists succeeded work items per domain and calls
// ReopenSucceeded (one guarded UPDATE) for each, skipping blank IDs. This is the
// rc-1 keystone: deployable_unit_correlation must be replayed after the resolved
// DEPLOYS_FROM relationships it consumes exist.
func TestReopenSucceededReducerWorkItemsReplaysListedItems(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		// One list query returns two real ids plus a blank that must be skipped.
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"work-item-1"}, {""}, {"work-item-2"}}},
		},
		execResults: []sql.Result{
			rowsAffectedResult{rowsAffected: 1},
			rowsAffectedResult{rowsAffected: 1},
		},
	}
	store := IngestionStore{db: db, Now: func() time.Time { return now }}

	err := store.ReopenSucceededReducerWorkItems(
		context.Background(), nil, nil, []string{"deployable_unit_correlation"},
	)
	if err != nil {
		t.Fatalf("ReopenSucceededReducerWorkItems() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("list query count = %d, want %d", got, want)
	}
	if got := db.queries[0]; !strings.Contains(got.query, "domain = $1") ||
		!strings.Contains(got.query, "status = 'succeeded'") {
		t.Fatalf("list query = %q, want domain-parameterized succeeded selection", got.query)
	}
	if got, want := got0Arg(db.queries[0].args), "deployable_unit_correlation"; got != want {
		t.Fatalf("list query domain arg = %v, want %v", got, want)
	}
	// Two non-blank ids => two reopen UPDATEs; the blank id is skipped.
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("reopen exec count = %d, want %d (blank id must be skipped)", got, want)
	}
}

// TestReopenSucceededReducerWorkItemsSkipsBlankDomains proves whitespace-only or
// empty domains never reach the database.
func TestReopenSucceededReducerWorkItemsSkipsBlankDomains(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := IngestionStore{db: db, Now: time.Now}

	err := store.ReopenSucceededReducerWorkItems(
		context.Background(), nil, nil, []string{"", "   "},
	)
	if err != nil {
		t.Fatalf("ReopenSucceededReducerWorkItems() error = %v, want nil", err)
	}
	if got := len(db.queries); got != 0 {
		t.Fatalf("blank domains issued %d queries, want 0", got)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("blank domains issued %d execs, want 0", got)
	}
}

// TestReopenSucceededReducerWorkItemsRequiresDB proves a nil db is a hard error
// rather than a silent no-op.
func TestReopenSucceededReducerWorkItemsRequiresDB(t *testing.T) {
	t.Parallel()

	store := IngestionStore{}
	err := store.ReopenSucceededReducerWorkItems(
		context.Background(), nil, nil, []string{"deployable_unit_correlation"},
	)
	if err == nil {
		t.Fatal("ReopenSucceededReducerWorkItems() error = nil, want db-required error")
	}
	if !strings.Contains(err.Error(), "db is required") {
		t.Fatalf("error = %v, want db-required context", err)
	}
}

// TestReopenSucceededReducerWorkItemsPropagatesReopenError proves a failed
// reopen surfaces with the domain in the wrapped error rather than being
// swallowed.
func TestReopenSucceededReducerWorkItemsPropagatesReopenError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"work-item-1"}}},
		},
		execErrors: []error{errors.New("boom")},
	}
	store := IngestionStore{db: db, Now: func() time.Time { return now }}

	err := store.ReopenSucceededReducerWorkItems(
		context.Background(), nil, nil, []string{"deployable_unit_correlation"},
	)
	if err == nil {
		t.Fatal("ReopenSucceededReducerWorkItems() error = nil, want reopen failure")
	}
	if !strings.Contains(err.Error(), "reopen deployable_unit_correlation work items") {
		t.Fatalf("error = %v, want domain-tagged reopen context", err)
	}
}

func got0Arg(args []any) any {
	if len(args) == 0 {
		return nil
	}
	return args[0]
}
