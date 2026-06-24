// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func TestWorkloadCloudRelationshipWriterUsesExistingEndpoints(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewWorkloadCloudRelationshipWriter(executor, 0)

	rows := []map[string]any{{
		"workload_id":           "workload:orders-api",
		"cloud_resource_uid":    "cloud-resource:ssm-config",
		"relationship_type":     "USES",
		"resolution_mode":       "explicit_workload_anchor",
		"environment":           "prod",
		"relationship_basis":    "aws_resource_service_anchor",
		"service_anchor_source": "payload.workload_id",
		"service_anchor_reason": "explicit_workload_anchor",
		"source_fact_id":        "fact-1",
		"stable_fact_key":       "aws:resource:1",
		"source_system":         "aws",
		"source_record_id":      "arn:aws:ssm:example:parameter/config/orders-api/database-url",
		"collector_kind":        "aws_cloud",
	}}
	err := writer.WriteWorkloadCloudRelationshipEdges(
		context.Background(),
		rows,
		"scope-1",
		"gen-1",
		"reducer/workload-cloud-relationship",
	)
	if err != nil {
		t.Fatalf("WriteWorkloadCloudRelationshipEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}

	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MATCH (resource:CloudResource {uid: row.cloud_resource_uid})",
		"MATCH (workload:Workload {id: row.workload_id})<-[:INSTANCE_OF]-(instance:WorkloadInstance)",
		"WHERE instance.environment = row.environment",
		"MERGE (instance)-[rel:USES]->(resource)",
		"rel.scope_id = row.scope_id",
		"rel.generation_id = row.generation_id",
		"rel.evidence_source = row.evidence_source",
		"rel.environment = row.environment",
		"rel.relationship_basis = row.relationship_basis",
		"rel.service_anchor_source = row.service_anchor_source",
		"rel.service_anchor_reason = row.service_anchor_reason",
		"rel.source_fact_id = row.source_fact_id",
		"rel.stable_fact_key = row.stable_fact_key",
		"rel.source_system = row.source_system",
		"rel.source_record_id = row.source_record_id",
		"rel.collector_kind = row.collector_kind",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "MERGE (instance:WorkloadInstance") ||
		strings.Contains(cypher, "MERGE (resource:CloudResource") {
		t.Fatalf("writer must not create endpoint nodes:\n%s", cypher)
	}
}

func TestWorkloadCloudRelationshipWriterRetractScopedToEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewWorkloadCloudRelationshipWriter(executor, 0)

	err := writer.RetractWorkloadCloudRelationshipEdges(
		context.Background(),
		[]string{"scope-1"},
		"gen-1",
		"reducer/workload-cloud-relationship",
	)
	if err != nil {
		t.Fatalf("RetractWorkloadCloudRelationshipEdges returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (:WorkloadInstance)-[rel:USES]->(:CloudResource)",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract cypher missing %q:\n%s", want, cypher)
		}
	}
}
