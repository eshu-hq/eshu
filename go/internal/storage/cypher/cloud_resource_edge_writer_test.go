package cypher

import (
	"context"
	"strings"
	"testing"
)

func cloudResourceEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        "src-" + string(rune('a'+i)),
			"target_uid":        "tgt-" + string(rune('a'+i)),
			"relationship_type": "USES_KMS_KEY",
			"target_type":       "aws_kms_key",
			"resolution_mode":   "arn",
			"scope_id":          "scope-1",
			"generation_id":     "gen-1",
		})
	}
	return rows
}

func TestCloudResourceEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), nil, "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestCloudResourceEdgeWriterUsesMatchMatchMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), cloudResourceEdgeRows(1), "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
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
	if !strings.Contains(cypher, "MATCH (source:CloudResource {uid: row.source_uid})") {
		t.Fatalf("cypher must MATCH the source CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:CloudResource {uid: row.target_uid})") {
		t.Fatalf("cypher must MATCH the target CloudResource by uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (target:CloudResource") || strings.Contains(cypher, "MERGE (source:CloudResource") {
		t.Fatalf("cypher must not MERGE (fabricate) endpoint nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (source)-[rel:AWS_RELATIONSHIP {relationship_type: row.relationship_type}]->(target)") {
		t.Fatalf("edge MERGE identity must be (source, relationship_type, target):\n%s", cypher)
	}
}

func TestCloudResourceEdgeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 2)

	if err := writer.WriteCloudResourceEdges(context.Background(), cloudResourceEdgeRows(5), "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	// 5 rows at batch size 2 -> 3 statements.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestCloudResourceEdgeWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 2)

	if err := writer.WriteCloudResourceEdges(context.Background(), cloudResourceEdgeRows(5), "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 3 {
		t.Fatalf("group statement count = %d, want 3", len(executor.groupCalls[0]))
	}
}

func TestCloudResourceEdgeWriterAnnotatesEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), cloudResourceEdgeRows(1), "reducer/aws-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	if got := rows[0]["evidence_source"]; got != "reducer/aws-relationships" {
		t.Fatalf("evidence_source = %v, want reducer/aws-relationships", got)
	}
	if !strings.Contains(executor.calls[0].Cypher, "rel.evidence_source = row.evidence_source") {
		t.Fatalf("cypher must persist evidence_source for retract scoping:\n%s", executor.calls[0].Cypher)
	}
}

func TestCloudResourceEdgeWriterRetractScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.RetractCloudResourceEdges(
		context.Background(),
		[]string{"scope-1"},
		"gen-1",
		"reducer/aws-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1 retract statement", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "[rel:AWS_RELATIONSHIP]") {
		t.Fatalf("retract must target AWS_RELATIONSHIP edges:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must be scoped to this reducer's evidence_source:\n%s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("retract must DELETE only the edge, never a node:\n%s", cypher)
	}
	if strings.Contains(cypher, "DETACH DELETE") || strings.Contains(cypher, "DELETE source") || strings.Contains(cypher, "DELETE target") {
		t.Fatalf("retract must not delete endpoint nodes:\n%s", cypher)
	}
}

func TestCloudResourceEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCloudResourceEdgeWriter(executor, 0)

	if err := writer.RetractCloudResourceEdges(context.Background(), nil, "gen-1", "reducer/aws-relationships"); err != nil {
		t.Fatalf("RetractCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}

func TestCloudResourceEdgeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface shape used by the relationship materialization handler.
	var _ interface {
		WriteCloudResourceEdges(ctx context.Context, rows []map[string]any, evidenceSource string) error
		RetractCloudResourceEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
	} = NewCloudResourceEdgeWriter(&recordingExecutor{}, 0)
}
