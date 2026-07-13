// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
)

// TestWorkloadCloudRelationshipWriterRetractRoutesThroughAutocommitExecute
// proves RetractWorkloadCloudRelationshipEdges dispatches its DELETE through
// Execute (autocommit), never through ExecuteGroup, on a GroupExecutor-capable
// executor. Issue #5152: dispatch() (used by both the write and retract
// paths) groups whenever the executor supports it, so the single USES retract
// statement was being sent through a managed ExecuteWrite transaction — the
// same shape measured to under-apply on NornicDB v1.1.11 for TAINT_FLOWS_TO,
// the SQL-relationship retract, and the repo-dependency retract
// (#4367/#5128/#5146). dispatchRetract fixes this the same way
// CodeInterprocEvidenceWriter does: sequential Execute, never ExecuteGroup.
func TestWorkloadCloudRelationshipWriterRetractRoutesThroughAutocommitExecute(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewWorkloadCloudRelationshipWriter(rec, 0)

	if err := w.RetractWorkloadCloudRelationshipEdges(context.Background(), []string{"scope-1"}, "gen-1", "reducer/workload-cloud-relationship"); err != nil {
		t.Fatalf("RetractWorkloadCloudRelationshipEdges() error = %v, want nil", err)
	}
	if len(rec.executeCyphers) != 1 {
		t.Fatalf("Execute calls = %d, want 1 (autocommit route)", len(rec.executeCyphers))
	}
	if len(rec.groupCyphers) != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0; USES DELETE must not use ExecuteGroup (NornicDB v1.1.11 under-applies) — see #5152", len(rec.groupCyphers))
	}
}

// TestWorkloadCloudRelationshipWriterWriteRoutesThroughExecuteGroup proves the
// write path still uses ExecuteGroup (the batched MERGE route) so the retract
// fix did not accidentally downgrade writes to per-statement autocommit.
func TestWorkloadCloudRelationshipWriterWriteRoutesThroughExecuteGroup(t *testing.T) {
	t.Parallel()

	rec := &dispatchRouteRecorder{}
	w := NewWorkloadCloudRelationshipWriter(rec, 0)

	rows := []map[string]any{{
		"workload_id":        "workload:orders-api",
		"cloud_resource_uid": "cloud-resource:ssm-config",
		"relationship_type":  "USES",
		"resolution_mode":    "explicit_workload_anchor",
		"environment":        "prod",
		"relationship_basis": "aws_resource_service_anchor",
		"source_fact_id":     "fact-1",
		"stable_fact_key":    "aws:resource:1",
		"source_system":      "aws",
		"source_record_id":   "arn:aws:ssm:example:parameter/config/orders-api/database-url",
		"collector_kind":     "aws_cloud",
	}}
	if err := w.WriteWorkloadCloudRelationshipEdges(context.Background(), rows, "scope-1", "gen-1", "reducer/workload-cloud-relationship"); err != nil {
		t.Fatalf("WriteWorkloadCloudRelationshipEdges() error = %v, want nil", err)
	}
	if len(rec.groupCyphers) == 0 {
		t.Fatal("write ExecuteGroup calls = 0, want >=1; the MERGE write path must use ExecuteGroup")
	}
	if len(rec.executeCyphers) != 0 {
		t.Fatalf("write Execute calls = %d, want 0; the write path must batch through ExecuteGroup", len(rec.executeCyphers))
	}
}
