// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// iamCanAssumeEdgeRows mirrors the rows ExtractIAMCanAssumeEdgeRows produces:
// the assuming-principal uid, the role-with-trust-policy uid, the closed
// relationship type, the assuming-principal kind, and the resolution mode. It
// omits scope_id/generation_id/evidence_source — the writer injects those
// reducer-scoped annotations from its call arguments.
func iamCanAssumeEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"principal_uid":     "principal-" + string(rune('a'+i)),
			"role_uid":          "role-" + string(rune('a'+i)),
			"relationship_type": "CAN_ASSUME",
			"principal_kind":    "role",
			"resolution_mode":   "arn",
		})
	}
	return rows
}

func TestIAMCanAssumeEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMCanAssumeEdgeWriter(executor, 0)

	if err := writer.WriteIAMCanAssumeEdges(context.Background(), nil, "scope-1", "gen-1", "reducer/iam-can-assume"); err != nil {
		t.Fatalf("WriteIAMCanAssumeEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestIAMCanAssumeEdgeWriterUsesStaticTokenMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMCanAssumeEdgeWriter(executor, 0)

	if err := writer.WriteIAMCanAssumeEdges(context.Background(), iamCanAssumeEdgeRows(1), "scope-1", "gen-1", "reducer/iam-can-assume"); err != nil {
		t.Fatalf("WriteIAMCanAssumeEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	// Two MATCHes before the MERGE guarantee a missing endpoint is a no-op,
	// never a fabricated node — the graceful-degradation contract from #805.
	if !strings.Contains(cypher, "MATCH (principal:CloudResource {uid: row.principal_uid})") {
		t.Fatalf("cypher must MATCH the assuming-principal CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (role:CloudResource {uid: row.role_uid})") {
		t.Fatalf("cypher must MATCH the role CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (principal)-[rel:CAN_ASSUME]->(role)") {
		t.Fatalf("edge MERGE must use the static CAN_ASSUME relationship type:\n%s", cypher)
	}
	// scope_id/generation_id/evidence_source are stamped onto the edge so the
	// prior-generation retract can scope to reducer-owned edges.
	for _, want := range []string{"rel.scope_id = row.scope_id", "rel.generation_id = row.generation_id", "rel.evidence_source = row.evidence_source"} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
}

func TestIAMCanAssumeEdgeWriterRejectsForeignRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMCanAssumeEdgeWriter(executor, 0)

	// A row whose relationship_type is outside the closed single-member
	// vocabulary must be rejected, never interpolated into the schema surface.
	rows := []map[string]any{{
		"principal_uid":     "principal-a",
		"role_uid":          "role-a",
		"relationship_type": "CAN_ASSUME_EVERYTHING",
		"principal_kind":    "role",
		"resolution_mode":   "arn",
	}}
	if err := writer.WriteIAMCanAssumeEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/iam-can-assume"); err == nil {
		t.Fatal("WriteIAMCanAssumeEdges accepted an out-of-vocabulary relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestIAMCanAssumeEdgeWriterRejectsInjectionToken(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMCanAssumeEdgeWriter(executor, 0)

	rows := []map[string]any{{
		"principal_uid":     "principal-a",
		"role_uid":          "role-a",
		"relationship_type": "CAN_ASSUME]->() DELETE n //",
		"principal_kind":    "role",
		"resolution_mode":   "arn",
	}}
	if err := writer.WriteIAMCanAssumeEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/iam-can-assume"); err == nil {
		t.Fatal("WriteIAMCanAssumeEdges accepted an injection-shaped relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestIAMCanAssumeEdgeWriterRetractScopedToEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMCanAssumeEdgeWriter(executor, 0)

	if err := writer.RetractIAMCanAssumeEdges(context.Background(), []string{"scope-1"}, "gen-1", "reducer/iam-can-assume"); err != nil {
		t.Fatalf("RetractIAMCanAssumeEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "[rel:CAN_ASSUME]") {
		t.Fatalf("retract must match the CAN_ASSUME relationship type:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.scope_id IN $scope_ids") {
		t.Fatalf("retract must scope by the edge's own scope_id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("retract must DELETE only the edge, never the nodes:\n%s", cypher)
	}
}

func TestIAMCanAssumeEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMCanAssumeEdgeWriter(executor, 0)

	if err := writer.RetractIAMCanAssumeEdges(context.Background(), nil, "gen-1", "reducer/iam-can-assume"); err != nil {
		t.Fatalf("RetractIAMCanAssumeEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}
