// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized symbol->runtime edge retract coverage (C-14 #4367
// retract axis): HANDLES_ROUTE, RUNS_IN, INVOKES_CLOUD_ACTION, and the
// workload DEPENDS_ON.
//
// These four domains already retract through single-label anchored single
// statements (no grouping, no disjunction), so this is a coverage-claim proof,
// not a fix: the replay-coverage gate only checks that a referenced artifact
// exists, so claiming the edge types without a live test that writes and
// retracts each one on the pinned backend would be a false green.
//
// The test drives the REAL production write and retract paths
// (cypher.EdgeWriter.WriteEdges / RetractEdges) for DomainHandlesRoute
// (Function->Endpoint), DomainRunsIn (Function->Workload via the
// Repository-DEFINES chain), DomainInvokesCloudAction (Function->CloudAction,
// the action node MERGEd inline), and DomainWorkloadDependency
// (Workload->Workload DEPENDS_ON), each with an out-of-scope control edge.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	rtInRepo  = "replay-runtime-edge:in"
	rtOutRepo = "replay-runtime-edge:out"
	rtMarker  = "replay-runtime-edge"
	rtSource  = "finalization/workloads"

	rtFnRoute    = "runtime-edge:fn:route"
	rtFnRun      = "runtime-edge:fn:run"
	rtFnInvoke   = "runtime-edge:fn:invoke"
	rtFnOutRoute = "runtime-edge:fn:route-out"
	rtRoutePath  = "/api/v0/replay-runtime"
	rtWorkloadIn = "runtime-edge:wl:in"
	rtWorkloadA  = "runtime-edge:wl:a"
	rtWorkloadB  = "runtime-edge:wl:b"
	rtWorkloadC  = "runtime-edge:wl:out-c"
	rtWorkloadD  = "runtime-edge:wl:out-d"
	rtActionID   = "runtime-edge:action:s3-get"
)

// TestReducerRuntimeEdgeRetractGraphTruth proves the HANDLES_ROUTE, RUNS_IN,
// INVOKES_CLOUD_ACTION, and workload DEPENDS_ON retract paths delete only the
// in-scope edges on a real NornicDB.
func TestReducerRuntimeEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the runtime-edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupRuntimeEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupRuntimeEdgeScope(cleanCtx, t, exec)
	})

	seedRuntimeEdgeNodes(ctx, t, exec)

	writer := cypher.NewEdgeWriter(exec, 0)
	inRows := func(domain string, rows []reducer.SharedProjectionIntentRow) {
		t.Helper()
		if err := writer.WriteEdges(ctx, domain, rows, rtSource); err != nil {
			t.Fatalf("WriteEdges(%s): %v", domain, err)
		}
	}

	inRows(reducer.DomainHandlesRoute, []reducer.SharedProjectionIntentRow{
		{IntentID: "route-in", RepositoryID: rtInRepo, Payload: map[string]any{
			"function_entity_id": rtFnRoute, "repo_id": rtInRepo, "path": rtRoutePath,
		}},
		{IntentID: "route-out", RepositoryID: rtOutRepo, Payload: map[string]any{
			"function_entity_id": rtFnOutRoute, "repo_id": rtOutRepo, "path": rtRoutePath,
		}},
	})
	inRows(reducer.DomainRunsIn, []reducer.SharedProjectionIntentRow{
		{IntentID: "runs-in", RepositoryID: rtInRepo, Payload: map[string]any{
			"function_id": rtFnRun, "repo_id": rtInRepo,
		}},
	})
	inRows(reducer.DomainInvokesCloudAction, []reducer.SharedProjectionIntentRow{
		{IntentID: "invoke-in", RepositoryID: rtInRepo, Payload: map[string]any{
			"function_id": rtFnInvoke, "cloud_action": "s3:GetObject", "action_id": rtActionID,
		}},
	})
	inRows(reducer.DomainWorkloadDependency, []reducer.SharedProjectionIntentRow{
		{IntentID: "wldep-in", RepositoryID: rtInRepo, Payload: map[string]any{
			"workload_id": rtWorkloadA, "target_workload_id": rtWorkloadB,
		}},
		{IntentID: "wldep-out", RepositoryID: rtOutRepo, Payload: map[string]any{
			"workload_id": rtWorkloadC, "target_workload_id": rtWorkloadD,
		}},
	})

	routeQ := "MATCH (:Function {uid: $f})-[r:HANDLES_ROUTE]->(:Endpoint {repo_id: $repo, path: $p}) RETURN count(r)"
	runsInQ := "MATCH (:Function {uid: $f})-[r:RUNS_IN]->(:Workload {id: $w}) RETURN count(r)"
	invokeQ := "MATCH (:Function {uid: $f})-[r:INVOKES_CLOUD_ACTION]->(:CloudAction {id: $a}) RETURN count(r)"
	wlDepQ := "MATCH (:Workload {id: $s})-[r:DEPENDS_ON]->(:Workload {id: $t}) RETURN count(r)"

	inRoute := map[string]any{"f": rtFnRoute, "repo": rtInRepo, "p": rtRoutePath}
	outRoute := map[string]any{"f": rtFnOutRoute, "repo": rtOutRepo, "p": rtRoutePath}
	inRun := map[string]any{"f": rtFnRun, "w": rtWorkloadIn}
	inInvoke := map[string]any{"f": rtFnInvoke, "a": rtActionID}
	inWlDep := map[string]any{"s": rtWorkloadA, "t": rtWorkloadB}
	outWlDep := map[string]any{"s": rtWorkloadC, "t": rtWorkloadD}

	assertEdgeCount(ctx, t, exec, routeQ, inRoute, 1, "write: in-scope HANDLES_ROUTE present")
	assertEdgeCount(ctx, t, exec, routeQ, outRoute, 1, "write: out-of-scope HANDLES_ROUTE present")
	assertEdgeCount(ctx, t, exec, runsInQ, inRun, 1, "write: in-scope RUNS_IN present")
	assertEdgeCount(ctx, t, exec, invokeQ, inInvoke, 1, "write: in-scope INVOKES_CLOUD_ACTION present")
	assertEdgeCount(ctx, t, exec, wlDepQ, inWlDep, 1, "write: in-scope workload DEPENDS_ON present")
	assertEdgeCount(ctx, t, exec, wlDepQ, outWlDep, 1, "write: out-of-scope workload DEPENDS_ON present")

	retract := func(domain string) {
		t.Helper()
		rows := []reducer.SharedProjectionIntentRow{
			{IntentID: "retract-" + domain, RepositoryID: rtInRepo, Payload: map[string]any{"repo_id": rtInRepo}},
		}
		if err := writer.RetractEdges(ctx, domain, rows, rtSource); err != nil {
			t.Fatalf("RetractEdges(%s): %v", domain, err)
		}
	}
	retract(reducer.DomainHandlesRoute)
	retract(reducer.DomainRunsIn)
	retract(reducer.DomainInvokesCloudAction)
	retract(reducer.DomainWorkloadDependency)

	assertEdgeCount(ctx, t, exec, routeQ, inRoute, 0, "retract: in-scope HANDLES_ROUTE gone")
	assertEdgeCount(ctx, t, exec, runsInQ, inRun, 0, "retract: in-scope RUNS_IN gone")
	assertEdgeCount(ctx, t, exec, invokeQ, inInvoke, 0, "retract: in-scope INVOKES_CLOUD_ACTION gone")
	assertEdgeCount(ctx, t, exec, wlDepQ, inWlDep, 0, "retract: in-scope workload DEPENDS_ON gone")
	// Scoped retracts, not wipes: the out-of-scope edges survive.
	assertEdgeCount(ctx, t, exec, routeQ, outRoute, 1, "retract: out-of-scope HANDLES_ROUTE survives")
	assertEdgeCount(ctx, t, exec, wlDepQ, outWlDep, 1, "retract: out-of-scope workload DEPENDS_ON survives")
	// Edge retract must never delete endpoint nodes (the CloudAction node is
	// deliberately shared and stays).
	for _, q := range []struct {
		cypherText string
		key        string
	}{
		{"MATCH (n:Function {uid: $u}) RETURN count(n)", rtFnRoute},
		{"MATCH (n:Function {uid: $u}) RETURN count(n)", rtFnRun},
		{"MATCH (n:Function {uid: $u}) RETURN count(n)", rtFnInvoke},
		{"MATCH (n:Workload {id: $u}) RETURN count(n)", rtWorkloadIn},
		{"MATCH (n:Workload {id: $u}) RETURN count(n)", rtWorkloadA},
		{"MATCH (n:Workload {id: $u}) RETURN count(n)", rtWorkloadB},
		{"MATCH (n:CloudAction {id: $u}) RETURN count(n)", rtActionID},
	} {
		assertEdgeCount(ctx, t, exec, q.cypherText, map[string]any{"u": q.key}, 1, "node survives: "+q.key)
	}
}

