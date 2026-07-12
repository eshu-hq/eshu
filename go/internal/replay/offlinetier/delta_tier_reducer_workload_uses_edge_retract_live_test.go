// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized workload/cloud usage edge retract coverage (C-14 #4367
// retract axis): USES (WorkloadInstance -> CloudResource) and USES_PROFILE
// (EC2 instance CloudResource -> IAM instance-profile CloudResource).
//
// Issue #5152: both WorkloadCloudRelationshipWriter.RetractWorkloadCloudRelationshipEdges
// and EC2UsesProfileEdgeWriter.RetractEC2UsesProfileEdges dispatched their
// single retract DELETE through dispatch(), the same code path the writer's
// MERGE-shaped write uses. dispatch() groups whenever the executor implements
// GroupExecutor, so the retract statement — a single UNWIND-free MATCH...DELETE
// — ran inside a managed ExecuteWrite transaction. On the pinned NornicDB
// v1.1.11 a DELETE inside a managed transaction can under-apply even as a
// single statement (measured for TAINT_FLOWS_TO in
// CodeInterprocEvidenceWriter and for the SQL-relationship and
// repo-dependency retracts, #4367/#5128/#5146). Fixed by adding a
// dispatchRetract method to each writer (sequential Execute, never
// ExecuteGroup) and routing the retract through it, mirroring
// CodeInterprocEvidenceWriter.dispatchRetract. The unit regressions are
// TestWorkloadCloudRelationshipWriterRetractRoutesThroughAutocommitExecute
// and TestEC2UsesProfileEdgeWriterRetractRoutesThroughAutocommitExecute in
// go/internal/storage/cypher; this is the live NornicDB proof.
//
// The test drives the REAL production writer methods
// (WriteWorkloadCloudRelationshipEdges/RetractWorkloadCloudRelationshipEdges
// and WriteEC2UsesProfileEdges/RetractEC2UsesProfileEdges) directly against
// exec (liveExecutor), which implements GroupExecutor exactly like the
// production NornicDB driver adapter — the same executor shape that exposed
// the grouped-retract defect.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	wuScopeIn  = "replay-workload-uses:in"
	wuScopeOut = "replay-workload-uses:out"
	wuMarker   = "replay-workload-uses"

	wuEc2In      = "workload-uses:ec2:in"
	wuProfileIn  = "workload-uses:profile:in"
	wuEc2Out     = "workload-uses:ec2:out"
	wuProfileOut = "workload-uses:profile:out"

	wuCloudResIn  = "workload-uses:cloudres:in"
	wuCloudResOut = "workload-uses:cloudres:out"
	wuWorkloadIn  = "workload-uses:workload:in"
	wuWorkloadOut = "workload-uses:workload:out"
	wuInstanceIn  = "workload-uses:instance:in"
	wuInstanceOut = "workload-uses:instance:out"

	wuEvidenceUses        = "reducer/workload-cloud-relationship"
	wuEvidenceUsesProfile = "reducer/ec2-uses-profile"
	wuEnvironment         = "prod"
)

