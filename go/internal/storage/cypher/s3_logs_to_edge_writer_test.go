// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// s3LogsToEdgeRows mirrors the rows ExtractS3LogsToEdgeRows produces: the source
// S3 bucket uid, the target log-bucket uid, the closed relationship type, and
// the resolution mode. It omits scope_id/generation_id/evidence_source — the
// writer injects those reducer-scoped annotations from its call arguments.
func s3LogsToEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        "src-" + string(rune('a'+i)),
			"target_uid":        "tgt-" + string(rune('a'+i)),
			"relationship_type": "LOGS_TO",
			"resolution_mode":   "name",
		})
	}
	return rows
}

func TestS3LogsToEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3LogsToEdgeWriter(executor, 0)

	if err := writer.WriteS3LogsToEdges(context.Background(), nil, "scope-1", "gen-1", "reducer/s3-logs-to"); err != nil {
		t.Fatalf("WriteS3LogsToEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestS3LogsToEdgeWriterUsesStaticTokenMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3LogsToEdgeWriter(executor, 0)

	if err := writer.WriteS3LogsToEdges(context.Background(), s3LogsToEdgeRows(1), "scope-1", "gen-1", "reducer/s3-logs-to"); err != nil {
		t.Fatalf("WriteS3LogsToEdges returned error: %v", err)
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
		t.Fatalf("cypher must MATCH the source S3 bucket CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:CloudResource {uid: row.target_uid})") {
		t.Fatalf("cypher must MATCH the target log-bucket CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (source)-[rel:LOGS_TO]->(target)") {
		t.Fatalf("edge MERGE must use the static LOGS_TO relationship type:\n%s", cypher)
	}
	// resolution_mode is a queryable edge property; scope_id/generation_id/
	// evidence_source are stamped so the prior-generation retract can scope to
	// reducer-owned edges. None of these may appear in the MERGE map.
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
	// The MERGE identity must be the static (source, LOGS_TO, target) only —
	// no property-keyed relationship MERGE that would hit the NornicDB 20s
	// relationship-property MERGE timeout (#805 §5.3).
	if strings.Contains(cypher, "MERGE (source)-[rel:LOGS_TO {") {
		t.Fatalf("MERGE must not carry a relationship-property map:\n%s", cypher)
	}
}

func TestS3LogsToEdgeWriterRejectsForeignRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3LogsToEdgeWriter(executor, 0)

	rows := []map[string]any{{
		"source_uid":        "src-a",
		"target_uid":        "tgt-a",
		"relationship_type": "LOGS_TO_EVERYTHING",
		"resolution_mode":   "name",
	}}
	if err := writer.WriteS3LogsToEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/s3-logs-to"); err == nil {
		t.Fatal("WriteS3LogsToEdges accepted an out-of-vocabulary relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestS3LogsToEdgeWriterRejectsInjectionToken(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3LogsToEdgeWriter(executor, 0)

	rows := []map[string]any{{
		"source_uid":        "src-a",
		"target_uid":        "tgt-a",
		"relationship_type": "LOGS_TO]->() DELETE n //",
		"resolution_mode":   "name",
	}}
	if err := writer.WriteS3LogsToEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/s3-logs-to"); err == nil {
		t.Fatal("WriteS3LogsToEdges accepted an injection-shaped relationship_type")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when validation rejects the row", len(executor.calls))
	}
}

func TestS3LogsToEdgeWriterRetractScopedToEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3LogsToEdgeWriter(executor, 0)

	if err := writer.RetractS3LogsToEdges(context.Background(), []string{"scope-1"}, "gen-1", "reducer/s3-logs-to"); err != nil {
		t.Fatalf("RetractS3LogsToEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "[rel:LOGS_TO]") {
		t.Fatalf("retract must match the LOGS_TO relationship type:\n%s", cypher)
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

func TestS3LogsToEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewS3LogsToEdgeWriter(executor, 0)

	if err := writer.RetractS3LogsToEdges(context.Background(), nil, "gen-1", "reducer/s3-logs-to"); err != nil {
		t.Fatalf("RetractS3LogsToEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}
