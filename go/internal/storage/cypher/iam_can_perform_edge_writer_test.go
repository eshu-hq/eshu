package cypher

import (
	"context"
	"strings"
	"testing"
)

const iamCanPerformEvidence = "reducer/iam-can-perform"

func iamCanPerformEdgeRows() []map[string]any {
	return []map[string]any{
		{
			"principal_uid":    "principal-a",
			"resource_uid":     "bucket-a",
			"actions":          []string{"s3:getobject"},
			"action_count":     1,
			"evaluation_scope": "identity_policy_only",
			"grant_sources":    []string{"identity_policy"},
		},
		{
			"principal_uid":    "principal-a",
			"resource_uid":     "key-b",
			"actions":          []string{"kms:decrypt"},
			"action_count":     1,
			"evaluation_scope": "identity_policy_only",
			"grant_sources":    []string{"identity_policy"},
		},
	}
}

// TestIAMCanPerformEdgeWriterEmptyRowsIsNoOp proves an empty generation issues no
// statements.
func TestIAMCanPerformEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIAMCanPerformEdgeWriter(executor, 0)
	if err := writer.WriteIAMCanPerformEdges(context.Background(), nil, "scope-1", "gen-1", iamCanPerformEvidence); err != nil {
		t.Fatalf("WriteIAMCanPerformEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

// TestIAMCanPerformEdgeWriterStaticTypeAndDualMatch proves the writer MERGEs on the
// static CAN_PERFORM type over two uid-indexed CloudResource anchors and never
// fabricates a node. This pins the perf-critical static-token + dual-MATCH contract.
func TestIAMCanPerformEdgeWriterStaticTypeAndDualMatch(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMCanPerformEdgeWriter(executor, 500)
	if err := writer.WriteIAMCanPerformEdges(context.Background(), iamCanPerformEdgeRows(), "scope-1", "gen-1", iamCanPerformEvidence); err != nil {
		t.Fatalf("WriteIAMCanPerformEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 || len(executor.groupCalls[0]) != 1 {
		t.Fatalf("expected one atomic group of one batched statement, got %v", executor.groupCalls)
	}
	cypher := executor.groupCalls[0][0].Cypher
	if !strings.Contains(cypher, "MATCH (p:CloudResource {uid: row.principal_uid})") {
		t.Fatalf("must MATCH the principal CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (r:CloudResource {uid: row.resource_uid})") {
		t.Fatalf("must MATCH the resource CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (p)-[rel:CAN_PERFORM]->(r)") {
		t.Fatalf("must MERGE on the static CAN_PERFORM type:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (p:CloudResource") || strings.Contains(cypher, "MERGE (r:CloudResource") {
		t.Fatalf("must not fabricate endpoint nodes:\n%s", cypher)
	}
}

// TestIAMCanPerformEdgeWriterKeepsActionsOutOfMergeKey proves the granted action
// set is a SET property, NOT part of the MERGE identity — the keying decision that
// keeps the MERGE static-token (design §4, the NornicDB property-keyed-rel trap).
// It also pins the evaluation_scope honesty label and the scoped-retract field.
func TestIAMCanPerformEdgeWriterKeepsActionsOutOfMergeKey(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMCanPerformEdgeWriter(executor, 500)
	if err := writer.WriteIAMCanPerformEdges(context.Background(), iamCanPerformEdgeRows(), "scope-1", "gen-1", iamCanPerformEvidence); err != nil {
		t.Fatalf("WriteIAMCanPerformEdges returned error: %v", err)
	}
	cypher := executor.groupCalls[0][0].Cypher
	mergeLine, _, _ := strings.Cut(cypher[strings.Index(cypher, "MERGE"):], "\n") //nolint:gocritic // offBy1: the test fixture builds Cypher with at least one MERGE clause, so Index() != -1.
	if strings.Contains(mergeLine, "action") {
		t.Fatalf("actions must NOT be in the MERGE key (property-keyed MERGE perf trap):\n%s", mergeLine)
	}
	if strings.Contains(mergeLine, "grant_source") {
		t.Fatalf("grant_sources must NOT be in the MERGE key:\n%s", mergeLine)
	}
	if !strings.Contains(cypher, "SET rel.actions = row.actions") {
		t.Fatalf("actions must be a SET list property:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.action_count = row.action_count") {
		t.Fatalf("action_count must be set:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evaluation_scope = row.evaluation_scope") {
		t.Fatalf("evaluation_scope honesty label must be set on the edge:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.grant_sources = row.grant_sources") {
		t.Fatalf("grant_sources must be a SET list property:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = row.evidence_source") {
		t.Fatalf("evidence_source must be stamped on the edge for scoped retract:\n%s", cypher)
	}
}

// TestIAMCanPerformEdgeWriterStampsScopeFields proves the writer injects
// scope_id/generation_id/evidence_source onto every row (the retract filter
// depends on rel.scope_id, not the endpoint nodes).
func TestIAMCanPerformEdgeWriterStampsScopeFields(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMCanPerformEdgeWriter(executor, 500)
	if err := writer.WriteIAMCanPerformEdges(context.Background(), iamCanPerformEdgeRows(), "scope-7", "gen-9", iamCanPerformEvidence); err != nil {
		t.Fatalf("WriteIAMCanPerformEdges returned error: %v", err)
	}
	rows := executor.groupCalls[0][0].Parameters["rows"].([]map[string]any)
	for _, row := range rows {
		if row["scope_id"] != "scope-7" || row["generation_id"] != "gen-9" || row["evidence_source"] != iamCanPerformEvidence {
			t.Fatalf("row missing stamped scope fields: %v", row)
		}
		if row["evaluation_scope"] != "identity_policy_only" {
			t.Fatalf("row missing identity_policy_only honesty label: %v", row)
		}
		if got := row["grant_sources"].([]string); len(got) != 1 || got[0] != "identity_policy" {
			t.Fatalf("row missing grant_sources: %v", row)
		}
	}
}

// TestIAMCanPerformEdgeWriterIdempotentInputDoesNotMutateCaller proves the writer
// clones rows (never mutates the extractor's resolved row maps), so a retry that
// re-passes the same slice is safe.
func TestIAMCanPerformEdgeWriterIdempotentInputDoesNotMutateCaller(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMCanPerformEdgeWriter(executor, 500)
	rows := iamCanPerformEdgeRows()
	if err := writer.WriteIAMCanPerformEdges(context.Background(), rows, "scope-1", "gen-1", iamCanPerformEvidence); err != nil {
		t.Fatalf("WriteIAMCanPerformEdges returned error: %v", err)
	}
	for _, row := range rows {
		if _, ok := row["scope_id"]; ok {
			t.Fatalf("writer mutated caller row with scope_id: %v", row)
		}
	}
}

// TestIAMCanPerformEdgeRetractScopesByEvidenceSource proves the retract deletes
// only this reducer's edges, scoped by scope_id + evidence_source, and never
// touches endpoint nodes.
func TestIAMCanPerformEdgeRetractScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMCanPerformEdgeWriter(executor, 500)
	if err := writer.RetractIAMCanPerformEdges(context.Background(), []string{"scope-1"}, "gen-1", iamCanPerformEvidence); err != nil {
		t.Fatalf("RetractIAMCanPerformEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 || len(executor.groupCalls[0]) != 1 {
		t.Fatalf("expected one retract statement, got %v", executor.groupCalls)
	}
	stmt := executor.groupCalls[0][0]
	if !strings.Contains(stmt.Cypher, "[rel:CAN_PERFORM]") {
		t.Fatalf("retract must target the CAN_PERFORM type:\n%s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.scope_id IN $scope_ids") || !strings.Contains(stmt.Cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must scope by edge scope_id AND evidence_source:\n%s", stmt.Cypher)
	}
	// Guard against deleting either endpoint node. "DELETE r" alone would collide
	// with the legitimate "DELETE rel", so match the node-delete forms precisely.
	if strings.Contains(stmt.Cypher, "DELETE p\n") || strings.Contains(stmt.Cypher, "DELETE p ") ||
		strings.Contains(stmt.Cypher, "DELETE r\n") || strings.Contains(stmt.Cypher, "DELETE r ") {
		t.Fatalf("retract must not delete endpoint nodes:\n%s", stmt.Cypher)
	}
	if !strings.HasSuffix(strings.TrimSpace(stmt.Cypher), "DELETE rel") {
		t.Fatalf("retract must delete only the relationship:\n%s", stmt.Cypher)
	}
}

// TestIAMCanPerformEdgeRetractEmptyScopesIsNoOp proves an empty scope set issues no
// statement.
func TestIAMCanPerformEdgeRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIAMCanPerformEdgeWriter(executor, 500)
	if err := writer.RetractIAMCanPerformEdges(context.Background(), nil, "gen-1", iamCanPerformEvidence); err != nil {
		t.Fatalf("RetractIAMCanPerformEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 0 {
		t.Fatalf("empty scope set must be a no-op, got %v", executor.groupCalls)
	}
}
