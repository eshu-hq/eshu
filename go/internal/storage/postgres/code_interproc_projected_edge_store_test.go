// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestCodeInterprocProjectedEdgeStoreSchemaSQL proves the migration DDL
// includes the expected table and every expected index.
func TestCodeInterprocProjectedEdgeStoreSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := CodeInterprocProjectedEdgeSchemaSQL()
	for _, want := range []string{
		"code_interproc_projected_edge",
		"code_interproc_projected_edge_source_scope_idx",
		"code_interproc_projected_edge_source_idx",
		"code_interproc_projected_edge_stale_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("CodeInterprocProjectedEdgeSchemaSQL() missing %q:\n%s", want, sql)
		}
	}
}

// TestCodeInterprocProjectedEdgeStoreRecordDedupesAndSkipsBlanks proves
// RecordProjectedEdges de-duplicates uids within a batch and skips blank uids.
func TestCodeInterprocProjectedEdgeStoreRecordDedupesAndSkipsBlanks(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeInterprocProjectedEdgeStore(db)
	at := time.Date(2026, time.July, 7, 0, 0, 0, 0, time.UTC)

	err := store.RecordProjectedEdges(
		context.Background(),
		"reducer/code-interproc",
		"scope-1",
		"gen-1",
		[]string{"uid-a", "", "uid-a", "uid-b"},
		at,
	)
	if err != nil {
		t.Fatalf("RecordProjectedEdges error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	args := db.execs[0].args
	// 2 unique non-blank uids: uid-a, uid-b. Each row = 5 args.
	if len(args) != 10 {
		t.Fatalf("args count = %d, want 10 (2 rows * 5 columns)", len(args))
	}
	if args[0] != "reducer/code-interproc" || args[1] != "scope-1" || args[2] != "gen-1" {
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

// TestCodeInterprocProjectedEdgeStoreRecordEmptyIsNoOp proves no write occurs
// when all uids are blank.
func TestCodeInterprocProjectedEdgeStoreRecordEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeInterprocProjectedEdgeStore(db)

	if err := store.RecordProjectedEdges(
		context.Background(),
		"reducer/code-interproc",
		"scope-1",
		"gen-1",
		nil,
		time.Now(),
	); err != nil {
		t.Fatalf("RecordProjectedEdges error: %v", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec calls = %d, want 0", len(db.execs))
	}
}

// TestCodeInterprocProjectedEdgeStoreListSourceUIDsForScopes proves the scope
// list query has the correct shape.
func TestCodeInterprocProjectedEdgeStoreListSourceUIDsForScopes(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeInterprocProjectedEdgeStore(db)

	_, err := store.ListSourceUIDsForScopes(
		context.Background(),
		"reducer/code-interproc",
		[]string{"scope-1", "scope-2"},
	)
	if err != nil {
		t.Fatalf("ListSourceUIDsForScopes error: %v", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(db.queries))
	}
	q := db.queries[0]
	if !strings.Contains(q.query, "DISTINCT source_function_uid") {
		t.Fatalf("ListSourceUIDsForScopes query missing DISTINCT:\n%s", q.query)
	}
	if !strings.Contains(q.query, "scope_id = ANY($2)") {
		t.Fatalf("ListSourceUIDsForScopes query missing ANY:\n%s", q.query)
	}
	if len(q.args) != 2 || q.args[0] != "reducer/code-interproc" {
		t.Fatalf("args wrong: %+v", q.args)
	}
}

// TestCodeInterprocProjectedEdgeStoreListStaleSourceUIDs proves the stale
// query filters by generation_id <> current.
func TestCodeInterprocProjectedEdgeStoreListStaleSourceUIDs(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeInterprocProjectedEdgeStore(db)

	_, err := store.ListStaleSourceUIDs(
		context.Background(),
		"reducer/code-interproc",
		"scope-1",
		"gen-current",
		500,
	)
	if err != nil {
		t.Fatalf("ListStaleSourceUIDs error: %v", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(db.queries))
	}
	q := db.queries[0]
	if !strings.Contains(q.query, "generation_id <> $3") {
		t.Fatalf("ListStaleSourceUIDs query missing generation_id <>:\n%s", q.query)
	}
	if !strings.Contains(q.query, "LIMIT $4") {
		t.Fatalf("ListStaleSourceUIDs query missing LIMIT:\n%s", q.query)
	}
	if len(q.args) != 4 || q.args[0] != "reducer/code-interproc" || q.args[1] != "scope-1" {
		t.Fatalf("args wrong: %+v", q.args)
	}
}

// TestCodeInterprocProjectedEdgeStorePruneForScopes proves the scope prune
// query targets the right table and columns.
func TestCodeInterprocProjectedEdgeStorePruneForScopes(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeInterprocProjectedEdgeStore(db)

	if err := store.PruneForScopes(
		context.Background(),
		"reducer/code-interproc",
		[]string{"scope-1"},
	); err != nil {
		t.Fatalf("PruneForScopes error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	if !strings.Contains(query, "DELETE FROM code_interproc_projected_edge") {
		t.Fatalf("PruneForScopes query missing DELETE:\n%s", query)
	}
	if !strings.Contains(query, "scope_id = ANY($2)") {
		t.Fatalf("PruneForScopes query missing ANY:\n%s", query)
	}
}

// TestCodeInterprocProjectedEdgeStorePruneStale proves the stale prune
// deletes rows with generation_id <> current.
func TestCodeInterprocProjectedEdgeStorePruneStale(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeInterprocProjectedEdgeStore(db)

	if err := store.PruneStale(
		context.Background(),
		"reducer/code-interproc",
		"scope-1",
		"gen-current",
	); err != nil {
		t.Fatalf("PruneStale error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	if !strings.Contains(query, "DELETE FROM code_interproc_projected_edge") {
		t.Fatalf("PruneStale query missing DELETE:\n%s", query)
	}
	if !strings.Contains(query, "generation_id <> $3") {
		t.Fatalf("PruneStale query missing generation_id <>:\n%s", query)
	}
}

// TestCodeInterprocProjectedEdgeMigrationSchemaSQLParity proves the Go DDL
// constant is byte-identical to the migration file embedded SQL.
func TestCodeInterprocProjectedEdgeMigrationSchemaSQLParity(t *testing.T) {
	t.Parallel()

	goDDL := CodeInterprocProjectedEdgeSchemaSQL()
	migrationSQL := MigrationSQL("code_interproc_projected_edge")
	if goDDL != migrationSQL {
		t.Fatalf("Go DDL != migration SQL.\nGo DDL:\n%s\nMigration:\n%s", goDDL, migrationSQL)
	}
}
