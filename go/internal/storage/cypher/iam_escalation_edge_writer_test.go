// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

const iamEscalationEvidence = "reducer/iam-escalation"

func iamEscalationEdgeRows() []map[string]any {
	return []map[string]any{
		{
			"principal_uid":   "principal-a",
			"target_uid":      "policy-a",
			"primitives":      []string{"iam_create_policy_version"},
			"primitive_count": 1,
		},
		{
			"principal_uid":   "principal-a",
			"target_uid":      "role-b",
			"primitives":      []string{"iam_attach_role_policy", "iam_put_role_policy"},
			"primitive_count": 2,
		},
	}
}

// TestIAMEscalationEdgeWriterEmptyRowsIsNoOp proves an empty generation issues no
// statements.
func TestIAMEscalationEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMEscalationEdgeWriter(executor, 0)
	if err := writer.WriteIAMEscalationEdges(context.Background(), nil, "scope-1", "gen-1", iamEscalationEvidence); err != nil {
		t.Fatalf("WriteIAMEscalationEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

// TestIAMEscalationEdgeWriterStaticTypeAndDualMatch proves the writer MERGEs on the
// static CAN_ESCALATE_TO type over two uid-indexed CloudResource anchors and never
// fabricates a node. This pins the perf-critical static-token + dual-MATCH contract.
func TestIAMEscalationEdgeWriterStaticTypeAndDualMatch(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMEscalationEdgeWriter(executor, 500)
	if err := writer.WriteIAMEscalationEdges(context.Background(), iamEscalationEdgeRows(), "scope-1", "gen-1", iamEscalationEvidence); err != nil {
		t.Fatalf("WriteIAMEscalationEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 || len(executor.groupCalls[0]) != 1 {
		t.Fatalf("expected one atomic group of one batched statement, got %v", executor.groupCalls)
	}
	cypher := executor.groupCalls[0][0].Cypher
	if !strings.Contains(cypher, "MATCH (p:CloudResource {uid: row.principal_uid})") {
		t.Fatalf("must MATCH the principal CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (t:CloudResource {uid: row.target_uid})") {
		t.Fatalf("must MATCH the target CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (p)-[rel:CAN_ESCALATE_TO]->(t)") {
		t.Fatalf("must MERGE on the static CAN_ESCALATE_TO type:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (p:CloudResource") || strings.Contains(cypher, "MERGE (t:CloudResource") {
		t.Fatalf("must not fabricate endpoint nodes:\n%s", cypher)
	}
}

// TestIAMEscalationEdgeWriterKeepsPrimitiveOutOfMergeKey proves the escalation
// primitive set is a SET property, NOT part of the MERGE identity — the keying
// decision that keeps the MERGE static-token (catalog doc §5, perf trap avoidance).
func TestIAMEscalationEdgeWriterKeepsPrimitiveOutOfMergeKey(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMEscalationEdgeWriter(executor, 500)
	if err := writer.WriteIAMEscalationEdges(context.Background(), iamEscalationEdgeRows(), "scope-1", "gen-1", iamEscalationEvidence); err != nil {
		t.Fatalf("WriteIAMEscalationEdges returned error: %v", err)
	}
	cypher := executor.groupCalls[0][0].Cypher
	mergeLine, _, _ := strings.Cut(cypher[strings.Index(cypher, "MERGE"):], "\n") //nolint:gocritic // offBy1: the test fixture builds Cypher with at least one MERGE clause, so Index() != -1.
	if strings.Contains(mergeLine, "primitive") {
		t.Fatalf("primitive must NOT be in the MERGE key (property-keyed MERGE perf trap):\n%s", mergeLine)
	}
	if !strings.Contains(cypher, "SET rel.primitives = row.primitives") {
		t.Fatalf("primitives must be a SET list property:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = row.evidence_source") {
		t.Fatalf("evidence_source must be stamped on the edge for scoped retract:\n%s", cypher)
	}
}

// TestIAMEscalationEdgeWriterStampsScopeFields proves the writer injects
// scope_id/generation_id/evidence_source onto every row (the retract filter
// depends on rel.scope_id, not the endpoint nodes).
func TestIAMEscalationEdgeWriterStampsScopeFields(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMEscalationEdgeWriter(executor, 500)
	if err := writer.WriteIAMEscalationEdges(context.Background(), iamEscalationEdgeRows(), "scope-7", "gen-9", iamEscalationEvidence); err != nil {
		t.Fatalf("WriteIAMEscalationEdges returned error: %v", err)
	}
	rows := executor.groupCalls[0][0].Parameters["rows"].([]map[string]any)
	for _, row := range rows {
		if row["scope_id"] != "scope-7" || row["generation_id"] != "gen-9" || row["evidence_source"] != iamEscalationEvidence {
			t.Fatalf("row missing stamped scope fields: %v", row)
		}
	}
}

// TestIAMEscalationEdgeWriterIdempotentInputDoesNotMutateCaller proves the writer
// clones rows (never mutates the extractor's resolved row maps), so a retry that
// re-passes the same slice is safe.
func TestIAMEscalationEdgeWriterIdempotentInputDoesNotMutateCaller(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMEscalationEdgeWriter(executor, 500)
	rows := iamEscalationEdgeRows()
	if err := writer.WriteIAMEscalationEdges(context.Background(), rows, "scope-1", "gen-1", iamEscalationEvidence); err != nil {
		t.Fatalf("WriteIAMEscalationEdges returned error: %v", err)
	}
	for _, row := range rows {
		if _, ok := row["scope_id"]; ok {
			t.Fatalf("writer mutated caller row with scope_id: %v", row)
		}
	}
}

// TestIAMEscalationEdgeRetractScopesByEvidenceSource proves the retract deletes
// only this reducer's edges, scoped by scope_id + evidence_source, never
// touches endpoint nodes, and never dispatches through ExecuteGroup (a
// managed transaction under-applies a retract DELETE on NornicDB v1.1.11;
// docs/public/reference/nornicdb-pitfalls.md). sqlSequentialRecordingExecutor
// implements GroupExecutor and records group calls, so a revert to the
// grouped dispatch() would fail this test.
func TestIAMEscalationEdgeRetractScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewIAMEscalationEdgeWriter(executor, 500)
	if err := writer.RetractIAMEscalationEdges(context.Background(), []string{"scope-1"}, "gen-1", iamEscalationEvidence); err != nil {
		t.Fatalf("RetractIAMEscalationEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0 (grouped DELETEs under-apply on NornicDB v1.1.11)", len(executor.groupCalls))
	}
	if len(executor.calls) != 1 {
		t.Fatalf("expected one sequential retract statement, got %v", executor.calls)
	}
	stmt := executor.calls[0]
	if !strings.Contains(stmt.Cypher, "[rel:CAN_ESCALATE_TO]") {
		t.Fatalf("retract must target the CAN_ESCALATE_TO type:\n%s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.scope_id IN $scope_ids") || !strings.Contains(stmt.Cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by edge scope_id AND evidence_source:\n%s", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "DELETE p") || strings.Contains(stmt.Cypher, "DELETE t") {
		t.Fatalf("retract must not delete endpoint nodes:\n%s", stmt.Cypher)
	}
}

// TestIAMEscalationEdgeRetractEmptyScopesIsNoOp proves an empty scope set issues no
// statement.
func TestIAMEscalationEdgeRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMEscalationEdgeWriter(executor, 500)
	if err := writer.RetractIAMEscalationEdges(context.Background(), nil, "gen-1", iamEscalationEvidence); err != nil {
		t.Fatalf("RetractIAMEscalationEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("empty scope set must be a no-op, got %v", executor.groupCalls)
	}
}
