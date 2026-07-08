// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestProjectedSourceEdgeStoreSchemaSQL proves the migration DDL includes the
// expected table and every expected index.
func TestProjectedSourceEdgeStoreSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := ProjectedSourceEdgeSchemaSQL()
	for _, want := range []string{
		"projected_source_edge",
		"projected_source_edge_source_scope_idx",
		"projected_source_edge_source_idx",
		"projected_source_edge_stale_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("ProjectedSourceEdgeSchemaSQL() missing %q:\n%s", want, sql)
		}
	}
}

// TestProjectedSourceEdgeStoreRecordDedupesAndSkipsBlanks proves
// RecordProjectedSources de-duplicates uids within a batch and skips blank
// uids.
func TestProjectedSourceEdgeStoreRecordDedupesAndSkipsBlanks(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewProjectedSourceEdgeStore(db)
	at := time.Date(2026, time.July, 7, 0, 0, 0, 0, time.UTC)

	err := store.RecordProjectedSources(
		context.Background(),
		"reducer/example-source",
		"scope-1",
		"gen-1",
		[]string{"uid-a", "", "uid-a", "uid-b"},
		at,
	)
	if err != nil {
		t.Fatalf("RecordProjectedSources error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	args := db.execs[0].args
	// 2 unique non-blank uids: uid-a, uid-b. Each row = 5 args.
	if len(args) != 10 {
		t.Fatalf("args count = %d, want 10 (2 rows * 5 columns)", len(args))
	}
	if args[0] != "reducer/example-source" || args[1] != "scope-1" || args[2] != "gen-1" {
		t.Fatalf("first row keys wrong: %+v", args[:3])
	}
	// Confirm the first uid is "uid-a" (sorted), second is "uid-b".
	if args[3] != "uid-a" {
		t.Fatalf("first uid = %v, want uid-a", args[3])
	}
	if args[8] != "uid-b" {
		t.Fatalf("second uid = %v, want uid-b", args[8])
	}
}

// TestProjectedSourceEdgeStoreRecordEmptyIsNoOp proves no write occurs when
// all uids are blank.
func TestProjectedSourceEdgeStoreRecordEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewProjectedSourceEdgeStore(db)

	if err := store.RecordProjectedSources(
		context.Background(),
		"reducer/example-source",
		"scope-1",
		"gen-1",
		nil,
		time.Now(),
	); err != nil {
		t.Fatalf("RecordProjectedSources error: %v", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec calls = %d, want 0", len(db.execs))
	}
}

// TestProjectedSourceEdgeStoreListSourceUIDsForScopes proves the scope list
// query has the correct shape and is isolated by evidence source.
func TestProjectedSourceEdgeStoreListSourceUIDsForScopes(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewProjectedSourceEdgeStore(db)

	_, err := store.ListSourceUIDsForScopes(
		context.Background(),
		"reducer/example-source",
		[]string{"scope-1", "scope-2"},
	)
	if err != nil {
		t.Fatalf("ListSourceUIDsForScopes error: %v", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(db.queries))
	}
	q := db.queries[0]
	if !strings.Contains(q.query, "DISTINCT source_uid") {
		t.Fatalf("ListSourceUIDsForScopes query missing DISTINCT:\n%s", q.query)
	}
	if !strings.Contains(q.query, "scope_id = ANY($2)") {
		t.Fatalf("ListSourceUIDsForScopes query missing ANY:\n%s", q.query)
	}
	if len(q.args) != 2 || q.args[0] != "reducer/example-source" {
		t.Fatalf("args wrong: %+v", q.args)
	}
}

// TestProjectedSourceEdgeStorePruneForScopes proves the scope prune query
// targets the right table and columns.
func TestProjectedSourceEdgeStorePruneForScopes(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewProjectedSourceEdgeStore(db)

	if err := store.PruneForScopes(
		context.Background(),
		"reducer/example-source",
		[]string{"scope-1"},
	); err != nil {
		t.Fatalf("PruneForScopes error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	if !strings.Contains(query, "DELETE FROM projected_source_edge") {
		t.Fatalf("PruneForScopes query missing DELETE:\n%s", query)
	}
	if !strings.Contains(query, "scope_id = ANY($2)") {
		t.Fatalf("PruneForScopes query missing ANY:\n%s", query)
	}
}

// TestProjectedSourceEdgeStoreEnsureSchemaRequiresDB proves EnsureSchema
// rejects a nil database rather than panicking.
func TestProjectedSourceEdgeStoreEnsureSchemaRequiresDB(t *testing.T) {
	t.Parallel()

	store := NewProjectedSourceEdgeStore(nil)
	if err := store.EnsureSchema(context.Background()); err == nil {
		t.Fatal("EnsureSchema() error = nil, want error for nil database")
	}
}

// TestProjectedSourceEdgeStoreEnsureSchemaAppliesDDL proves EnsureSchema
// executes the schema DDL against the database.
func TestProjectedSourceEdgeStoreEnsureSchemaAppliesDDL(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewProjectedSourceEdgeStore(db)

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	if db.execs[0].query != ProjectedSourceEdgeSchemaSQL() {
		t.Fatalf("EnsureSchema executed query does not match ProjectedSourceEdgeSchemaSQL()")
	}
}

// TestProjectedSourceEdgeMigrationSchemaSQLParity proves the Go DDL constant
// is byte-identical to the migration file embedded SQL.
func TestProjectedSourceEdgeMigrationSchemaSQLParity(t *testing.T) {
	t.Parallel()

	goDDL := ProjectedSourceEdgeSchemaSQL()
	migrationSQL := MigrationSQL("projected_source_edge")
	if goDDL != migrationSQL {
		t.Fatalf("Go DDL != migration SQL.\nGo DDL:\n%s\nMigration:\n%s", goDDL, migrationSQL)
	}
}
