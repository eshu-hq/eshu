// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// ec2UsesProfileEdgeRows mirrors the rows ExtractEC2UsesProfileEdgeRows produces:
// the source EC2 instance uid, the target instance-profile uid, the closed
// relationship type, and the resolution mode. It omits
// scope_id/generation_id/evidence_source — the writer injects those
// reducer-scoped annotations from its call arguments.
func ec2UsesProfileEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        "ec2-" + string(rune('a'+i)),
			"target_uid":        "profile-" + string(rune('a'+i)),
			"relationship_type": "USES_PROFILE",
			"resolution_mode":   "arn",
		})
	}
	return rows
}

func TestEC2UsesProfileEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2UsesProfileEdgeWriter(executor, 0)

	if err := writer.WriteEC2UsesProfileEdges(context.Background(), nil, "scope-1", "gen-1", "reducer/ec2-uses-profile"); err != nil {
		t.Fatalf("WriteEC2UsesProfileEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestEC2UsesProfileEdgeWriterUsesStaticTokenMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2UsesProfileEdgeWriter(executor, 0)

	if err := writer.WriteEC2UsesProfileEdges(context.Background(), ec2UsesProfileEdgeRows(1), "scope-1", "gen-1", "reducer/ec2-uses-profile"); err != nil {
		t.Fatalf("WriteEC2UsesProfileEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	// Two anchored MATCHes before the MERGE guarantee a missing endpoint is a
	// no-op, never a fabricated node, and avoid any cartesian product.
	if !strings.Contains(cypher, "MATCH (source:CloudResource {uid: row.source_uid})") {
		t.Fatalf("cypher must MATCH the source EC2 instance CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:CloudResource {uid: row.target_uid})") {
		t.Fatalf("cypher must MATCH the target instance-profile CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (source)-[rel:USES_PROFILE]->(target)") {
		t.Fatalf("edge MERGE must use the static USES_PROFILE relationship type:\n%s", cypher)
	}
	for _, want := range []string{
		"rel.resolution_mode = row.resolution_mode",
		"rel.scope_id = row.scope_id",
		"rel.generation_id = row.generation_id",
		"rel.evidence_source = row.evidence_source",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	// The MERGE identity must be the static (source, USES_PROFILE, target) only —
	// no property-keyed relationship MERGE that would hit the NornicDB 20s
	// relationship-property MERGE timeout (#805 §5.3).
	if strings.Contains(cypher, "MERGE (source)-[rel:USES_PROFILE {") {
		t.Fatalf("MERGE must not carry a relationship-property map:\n%s", cypher)
	}
}

func TestEC2UsesProfileEdgeWriterRejectsForeignRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2UsesProfileEdgeWriter(executor, 0)

	rows := []map[string]any{{
		"source_uid":        "ec2-a",
		"target_uid":        "profile-a",
		"relationship_type": "USES_EVERYTHING",
		"resolution_mode":   "arn",
	}}
	if err := writer.WriteEC2UsesProfileEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/ec2-uses-profile"); err == nil {
		t.Fatal("WriteEC2UsesProfileEdges accepted an out-of-vocabulary relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestEC2UsesProfileEdgeWriterRejectsInjectionToken(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2UsesProfileEdgeWriter(executor, 0)

	rows := []map[string]any{{
		"source_uid":        "ec2-a",
		"target_uid":        "profile-a",
		"relationship_type": "USES_PROFILE]->() DELETE n //",
		"resolution_mode":   "arn",
	}}
	if err := writer.WriteEC2UsesProfileEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/ec2-uses-profile"); err == nil {
		t.Fatal("WriteEC2UsesProfileEdges accepted an injection-shaped relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestEC2UsesProfileEdgeWriterRetractScopedToEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2UsesProfileEdgeWriter(executor, 0)

	if err := writer.RetractEC2UsesProfileEdges(context.Background(), []string{"scope-1"}, "gen-1", "reducer/ec2-uses-profile"); err != nil {
		t.Fatalf("RetractEC2UsesProfileEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "[rel:USES_PROFILE]") {
		t.Fatalf("retract must match the USES_PROFILE relationship type:\n%s", cypher)
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

func TestEC2UsesProfileEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEC2UsesProfileEdgeWriter(executor, 0)

	if err := writer.RetractEC2UsesProfileEdges(context.Background(), nil, "gen-1", "reducer/ec2-uses-profile"); err != nil {
		t.Fatalf("RetractEC2UsesProfileEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}
