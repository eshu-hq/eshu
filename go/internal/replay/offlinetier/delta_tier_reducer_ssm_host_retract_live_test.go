// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// SSM host inventory retract coverage (eshu-hq/eshu#9558, C-14 #4367
// retract-depth backfill pattern): SSMHostApplication,
// SSMHostPlatform, and REPORTED_WITH.
//
// HostNodeWriter.RetractStaleApplicationNodes and RetractPlatformNodes both
// DETACH DELETE their nodes, so pairing edges die with their endpoint —
// there is deliberately no edge-only retract writer (the conflicted-platform
// path retracts the node with no replacement; the supersession read matches
// edges by endpoint uid, so a surviving edge without its node can never
// surface). This test proves that graph truth on a real NornicDB: platform
// retract removes the node AND its REPORTED_WITH edge while the paired
// application node survives, and stale app retract removes the aged node,
// all scoped (same evidence_source, different scope_id survives).
//
// The test drives the REAL production writer constructor and methods
// (ssm.NewHostNodeWriter, WriteHostObservationBatch, RetractPlatformNodes,
// RetractStaleApplicationNodes) against liveExecutor, which implements
// GroupExecutor exactly like production's reducerNeo4jExecutor — and doubles
// as the PostureExistenceReader (liveExecutor.Run), so the retract
// countStale reads run against the same live backend. A retract that routed
// through a broken dispatch path would reproduce here.
//
// Skills active: golang-engineering, cypher-query-rigor,
// eshu-golden-corpus-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	ssm "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/ssm"
)

const (
	ssmRetractEvidenceSource = "reducer/ssm-host-inventory"
	ssmRetractScopeIn        = "replay-ssm-host-retract:scope-in"
	ssmRetractScopeOut       = "replay-ssm-host-retract:scope-out"
	ssmRetractGenerationID   = "gen-1"

	ssmRetractAppIn       = "replay-ssm-app-in"
	ssmRetractPlatformIn  = "replay-ssm-platform-in"
	ssmRetractAppOut      = "replay-ssm-app-out"
	ssmRetractPlatformOut = "replay-ssm-platform-out"

	ssmRetractInstanceIn  = "i-retract-in"
	ssmRetractInstanceOut = "i-retract-out"

	ssmRetractCaptureTime = "2026-10-29T22:31:19Z"
)

// ssmRetractAppRow shapes one application observation row like the production
// HostNodeWriter row contract (capture_time RFC3339 input).
func ssmRetractAppRow(uid, instance string) map[string]any {
	return map[string]any{
		"uid": uid, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
		"instance_id": instance, "resource_type": "AWS::EC2::Instance", "source_type": "AWS:Application",
		"schema_version": "1.0.0", "capture_time": ssmRetractCaptureTime, "name": "alpha",
		"source_fact_id": "a-" + instance, "source_order_key": "k-" + instance,
	}
}

// ssmRetractPlatformRow shapes one platform observation row like the
// production HostNodeWriter row contract (observation_time RFC3339 input).
func ssmRetractPlatformRow(uid, instance string) map[string]any {
	return map[string]any{
		"uid": uid, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
		"instance_id": instance, "resource_type": "AWS::EC2::Instance", "source_type": "ssm_inventory",
		"platform_type": "Linux", "observation_time": "2026-10-29T22:33:02Z",
		"source_fact_id": "p-" + instance, "source_order_key": "k-" + instance,
	}
}

// ssmRetractEdgeRow shapes one REPORTED_WITH pairing row: endpoint uids plus
// the closed-vocabulary relationship the writer screens before interpolating.
func ssmRetractEdgeRow(appUID, platformUID string) map[string]any {
	return map[string]any{
		"from_uid": appUID, "to_uid": platformUID, "relationship": "REPORTED_WITH",
	}
}

