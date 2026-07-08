// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// gcpCloudResourceEdgeRows mirrors the rows ExtractGCPRelationshipEdgeRows
// produces: endpoint uids, relationship/target type, support state, and
// resolution mode. It omits scope_id/generation_id/evidence_source — those are
// reducer-scoped annotations the writer injects from its call arguments.
func gcpCloudResourceEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        "src-" + string(rune('a'+i)),
			"target_uid":        "tgt-" + string(rune('a'+i)),
			"relationship_type": "INSTANCE_TO_DISK",
			"target_type":       "compute.googleapis.com/Disk",
			"support_state":     "supported",
			"resolution_mode":   "full_resource_name",
		})
	}
	return rows
}

func TestGCPCloudResourceEdgeWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), nil, "scope-1", "gen-1", "reducer/gcp-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

func TestGCPCloudResourceEdgeWriterUsesStaticRelationshipTypeMerge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), gcpCloudResourceEdgeRows(1), "scope-1", "gen-1", "reducer/gcp-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND batch shape:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (source:CloudResource {uid: row.source_uid})") {
		t.Fatalf("cypher must MATCH the source CloudResource by uid:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:CloudResource {uid: row.target_uid})") {
		t.Fatalf("cypher must MATCH the target CloudResource by uid:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (target:CloudResource") || strings.Contains(cypher, "MERGE (source:CloudResource") {
		t.Fatalf("cypher must not MERGE (fabricate) endpoint nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (source)-[rel:GCP_INSTANCE_TO_DISK]->(target)") {
		t.Fatalf("edge MERGE must use the sanitized GCP relationship type as the Cypher relationship type:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.relationship_type = row.relationship_type") {
		t.Fatalf("edge must keep the original relationship_type as a property for API/readback truth:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.support_state = row.support_state") {
		t.Fatalf("edge must persist the provider support_state for readback truth:\n%s", cypher)
	}
}

func TestGCPCloudResourceEdgeWriterPreservesRelationshipTypeCaseInIdentity(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 500)
	rows := []map[string]any{
		{
			"source_uid":        "shared-source",
			"target_uid":        "shared-target",
			"relationship_type": "uses_network",
			"target_type":       "compute.googleapis.com/Network",
			"support_state":     "supported",
			"resolution_mode":   "full_resource_name",
		},
		{
			"source_uid":        "shared-source",
			"target_uid":        "shared-target",
			"relationship_type": "USES_NETWORK",
			"target_type":       "compute.googleapis.com/Network",
			"support_state":     "supported",
			"resolution_mode":   "full_resource_name",
		},
	}

	if err := writer.WriteCloudResourceEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/gcp-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	stmts := executor.groupCalls[0]
	if len(stmts) != 2 {
		t.Fatalf("group statement count = %d, want case-distinct relationship types to stay distinct", len(stmts))
	}
	gotCypher := stmts[0].Cypher + "\n" + stmts[1].Cypher
	for _, want := range []string{
		"MERGE (source)-[rel:GCP_USES_NETWORK]->(target)",
		"MERGE (source)-[rel:GCP_uses_network]->(target)",
	} {
		if !strings.Contains(gotCypher, want) {
			t.Fatalf("missing case-preserving relationship MERGE %q in:\n%s", want, gotCypher)
		}
	}
}

func TestGCPCloudResourceEdgeWriterRejectsUnsafeRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 0)
	rows := gcpCloudResourceEdgeRows(1)
	rows[0]["relationship_type"] = "bad type`) DELETE n //"

	err := writer.WriteCloudResourceEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/gcp-relationships")
	if err == nil {
		t.Fatal("WriteCloudResourceEdges returned nil, want unsafe relationship_type error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when relationship_type is unsafe", len(executor.calls))
	}
}

func TestGCPCloudResourceEdgeWriterAnnotatesScopeGenerationEvidence(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), gcpCloudResourceEdgeRows(1), "scope-1", "gen-1", "reducer/gcp-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows parameter type = %T, want []map[string]any", executor.calls[0].Parameters["rows"])
	}
	if got := rows[0]["scope_id"]; got != "scope-1" {
		t.Fatalf("scope_id = %v, want scope-1 (injected by writer for scope-scoped retract)", got)
	}
	if got := rows[0]["generation_id"]; got != "gen-1" {
		t.Fatalf("generation_id = %v, want gen-1 (injected by writer)", got)
	}
	if got := rows[0]["evidence_source"]; got != "reducer/gcp-relationships" {
		t.Fatalf("evidence_source = %v, want reducer/gcp-relationships", got)
	}
}