// seedRuntimeEdgeNodes creates the Function, Endpoint, Repository-DEFINES
// chain, and Workload nodes the four write templates MATCH. The
// EvidenceArtifact-free CloudAction node is MERGEd by the write template
// itself.
func seedRuntimeEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE (:Function {uid: $fnRoute, repo_id: $in, marker: $marker}),
       (:Function {uid: $fnRun, repo_id: $in, marker: $marker}),
       (:Function {uid: $fnInvoke, repo_id: $in, marker: $marker}),
       (:Function {uid: $fnOutRoute, repo_id: $out, marker: $marker}),
       (:Endpoint {repo_id: $in, path: $route, marker: $marker}),
       (:Endpoint {repo_id: $out, path: $route, marker: $marker}),
       (repoIn:Repository {id: $in, marker: $marker}),
       (wlIn:Workload {id: $wlIn, repo_id: $in, marker: $marker}),
       (:Workload {id: $wlA, repo_id: $in, marker: $marker}),
       (:Workload {id: $wlB, repo_id: $in, marker: $marker}),
       (:Workload {id: $wlC, repo_id: $out, marker: $marker}),
       (:Workload {id: $wlD, repo_id: $out, marker: $marker}),
       (repoIn)-[:DEFINES]->(wlIn)`,
		Parameters: map[string]any{
			"fnRoute": rtFnRoute, "fnRun": rtFnRun, "fnInvoke": rtFnInvoke,
			"fnOutRoute": rtFnOutRoute, "route": rtRoutePath,
			"in": rtInRepo, "out": rtOutRepo,
			"wlIn": rtWorkloadIn, "wlA": rtWorkloadA, "wlB": rtWorkloadB,
			"wlC": rtWorkloadC, "wlD": rtWorkloadD,
			"marker": rtMarker,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed runtime-edge nodes: %v", err)
	}
}

// cleanupRuntimeEdgeScope removes every node this test creates, including the
// write-MERGEd CloudAction node.
func cleanupRuntimeEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, stmt := range []cypher.Statement{
		{
			Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
			Parameters: map[string]any{"marker": rtMarker},
		},
		{
			Cypher:     `MATCH (a:CloudAction {id: $action}) DETACH DELETE a`,
			Parameters: map[string]any{"action": rtActionID},
		},
	} {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup runtime-edge scope: %v", err)
		}
	}
}
