// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func azureCloudResourceEdgeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"source_uid":        "src-" + string(rune('a'+i)),
			"target_uid":        "tgt-" + string(rune('a'+i)),
			"relationship_type": "managed_by",
			"target_type":       "microsoft.network/networkinterfaces",
			"support_state":     "supported",
			"resolution_mode":   "arm_resource_id",
		})
	}
	return rows
}

func TestAzureCloudResourceEdgeWriterUsesNoFabricationMergeShape(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewAzureCloudResourceEdgeWriter(executor, 0)

	if err := writer.WriteCloudResourceEdges(context.Background(), azureCloudResourceEdgeRows(1), "scope-1", "gen-1", "reducer/azure-relationships"); err != nil {
		t.Fatalf("WriteCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (source:CloudResource {uid: row.source_uid})") {
		t.Fatalf("cypher must MATCH source endpoint:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:CloudResource {uid: row.target_uid})") {
		t.Fatalf("cypher must MATCH target endpoint:\n%s", cypher)
	}
	if strings.Contains(cypher, "MERGE (source:CloudResource") || strings.Contains(cypher, "MERGE (target:CloudResource") {
		t.Fatalf("cypher must not fabricate endpoint nodes:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (source)-[rel:AZURE_managed_by]->(target)") {
		t.Fatalf("edge MERGE must use AZURE-prefixed static relationship type:\n%s", cypher)
	}
}

func TestAzureCloudResourceEdgeWriterRejectsUnsafeRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewAzureCloudResourceEdgeWriter(executor, 0)
	rows := azureCloudResourceEdgeRows(1)
	rows[0]["relationship_type"] = "bad type`) DELETE n //"

	err := writer.WriteCloudResourceEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/azure-relationships")
	if err == nil {
		t.Fatal("WriteCloudResourceEdges returned nil, want unsafe relationship_type error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when relationship_type is unsafe", len(executor.calls))
	}
}

func TestAzureCloudResourceEdgeWriterRejectsUnsupportedRelationshipType(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewAzureCloudResourceEdgeWriter(executor, 0)
	rows := azureCloudResourceEdgeRows(1)
	rows[0]["relationship_type"] = "depends_on"

	err := writer.WriteCloudResourceEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/azure-relationships")
	if err == nil {
		t.Fatal("WriteCloudResourceEdges returned nil, want unsupported relationship_type error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 when relationship_type is unsupported", len(executor.calls))
	}
}

func TestAzureCloudResourceEdgeWriterRetractScopesByEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewAzureCloudResourceEdgeWriter(executor, 0)

	if err := writer.RetractCloudResourceEdges(context.Background(), []string{"scope-1"}, "gen-1", "reducer/azure-relationships"); err != nil {
		t.Fatalf("RetractCloudResourceEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (source:CloudResource)-[rel]->(:CloudResource)") {
		t.Fatalf("retract must target only CloudResource edges:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.scope_id IN $scope_ids") {
		t.Fatalf("retract must filter by edge scope_id:\n%s", cypher)
	}
	if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("retract must filter by evidence_source:\n%s", cypher)
	}
	if strings.Contains(cypher, "DETACH DELETE") || strings.Contains(cypher, "DELETE source") || strings.Contains(cypher, "DELETE target") {
		t.Fatalf("retract must delete only relationships:\n%s", cypher)
	}
}

// TestAzureCloudResourceEdgeWriterRetractByUIDsAnchoredDelete proves the
// anchored retract enumerates $source_uids via a single-clause
// `WHERE source.uid IN $source_uids` predicate that seeds the
// CloudResource.uid index, instead of scanning the whole :CloudResource label
// or splitting into a two-clause MATCH/MATCH.
func TestAzureCloudResourceEdgeWriterRetractByUIDsAnchoredDelete(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewAzureCloudResourceEdgeWriter(executor, 0)
	if err := writer.RetractCloudResourceEdgesByUIDs(
		context.Background(),
		[]string{"src-a"},
		[]string{"scope-1"},
		"reducer/azure-relationships",
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

// TestAzureCloudResourceEdgeWriterRetractByUIDsEmptyIsNoOp proves empty source
// uids is a clean no-op.
func TestAzureCloudResourceEdgeWriterRetractByUIDsEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewAzureCloudResourceEdgeWriter(executor, 0)
	if err := writer.RetractCloudResourceEdgesByUIDs(
		context.Background(), nil, []string{"scope-1"}, "reducer/azure-relationships",
	); err != nil {
		t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty uids", len(executor.calls))
	}
}