func TestGCPCloudResourceEdgeWriterBatchesRows(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 2)

	if err := writer.WriteCloudResourceEdges(context.Background(), gcpCloudResourceEdgeRows(5), "scope-1", "gen-1", "reducer/gcp-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	// 5 rows at batch size 2 -> 3 statements.
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batched statements", len(executor.calls))
	}
}

func TestGCPCloudResourceEdgeWriterRetractScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 0)

	if err := writer.RetractCloudResourceEdges(
		context.Background(),
		[]string{"scope-1"},
		"gen-1",
		"reducer/gcp-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1 retract statement", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (source:CloudResource)-[rel]->(:CloudResource)") {
		t.Fatalf("retract must target all reducer-owned CloudResource relationships for the scope:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.scope_id IN $scope_ids") {
		t.Fatalf("retract must filter by the edge scope_id (rel.scope_id IN $scope_ids):\n%s", cypher)
	}
	if strings.Contains(cypher, "source.scope_id") {
		t.Fatalf("retract must not filter by node scope_id — CloudResource nodes carry none:\n%s", cypher)
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

func TestGCPCloudResourceEdgeWriterRetractEmptyScopesIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 0)

	if err := writer.RetractCloudResourceEdges(context.Background(), nil, "gen-1", "reducer/gcp-relationships"); err != nil {
		t.Fatalf("RetractCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scope set", len(executor.calls))
	}
}

func TestGCPCloudResourceEdgeWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	// Compile-time guarantee that the cypher writer satisfies the reducer-owned
	// consumer interface shape used by the GCP relationship materialization
	// handler (the same shape the AWS edge writer satisfies).
	var _ interface {
		WriteCloudResourceEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
		RetractCloudResourceEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
		RetractCloudResourceEdgesByUIDs(ctx context.Context, sourceUIDs []string, scopeIDs []string, evidenceSource string) error
	} = NewGCPCloudResourceEdgeWriter(&recordingExecutor{}, 0)
}

// TestGCPCloudResourceEdgeWriterRetractByUIDsAnchoredDelete proves the
// anchored retract enumerates $source_uids via a single-clause
// `WHERE source.uid IN $source_uids` predicate that seeds the
// CloudResource.uid index, instead of scanning the whole :CloudResource label
// or splitting into a two-clause MATCH/MATCH.
func TestGCPCloudResourceEdgeWriterRetractByUIDsAnchoredDelete(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 0)
	if err := writer.RetractCloudResourceEdgesByUIDs(
		context.Background(),
		[]string{"src-a"},
		[]string{"scope-1"},
		"reducer/gcp-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (source:CloudResource)-[rel]->()",
		"WHERE source.uid IN $source_uids",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract by uids cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "UNWIND $source_uids AS suid") || strings.Contains(cypher, "{uid: suid}") {
		t.Fatalf("retract by uids cypher must not use the slow UNWIND + property-map MATCH shape:\n%s", cypher)
	}
}

// TestGCPCloudResourceEdgeWriterRetractByUIDsEmptyIsNoOp proves empty source
// uids is a clean no-op.
func TestGCPCloudResourceEdgeWriterRetractByUIDsEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 0)
	if err := writer.RetractCloudResourceEdgesByUIDs(
		context.Background(), nil, []string{"scope-1"}, "reducer/gcp-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty uids", len(executor.calls))
	}
}

// TestGCPCloudResourceEdgeWriterRetractByUIDsBatchesUids proves uids beyond
// the batch size split into multiple statements.
func TestGCPCloudResourceEdgeWriterRetractByUIDsBatchesUids(t *testing.T) {
	t.Parallel()

	uids := make([]string, 1200)
	for i := range uids {
		uids[i] = "uid-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	}

	executor := &recordingExecutor{}
	writer := NewGCPCloudResourceEdgeWriter(executor, 0)
	if err := writer.RetractCloudResourceEdgesByUIDs(
		context.Background(), uids, []string{"scope-1"}, "reducer/gcp-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3 batches", len(executor.calls))
	}
}