// TestReducerWorkloadUsesEdgeRetractGraphTruth proves the USES and
// USES_PROFILE retract paths delete only the in-scope edges on a real
// NornicDB, through the production WorkloadCloudRelationshipWriter and
// EC2UsesProfileEdgeWriter. It is the failing-then-green regression for the
// #5152 grouped-retract defect in both writers' dispatch().
func TestReducerWorkloadUsesEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the workload/uses edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupWorkloadUsesScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupWorkloadUsesScope(cleanCtx, t, exec)
	})

	seedWorkloadUsesNodes(ctx, t, exec)

	profileWriter := cypher.NewEC2UsesProfileEdgeWriter(exec, 0)
	usesWriter := cypher.NewWorkloadCloudRelationshipWriter(exec, 0)

	if err := profileWriter.WriteEC2UsesProfileEdges(ctx, []map[string]any{
		{"source_uid": wuEc2In, "target_uid": wuProfileIn, "relationship_type": "USES_PROFILE", "resolution_mode": "arn"},
	}, wuScopeIn, "gen-1", wuEvidenceUsesProfile); err != nil {
		t.Fatalf("WriteEC2UsesProfileEdges(in): %v", err)
	}
	if err := profileWriter.WriteEC2UsesProfileEdges(ctx, []map[string]any{
		{"source_uid": wuEc2Out, "target_uid": wuProfileOut, "relationship_type": "USES_PROFILE", "resolution_mode": "arn"},
	}, wuScopeOut, "gen-1", wuEvidenceUsesProfile); err != nil {
		t.Fatalf("WriteEC2UsesProfileEdges(out): %v", err)
	}

	usesRow := func(cloudResUID, workloadID string) map[string]any {
		return map[string]any{
			"workload_id":        workloadID,
			"cloud_resource_uid": cloudResUID,
			"relationship_type":  "USES",
			"resolution_mode":    "explicit_workload_anchor",
			"environment":        wuEnvironment,
			"relationship_basis": "aws_resource_service_anchor",
			"source_fact_id":     "fact-" + cloudResUID,
			"stable_fact_key":    "aws:resource:" + cloudResUID,
			"source_system":      "aws",
			"source_record_id":   "arn:aws:ssm:example:" + cloudResUID,
			"collector_kind":     "aws_cloud",
		}
	}
	if err := usesWriter.WriteWorkloadCloudRelationshipEdges(ctx, []map[string]any{usesRow(wuCloudResIn, wuWorkloadIn)}, wuScopeIn, "gen-1", wuEvidenceUses); err != nil {
		t.Fatalf("WriteWorkloadCloudRelationshipEdges(in): %v", err)
	}
	if err := usesWriter.WriteWorkloadCloudRelationshipEdges(ctx, []map[string]any{usesRow(wuCloudResOut, wuWorkloadOut)}, wuScopeOut, "gen-1", wuEvidenceUses); err != nil {
		t.Fatalf("WriteWorkloadCloudRelationshipEdges(out): %v", err)
	}

	profileQ := "MATCH (:CloudResource {uid: $s})-[r:USES_PROFILE]->(:CloudResource {uid: $t}) RETURN count(r)"
	usesQ := "MATCH (:WorkloadInstance {uid: $s})-[r:USES]->(:CloudResource {uid: $t}) RETURN count(r)"

	assertEdgeCount(ctx, t, exec, profileQ, map[string]any{"s": wuEc2In, "t": wuProfileIn}, 1, "write: in-scope USES_PROFILE present")
	assertEdgeCount(ctx, t, exec, profileQ, map[string]any{"s": wuEc2Out, "t": wuProfileOut}, 1, "write: out-of-scope USES_PROFILE present")
	assertEdgeCount(ctx, t, exec, usesQ, map[string]any{"s": wuInstanceIn, "t": wuCloudResIn}, 1, "write: in-scope USES present")
	assertEdgeCount(ctx, t, exec, usesQ, map[string]any{"s": wuInstanceOut, "t": wuCloudResOut}, 1, "write: out-of-scope USES present")

	if err := profileWriter.RetractEC2UsesProfileEdges(ctx, []string{wuScopeIn}, "gen-2", wuEvidenceUsesProfile); err != nil {
		t.Fatalf("RetractEC2UsesProfileEdges: %v", err)
	}
	if err := usesWriter.RetractWorkloadCloudRelationshipEdges(ctx, []string{wuScopeIn}, "gen-2", wuEvidenceUses); err != nil {
		t.Fatalf("RetractWorkloadCloudRelationshipEdges: %v", err)
	}

	assertEdgeCount(ctx, t, exec, profileQ, map[string]any{"s": wuEc2In, "t": wuProfileIn}, 0, "retract: in-scope USES_PROFILE gone")
	assertEdgeCount(ctx, t, exec, usesQ, map[string]any{"s": wuInstanceIn, "t": wuCloudResIn}, 0, "retract: in-scope USES gone")
	// Scoped retracts, not wipes.
	assertEdgeCount(ctx, t, exec, profileQ, map[string]any{"s": wuEc2Out, "t": wuProfileOut}, 1, "retract: out-of-scope USES_PROFILE survives")
	assertEdgeCount(ctx, t, exec, usesQ, map[string]any{"s": wuInstanceOut, "t": wuCloudResOut}, 1, "retract: out-of-scope USES survives")
	// Endpoint nodes survive; only the relationships were retracted.
	for _, uid := range []string{wuEc2In, wuProfileIn, wuCloudResIn} {
		assertEdgeCount(ctx, t, exec, "MATCH (n:CloudResource {uid: $u}) RETURN count(n)", map[string]any{"u": uid}, 1, "node survives: "+uid)
	}
	assertEdgeCount(ctx, t, exec, "MATCH (n:WorkloadInstance {uid: $u}) RETURN count(n)", map[string]any{"u": wuInstanceIn}, 1, "node survives: "+wuInstanceIn)
	assertEdgeCount(ctx, t, exec, "MATCH (n:Workload {id: $u}) RETURN count(n)", map[string]any{"u": wuWorkloadIn}, 1, "node survives: "+wuWorkloadIn)
}

// seedWorkloadUsesNodes creates the CloudResource, Workload, and
// WorkloadInstance nodes the write templates MATCH.
func seedWorkloadUsesNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE (:CloudResource {uid: $ec2In, marker: $marker}),
       (:CloudResource {uid: $profileIn, marker: $marker}),
       (:CloudResource {uid: $ec2Out, marker: $marker}),
       (:CloudResource {uid: $profileOut, marker: $marker}),
       (:CloudResource {uid: $cloudResIn, marker: $marker}),
       (:CloudResource {uid: $cloudResOut, marker: $marker}),
       (wIn:Workload {id: $workloadIn, marker: $marker}),
       (wOut:Workload {id: $workloadOut, marker: $marker}),
       (:WorkloadInstance {uid: $instanceIn, environment: $env, marker: $marker})-[:INSTANCE_OF]->(wIn),
       (:WorkloadInstance {uid: $instanceOut, environment: $env, marker: $marker})-[:INSTANCE_OF]->(wOut)`,
		Parameters: map[string]any{
			"ec2In": wuEc2In, "profileIn": wuProfileIn,
			"ec2Out": wuEc2Out, "profileOut": wuProfileOut,
			"cloudResIn": wuCloudResIn, "cloudResOut": wuCloudResOut,
			"workloadIn": wuWorkloadIn, "workloadOut": wuWorkloadOut,
			"instanceIn": wuInstanceIn, "instanceOut": wuInstanceOut,
			"env": wuEnvironment, "marker": wuMarker,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed workload-uses nodes: %v", err)
	}
}

// cleanupWorkloadUsesScope removes every node this test creates.
func cleanupWorkloadUsesScope(ctx context.Context, t *testing.T, exec deltaCleanupExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
		Parameters: map[string]any{"marker": wuMarker},
	}); err != nil {
		t.Fatalf("cleanup workload-uses scope: %v", err)
	}
}
