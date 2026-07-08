// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestCodeValueFlowBackfillStateStoreSchemaSQL proves the migration DDL includes
// the expected table.
func TestCodeValueFlowBackfillStateStoreSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := CodeValueFlowBackfillStateSchemaSQL()
	for _, want := range []string{
		"code_value_flow_backfill_state",
		"backfill_key TEXT PRIMARY KEY",
		"completed_at TIMESTAMPTZ NOT NULL",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("CodeValueFlowBackfillStateSchemaSQL() missing %q:\n%s", want, sql)
		}
	}
}

// TestCodeValueFlowBackfillStateStoreIsCompleteQueryShape proves the EXISTS
// query has the correct shape.
func TestCodeValueFlowBackfillStateStoreIsCompleteQueryShape(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeValueFlowBackfillStateStore(db)

	complete, err := store.IsComplete(context.Background(), "backfill-key")
	if err != nil {
		t.Fatalf("IsComplete error: %v", err)
	}
	if complete {
		t.Fatalf("IsComplete returned true with empty rows")
	}
	if len(db.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(db.queries))
	}
	q := db.queries[0]
	if !strings.Contains(q.query, "SELECT EXISTS") {
		t.Fatalf("IsComplete query missing SELECT EXISTS:\n%s", q.query)
	}
	if !strings.Contains(q.query, "code_value_flow_backfill_state") {
		t.Fatalf("IsComplete query missing table:\n%s", q.query)
	}
	if !strings.Contains(q.query, "backfill_key = $1") {
		t.Fatalf("IsComplete query missing backfill_key filter:\n%s", q.query)
	}
	if len(q.args) != 1 || q.args[0] != "backfill-key" {
		t.Fatalf("args wrong: %+v", q.args)
	}
}

// TestCodeValueFlowBackfillStateStoreMarkCompleteQueryShape proves the upsert
// query has the correct shape.
func TestCodeValueFlowBackfillStateStoreMarkCompleteQueryShape(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeValueFlowBackfillStateStore(db)
	at := time.Date(2026, time.July, 8, 0, 0, 0, 0, time.UTC)

	err := store.MarkComplete(context.Background(), "backfill-key", at)
	if err != nil {
		t.Fatalf("MarkComplete error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	if !strings.Contains(query, "INSERT INTO code_value_flow_backfill_state") {
		t.Fatalf("MarkComplete query missing INSERT:\n%s", query)
	}
	if !strings.Contains(query, "ON CONFLICT (backfill_key) DO NOTHING") {
		t.Fatalf("MarkComplete query missing ON CONFLICT DO NOTHING:\n%s", query)
	}
	args := db.execs[0].args
	if len(args) != 2 || args[0] != "backfill-key" {
		t.Fatalf("args wrong: %+v", args)
	}
}

// TestCodeValueFlowBackfillStateStoreIsCompleteTrue proves the EXISTS query
// returns true when the marker exists.
func TestCodeValueFlowBackfillStateStoreIsCompleteTrue(t *testing.T) {
	t.Parallel()

	db := ledgerHasRowsDB{result: true}
	store := NewCodeValueFlowBackfillStateStore(db)

	complete, err := store.IsComplete(context.Background(), "backfill-key")
	if err != nil {
		t.Fatalf("IsComplete error: %v", err)
	}
	if !complete {
		t.Fatalf("IsComplete returned false, want true")
	}
}

// TestCodeValueFlowBackfillStateStoreIsCompleteFalse proves the EXISTS query
// returns false when the marker does not exist.
func TestCodeValueFlowBackfillStateStoreIsCompleteFalse(t *testing.T) {
	t.Parallel()

	db := ledgerHasRowsDB{result: false}
	store := NewCodeValueFlowBackfillStateStore(db)

	complete, err := store.IsComplete(context.Background(), "backfill-key")
	if err != nil {
		t.Fatalf("IsComplete error: %v", err)
	}
	if complete {
		t.Fatalf("IsComplete returned true, want false")
	}
}

// TestCodeValueFlowBackfillStateMigrationSchemaSQLParity proves the Go DDL
// constant is byte-identical to the migration file embedded SQL.
func TestCodeValueFlowBackfillStateMigrationSchemaSQLParity(t *testing.T) {
	t.Parallel()

	goDDL := CodeValueFlowBackfillStateSchemaSQL()
	migrationSQL := MigrationSQL("code_value_flow_backfill_state")
	if goDDL != migrationSQL {
		t.Fatalf("Go DDL != migration SQL.\nGo DDL:\n%s\nMigration:\n%s", goDDL, migrationSQL)
	}
}
