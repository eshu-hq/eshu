// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestCodeTaintEvidenceProjectedNodeStoreSchemaSQL proves the migration DDL
// includes the expected table and every expected index.
func TestCodeTaintEvidenceProjectedNodeStoreSchemaSQL(t *testing.T) {
	t.Parallel()

	d := CodeTaintEvidenceProjectedNodeSchemaSQL()
	for _, want := range []string{
		"code_taint_evidence_projected_node",
		"code_taint_evidence_projected_node_source_scope_idx",
		"code_taint_evidence_projected_node_source_idx",
		"code_taint_evidence_projected_node_stale_idx",
	} {
		if !strings.Contains(d, want) {
			t.Fatalf("CodeTaintEvidenceProjectedNodeSchemaSQL() missing %q:\n%s", want, d)
		}
	}
}

// TestCodeTaintEvidenceProjectedNodeStoreRecordDedupesAndSkipsBlanks proves
// RecordProjectedNodes de-duplicates uids within a batch and skips blank uids.
func TestCodeTaintEvidenceProjectedNodeStoreRecordDedupesAndSkipsBlanks(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeTaintEvidenceProjectedNodeStore(db)
	at := time.Date(2026, time.July, 7, 0, 0, 0, 0, time.UTC)

	err := store.RecordProjectedNodes(
		context.Background(),
		"reducer/code-taint",
		"scope-1",
		"gen-1",
		[]string{"uid-a", "", "uid-a", "uid-b"},
		at,
	)
	if err != nil {
		t.Fatalf("RecordProjectedNodes error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	args := db.execs[0].args
	// 2 unique non-blank uids: uid-a, uid-b. Each row = 5 args.
	if len(args) != 10 {
		t.Fatalf("args count = %d, want 10 (2 rows * 5 columns)", len(args))
	}
	if args[0] != "reducer/code-taint" || args[1] != "scope-1" || args[2] != "gen-1" {
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

// TestCodeTaintEvidenceProjectedNodeStoreRecordEmptyIsNoOp proves no write
// occurs when all uids are blank.
func TestCodeTaintEvidenceProjectedNodeStoreRecordEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeTaintEvidenceProjectedNodeStore(db)

	if err := store.RecordProjectedNodes(
		context.Background(),
		"reducer/code-taint",
		"scope-1",
		"gen-1",
		nil,
		time.Now(),
	); err != nil {
		t.Fatalf("RecordProjectedNodes error: %v", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec calls = %d, want 0", len(db.execs))
	}
}

// TestCodeTaintEvidenceProjectedNodeStoreListNodeUIDsForScopes proves the scope
// list query has the correct shape.
func TestCodeTaintEvidenceProjectedNodeStoreListNodeUIDsForScopes(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeTaintEvidenceProjectedNodeStore(db)

	_, err := store.ListNodeUIDsForScopes(
		context.Background(),
		"reducer/code-taint",
		[]string{"scope-1", "scope-2"},
	)
	if err != nil {
		t.Fatalf("ListNodeUIDsForScopes error: %v", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(db.queries))
	}
	q := db.queries[0]
	if !strings.Contains(q.query, "DISTINCT node_uid") {
		t.Fatalf("ListNodeUIDsForScopes query missing DISTINCT:\n%s", q.query)
	}
	if !strings.Contains(q.query, "scope_id = ANY($2)") {
		t.Fatalf("ListNodeUIDsForScopes query missing ANY:\n%s", q.query)
	}
	if len(q.args) != 2 || q.args[0] != "reducer/code-taint" {
		t.Fatalf("args wrong: %+v", q.args)
	}
}

// TestCodeTaintEvidenceProjectedNodeStoreListStaleNodeUIDs proves the stale
// query filters by generation_id <> current.
func TestCodeTaintEvidenceProjectedNodeStoreListStaleNodeUIDs(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeTaintEvidenceProjectedNodeStore(db)

	_, err := store.ListStaleNodeUIDs(
		context.Background(),
		"reducer/code-taint",
		"scope-1",
		"gen-current",
		500,
	)
	if err != nil {
		t.Fatalf("ListStaleNodeUIDs error: %v", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(db.queries))
	}
	q := db.queries[0]
	if !strings.Contains(q.query, "generation_id <> $3") {
		t.Fatalf("ListStaleNodeUIDs query missing generation_id <>:\n%s", q.query)
	}
	if !strings.Contains(q.query, "LIMIT $4") {
		t.Fatalf("ListStaleNodeUIDs query missing LIMIT:\n%s", q.query)
	}
	if len(q.args) != 4 || q.args[0] != "reducer/code-taint" || q.args[1] != "scope-1" {
		t.Fatalf("args wrong: %+v", q.args)
	}
}

// TestCodeTaintEvidenceProjectedNodeStorePruneForScopes proves the scope prune
// query targets the right table and columns.
func TestCodeTaintEvidenceProjectedNodeStorePruneForScopes(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeTaintEvidenceProjectedNodeStore(db)

	if err := store.PruneForScopes(
		context.Background(),
		"reducer/code-taint",
		[]string{"scope-1"},
	); err != nil {
		t.Fatalf("PruneForScopes error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	if !strings.Contains(query, "DELETE FROM code_taint_evidence_projected_node") {
		t.Fatalf("PruneForScopes query missing DELETE:\n%s", query)
	}
	if !strings.Contains(query, "scope_id = ANY($2)") {
		t.Fatalf("PruneForScopes query missing ANY:\n%s", query)
	}
}

// TestCodeTaintEvidenceProjectedNodeStorePruneStale proves the stale prune
// deletes rows with generation_id <> current.
func TestCodeTaintEvidenceProjectedNodeStorePruneStale(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeTaintEvidenceProjectedNodeStore(db)

	if err := store.PruneStale(
		context.Background(),
		"reducer/code-taint",
		"scope-1",
		"gen-current",
	); err != nil {
		t.Fatalf("PruneStale error: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	if !strings.Contains(query, "DELETE FROM code_taint_evidence_projected_node") {
		t.Fatalf("PruneStale query missing DELETE:\n%s", query)
	}
	if !strings.Contains(query, "generation_id <> $3") {
		t.Fatalf("PruneStale query missing generation_id <>:\n%s", query)
	}
}

// TestCodeTaintEvidenceProjectedNodeStoreLedgerHasRowsForSourceQueryShape proves
// the EXISTS query has the correct shape and parameters.
func TestCodeTaintEvidenceProjectedNodeStoreLedgerHasRowsForSourceQueryShape(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewCodeTaintEvidenceProjectedNodeStore(db)

	hasRows, err := store.LedgerHasRowsForSource(
		context.Background(),
		"reducer/code-taint",
	)
	if err != nil {
		t.Fatalf("LedgerHasRowsForSource error: %v", err)
	}
	if hasRows {
		t.Fatalf("LedgerHasRowsForSource returned true with empty rows")
	}
	if len(db.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(db.queries))
	}
	q := db.queries[0]
	if !strings.Contains(q.query, "SELECT EXISTS") {
		t.Fatalf("LedgerHasRowsForSource query missing SELECT EXISTS:\n%s", q.query)
	}
	if !strings.Contains(q.query, "code_taint_evidence_projected_node") {
		t.Fatalf("LedgerHasRowsForSource query missing table:\n%s", q.query)
	}
	if !strings.Contains(q.query, "evidence_source = $1") {
		t.Fatalf("LedgerHasRowsForSource query missing evidence_source filter:\n%s", q.query)
	}
	if len(q.args) != 1 || q.args[0] != "reducer/code-taint" {
		t.Fatalf("args wrong: %+v", q.args)
	}
}

// TestCodeTaintEvidenceProjectedNodeStoreLedgerHasRowsForSourceTrue proves the
// EXISTS query returns true when the table has rows for the source.
func TestCodeTaintEvidenceProjectedNodeStoreLedgerHasRowsForSourceTrue(t *testing.T) {
	t.Parallel()

	db := ledgerHasRowsDB{result: true}
	store := NewCodeTaintEvidenceProjectedNodeStore(db)

	hasRows, err := store.LedgerHasRowsForSource(
		context.Background(),
		"reducer/code-taint",
	)
	if err != nil {
		t.Fatalf("LedgerHasRowsForSource error: %v", err)
	}
	if !hasRows {
		t.Fatalf("LedgerHasRowsForSource returned false, want true")
	}
}

// TestCodeTaintEvidenceProjectedNodeStoreLedgerHasRowsForSourceFalse proves the
// EXISTS query returns false when the table has no rows for the source.
func TestCodeTaintEvidenceProjectedNodeStoreLedgerHasRowsForSourceFalse(t *testing.T) {
	t.Parallel()

	db := ledgerHasRowsDB{result: false}
	store := NewCodeTaintEvidenceProjectedNodeStore(db)

	hasRows, err := store.LedgerHasRowsForSource(
		context.Background(),
		"reducer/code-taint",
	)
	if err != nil {
		t.Fatalf("LedgerHasRowsForSource error: %v", err)
	}
	if hasRows {
		t.Fatalf("LedgerHasRowsForSource returned true, want false")
	}
}

// TestCodeTaintEvidenceProjectedNodeMigrationSchemaSQLParity proves the Go DDL
// constant is byte-identical to the migration file embedded SQL.
func TestCodeTaintEvidenceProjectedNodeMigrationSchemaSQLParity(t *testing.T) {
	t.Parallel()

	goDDL := CodeTaintEvidenceProjectedNodeSchemaSQL()
	migrationSQL := MigrationSQL("code_taint_evidence_projected_node")
	if goDDL != migrationSQL {
		t.Fatalf("Go DDL != migration SQL.\nGo DDL:\n%s\nMigration:\n%s", goDDL, migrationSQL)
	}
}