// TestReducerSSMHostRetractGraphTruth proves the SSM host inventory retract
// paths delete exactly the in-scope tombstoned nodes and their incident
// REPORTED_WITH edges on a real NornicDB: platform retract first (the paired
// application node must survive its platform's DETACH DELETE), then stale
// application retract, with the out-of-scope host's full triple surviving
// both.
func TestReducerSSMHostRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the SSM host retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupSSMHostRetractScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupSSMHostRetractScope(cleanCtx, t, exec)
	})

	// liveExecutor doubles as the posture reader: countStale reads run
	// against the same live backend as the writes.
	writer := ssm.NewHostNodeWriter(exec, exec, 500)

	writeHost := func(scope, instance, appUID, platformUID string) {
		t.Helper()
		if err := writer.WriteHostObservationBatch(ctx,
			[]map[string]any{ssmRetractAppRow(appUID, instance)},
			[]map[string]any{ssmRetractPlatformRow(platformUID, instance)},
			[]map[string]any{ssmRetractEdgeRow(appUID, platformUID)},
			scope, ssmRetractGenerationID, ssmRetractEvidenceSource,
		); err != nil {
			t.Fatalf("WriteHostObservationBatch(%s): %v", scope, err)
		}
	}
	writeHost(ssmRetractScopeIn, ssmRetractInstanceIn, ssmRetractAppIn, ssmRetractPlatformIn)
	writeHost(ssmRetractScopeOut, ssmRetractInstanceOut, ssmRetractAppOut, ssmRetractPlatformOut)

	edgeQ := "MATCH (:SSMHostApplication {uid: $s})-[r:REPORTED_WITH]->(:SSMHostPlatform {uid: $t}) RETURN count(r)"
	inEdge := map[string]any{"s": ssmRetractAppIn, "t": ssmRetractPlatformIn}
	outEdge := map[string]any{"s": ssmRetractAppOut, "t": ssmRetractPlatformOut}

	assertEdgeCount(ctx, t, exec, "MATCH (n:SSMHostApplication {uid: $u}) RETURN count(n)", map[string]any{"u": ssmRetractAppIn}, 1, "write: in-scope app node present")
	assertEdgeCount(ctx, t, exec, "MATCH (n:SSMHostPlatform {uid: $u}) RETURN count(n)", map[string]any{"u": ssmRetractPlatformIn}, 1, "write: in-scope platform node present")
	assertEdgeCount(ctx, t, exec, edgeQ, inEdge, 1, "write: in-scope REPORTED_WITH present")
	assertEdgeCount(ctx, t, exec, edgeQ, outEdge, 1, "write: out-of-scope REPORTED_WITH present")

	// --- Retract the in-scope platform node (conflicted path). ---
	if _, err := writer.RetractPlatformNodes(ctx, ssmRetractScopeIn, ssmRetractEvidenceSource, []string{ssmRetractPlatformIn}); err != nil {
		t.Fatalf("RetractPlatformNodes: %v", err)
	}

	assertEdgeCount(ctx, t, exec, "MATCH (n:SSMHostPlatform {uid: $u}) RETURN count(n)", map[string]any{"u": ssmRetractPlatformIn}, 0, "retract: in-scope platform node gone")
	assertEdgeCount(ctx, t, exec, edgeQ, inEdge, 0, "retract: in-scope REPORTED_WITH died with its platform endpoint")
	// DETACH DELETE kills the node and its incident edges, never the paired node.
	assertEdgeCount(ctx, t, exec, "MATCH (n:SSMHostApplication {uid: $u}) RETURN count(n)", map[string]any{"u": ssmRetractAppIn}, 1, "retract: paired app node survives platform DETACH DELETE")
	// Scoped retract, not a wipe: the out-of-scope triple survives whole.
	assertEdgeCount(ctx, t, exec, "MATCH (n:SSMHostPlatform {uid: $u}) RETURN count(n)", map[string]any{"u": ssmRetractPlatformOut}, 1, "retract: out-of-scope platform survives")
	assertEdgeCount(ctx, t, exec, edgeQ, outEdge, 1, "retract: out-of-scope REPORTED_WITH survives")

	// --- Retract the now-stale in-scope application node. ---
	capture, err := time.Parse(time.RFC3339, ssmRetractCaptureTime)
	if err != nil {
		t.Fatalf("parse fixture capture time: %v", err)
	}
	if _, err := writer.RetractStaleApplicationNodes(ctx, ssmRetractScopeIn, ssmRetractEvidenceSource, ssm.HostTuple{
		Partition: "aws", AccountID: "000000000001", Region: "us-east-1", InstanceID: ssmRetractInstanceIn,
	}, capture.Add(time.Hour)); err != nil {
		t.Fatalf("RetractStaleApplicationNodes: %v", err)
	}

	assertEdgeCount(ctx, t, exec, "MATCH (n:SSMHostApplication {uid: $u}) RETURN count(n)", map[string]any{"u": ssmRetractAppIn}, 0, "retract: stale in-scope app node gone")
	assertEdgeCount(ctx, t, exec, "MATCH (n:SSMHostApplication {uid: $u}) RETURN count(n)", map[string]any{"u": ssmRetractAppOut}, 1, "retract: out-of-scope app survives")
	assertEdgeCount(ctx, t, exec, edgeQ, outEdge, 1, "retract: out-of-scope REPORTED_WITH still survives")
}

// cleanupSSMHostRetractScope removes every node this test creates by uid: the
// writer never stores a marker property, so the test tracks its own uids.
func cleanupSSMHostRetractScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher: `MATCH (n) WHERE n.uid IN $uids DETACH DELETE n`,
		Parameters: map[string]any{"uids": []string{
			ssmRetractAppIn, ssmRetractPlatformIn, ssmRetractAppOut, ssmRetractPlatformOut,
		}},
	}); err != nil {
		t.Fatalf("cleanup SSM host retract scope: %v", err)
	}
}
