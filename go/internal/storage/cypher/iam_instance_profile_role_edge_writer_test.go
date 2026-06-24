// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func iamInstanceProfileRoleEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"profile_uid":       "profile-" + string(rune('a'+i)),
			"role_uid":          "role-" + string(rune('a'+i)),
			"relationship_type": "HAS_ROLE",
			"resolution_mode":   "arn",
		})
	}
	return rows
}

func TestIAMInstanceProfileRoleEdgeWriterUsesStaticTokenMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMInstanceProfileRoleEdgeWriter(executor, 0)

	if err := writer.WriteIAMInstanceProfileRoleEdges(context.Background(), iamInstanceProfileRoleEdgeRows(1), "scope-1", "gen-1", "reducer/iam-instance-profile-role"); err != nil {
		t.Fatalf("WriteIAMInstanceProfileRoleEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MATCH (profile:CloudResource {uid: row.profile_uid})",
		"MATCH (role:CloudResource {uid: row.role_uid})",
		"MERGE (profile)-[rel:HAS_ROLE]->(role)",
		"rel.scope_id = row.scope_id",
		"rel.generation_id = row.generation_id",
		"rel.evidence_source = row.evidence_source",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "MERGE (profile)-[rel:HAS_ROLE {") {
		t.Fatalf("MERGE must not carry a relationship-property map:\n%s", cypher)
	}
}

func TestIAMInstanceProfileRoleEdgeWriterRejectsForeignRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMInstanceProfileRoleEdgeWriter(executor, 0)
	rows := []map[string]any{{
		"profile_uid":       "profile-a",
		"role_uid":          "role-a",
		"relationship_type": "HAS_ANY_ROLE",
		"resolution_mode":   "arn",
	}}

	if err := writer.WriteIAMInstanceProfileRoleEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/iam-instance-profile-role"); err == nil {
		t.Fatal("WriteIAMInstanceProfileRoleEdges accepted an out-of-vocabulary relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestIAMInstanceProfileRoleEdgeWriterRetractScopedToEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMInstanceProfileRoleEdgeWriter(executor, 0)

	if err := writer.RetractIAMInstanceProfileRoleEdges(context.Background(), []string{"scope-1"}, "gen-1", "reducer/iam-instance-profile-role"); err != nil {
		t.Fatalf("RetractIAMInstanceProfileRoleEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{"[rel:HAS_ROLE]", "rel.scope_id IN $scope_ids", "rel.evidence_source = $evidence_source", "DELETE rel"} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract cypher missing %q:\n%s", want, cypher)
		}
	}
}
