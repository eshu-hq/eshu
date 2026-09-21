// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// TestEC2InstanceNodeWriterRetractEmptyUIDsIsNoOp is issue #6887's EC2-side
// writer contract: retracting nothing executes nothing.
func TestEC2InstanceNodeWriterRetractEmptyUIDsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceNodeWriter(executor, 0)

	if err := writer.RetractEC2InstanceNodes(context.Background(), nil, "reducer/ec2-instances"); err != nil {
		t.Fatalf("RetractEC2InstanceNodes returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty uids", len(executor.calls))
	}
}

// TestEC2InstanceNodeWriterRetractAnchorsOnUID pins the #6887 EC2 delete
// shape: the same uid-anchored MATCH plus DETACH DELETE as the generic cloud
// writer — EC2 nodes share the cloud_resource_uid keyspace and the
// cloud_resource_uid_unique constraint, so one anchored shape serves both
// families. No evidence_source predicate, for the same strand-avoidance
// reason as the generic writer.
func TestEC2InstanceNodeWriterRetractAnchorsOnUID(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2InstanceNodeWriter(executor, 0)

	uids := []string{"uid-ec2-dead-1"}
	if err := writer.RetractEC2InstanceNodes(context.Background(), uids, "reducer/ec2-instances"); err != nil {
		t.Fatalf("RetractEC2InstanceNodes returned error: %v", err)
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
}

// TestEC2InstanceNodeWriterRetractNeverUsesGroupExecutor pins sequential
// Execute dispatch for EC2 deletes under the same #4367 precedent.
func TestEC2InstanceNodeWriterRetractNeverUsesGroupExecutor(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEC2InstanceNodeWriter(executor, 2)

	uids := []string{"u1", "u2", "u3"}
	if err := writer.RetractEC2InstanceNodes(context.Background(), uids, "reducer/ec2-instances"); err != nil {
		t.Fatalf("RetractEC2InstanceNodes returned error: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("len(groupCalls) = %d, want 0: retract must never use ExecuteGroup", len(executor.groupCalls))
	}
	if len(executor.executeCalls) != 2 {
		t.Fatalf("len(executeCalls) = %d, want 2 sequential batches", len(executor.executeCalls))
	}
}

// TestEC2InstanceNodeWriterRetractRequiresExecutor fails closed on a nil
// executor rather than silently dropping a delete.
func TestEC2InstanceNodeWriterRetractRequiresExecutor(t *testing.T) {
	t.Parallel()

	writer := NewEC2InstanceNodeWriter(nil, 0)

	if err := writer.RetractEC2InstanceNodes(context.Background(), []string{"u1"}, "reducer/ec2-instances"); err == nil {
		t.Fatal("RetractEC2InstanceNodes with nil executor = nil, want error")
	}
}
