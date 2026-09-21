// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// TestCloudResourceNodeWriterRetractEmptyUIDsIsNoOp is issue #6887's
// writer-level contract: retracting nothing executes nothing.
func TestCloudResourceNodeWriterRetractEmptyUIDsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 0)

	if err := writer.RetractCloudResourceNodes(context.Background(), nil, "reducer/aws-resources"); err != nil {
		t.Fatalf("RetractCloudResourceNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty uids", len(executor.calls))
	}
}

// TestCloudResourceNodeWriterRetractAnchorsOnUID pins the #6887 delete shape:
// a uid-anchored MATCH plus DETACH DELETE — never a bare-label scan (which
// costs a whole-store scan on NornicDB even with zero matches, #6822) and
// never an evidence_source predicate (the global live-check already proved no
// family is live; predicating on last-writer evidence_source would strand
// nodes rewritten by a sibling family).
func TestCloudResourceNodeWriterRetractAnchorsOnUID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 0)

	uids := []string{"uid-dead-1", "uid-dead-2"}
	if err := writer.RetractCloudResourceNodes(context.Background(), uids, "reducer/aws-resources"); err != nil {
		t.Fatalf("RetractCloudResourceNodes returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	stmt := executor.calls[0]
	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (n:CloudResource {uid: row.uid})") {
		t.Fatalf("cypher must anchor on uid identity only:\n%s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "DETACH DELETE n") {
		t.Fatalf("cypher must DETACH DELETE the anchored node:\n%s", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "MERGE") {
		t.Fatalf("retract cypher must never MERGE (never-create):\n%s", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "evidence_source =") || strings.Contains(stmt.Cypher, "evidence_source=") {
		t.Fatalf("retract cypher must not predicate on evidence_source:\n%s", stmt.Cypher)
	}
	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", stmt.Parameters["rows"])
	}
	if len(rows) != len(uids) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(uids))
	}
	for i, row := range rows {
		if row["uid"] != uids[i] {
			t.Fatalf("rows[%d][uid] = %v, want %q", i, row["uid"], uids[i])
		}
	}
}

// TestCloudResourceNodeWriterRetractBatchesUIDs proves the delete stays
// bounded: uid batches split at the writer batch size like the upsert path.
func TestCloudResourceNodeWriterRetractBatchesUIDs(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 2)

	uids := []string{"u1", "u2", "u3", "u4", "u5"}
	if err := writer.RetractCloudResourceNodes(context.Background(), uids, "reducer/aws-resources"); err != nil {
		t.Fatalf("RetractCloudResourceNodes returned error: %v", err)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batches for 5 uids at batch size 2", len(executor.calls))
	}
}

// TestCloudResourceNodeWriterRetractNeverUsesGroupExecutor pins the #4367
// precedent for deletes: retract statements run sequentially through Execute,
// never batched into an ExecuteGroup transaction where NornicDB can
// under-apply them.
func TestCloudResourceNodeWriterRetractNeverUsesGroupExecutor(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewCloudResourceNodeWriter(executor, 2)

	uids := []string{"u1", "u2", "u3"}
	if err := writer.RetractCloudResourceNodes(context.Background(), uids, "reducer/aws-resources"); err != nil {
		t.Fatalf("RetractCloudResourceNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("len(groupCalls) = %d, want 0: retract must never use ExecuteGroup", len(executor.groupCalls))
	}
	if len(executor.executeCalls) != 2 {
		t.Fatalf("len(executeCalls) = %d, want 2 sequential batches", len(executor.executeCalls))
	}
}

// TestCloudResourceNodeWriterRetractRequiresExecutor fails closed on a nil
// executor rather than silently dropping a delete.
func TestCloudResourceNodeWriterRetractRequiresExecutor(t *testing.T) {
	t.Parallel()

	writer := NewCloudResourceNodeWriter(nil, 0)

	if err := writer.RetractCloudResourceNodes(context.Background(), []string{"u1"}, "reducer/aws-resources"); err == nil {
		t.Fatal("RetractCloudResourceNodes with nil executor = nil, want error")
	}
}
