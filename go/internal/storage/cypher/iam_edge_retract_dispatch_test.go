// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// No-group dispatch guards for the C-14 #4367 IAM-edge retract backfill
// (CAN_ASSUME, CAN_ESCALATE_TO, CAN_PERFORM).
//
// On the pinned NornicDB v1.1.11 a retract DELETE dispatched through
// ExecuteGroup / a managed transaction under-applies even for a single
// statement (docs/public/reference/nornicdb-pitfalls.md). IAMCanAssumeEdgeWriter,
// IAMEscalationEdgeWriter, and IAMCanPerformEdgeWriter each routed their single
// retract statement through the shared dispatch() helper, which uses
// ExecuteGroup whenever the executor implements GroupExecutor -- the shape
// cmd/reducer wires unconditionally for every graph backend including
// NornicDB. Each writer now routes its Retract* method through a
// dispatchRetract helper (sequential Execute, never ExecuteGroup).
//
// These guards use sqlSequentialRecordingExecutor (edge_writer_sql_retract_
// test.go), which implements GroupExecutor and records group calls, so a
// revert to the grouped dispatch() fails here. They mirror
// TestKubernetesCorrelationEdgeWriterRetractNeverGroups et al.
// (cloud_edge_retract_dispatch_test.go).

import (
	"context"
	"testing"
)

func TestIAMCanAssumeEdgeWriterRetractNeverGroups(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewIAMCanAssumeEdgeWriter(executor, 0)
	if err := writer.RetractIAMCanAssumeEdges(
		context.Background(), []string{"scope-1"}, "gen-1", "reducer/iam-can-assume",
	); err != nil {
		t.Fatalf("RetractIAMCanAssumeEdges returned error: %v", err)
	}
	assertSingleSequentialRetract(t, executor,
		"MATCH (:CloudResource)-[rel:CAN_ASSUME]->(:CloudResource)",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	)
}

func TestIAMEscalationEdgeWriterRetractNeverGroups(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewIAMEscalationEdgeWriter(executor, 0)
	if err := writer.RetractIAMEscalationEdges(
		context.Background(), []string{"scope-1"}, "gen-1", "reducer/iam-escalation",
	); err != nil {
		t.Fatalf("RetractIAMEscalationEdges returned error: %v", err)
	}
	assertSingleSequentialRetract(t, executor,
		"MATCH (p:CloudResource)-[rel:CAN_ESCALATE_TO]->()",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	)
}

func TestIAMCanPerformEdgeWriterRetractNeverGroups(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewIAMCanPerformEdgeWriter(executor, 0)
	if err := writer.RetractIAMCanPerformEdges(
		context.Background(), []string{"scope-1"}, "gen-1", "reducer/iam-can-perform",
	); err != nil {
		t.Fatalf("RetractIAMCanPerformEdges returned error: %v", err)
	}
	assertSingleSequentialRetract(t, executor,
		"MATCH (p:CloudResource)-[rel:CAN_PERFORM]->()",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	)
}
