// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// instanceEdgeKey identifies one edge incident to a WorkloadInstance node in
// the tiny in-memory graph model TestWorkloadMaterializationHandlerRetracts
// SupersededPreCanonicalInstance replays from recorded Cypher calls.
type instanceEdgeKey struct {
	instanceID string
	edgeType   string
}

// replayWorkloadInstanceGraphModel replays the exact sequence of Cypher calls
// WorkloadMaterializer issued into a minimal in-memory model of WorkloadInstance
// node and edge presence: MERGE calls add, and the DETACH DELETE retract call
// removes the node and every edge keyed to that instance id (mirroring the
// DETACH DELETE Cypher semantic: deleting a node removes all its incident
// relationships regardless of type). It proves, from the handler's actual
// output, that a superseded instance and its edges are gone — not merely that
// the correct new key was written.
func replayWorkloadInstanceGraphModel(
	t *testing.T,
	calls []fakeExecutorCall,
	nodes map[string]bool,
	edges map[instanceEdgeKey]bool,
) {
	t.Helper()

	edgeCyphers := map[string]string{
		batchWorkloadInstanceOfEdgeUpsertCypher:    "INSTANCE_OF",
		batchDeploymentSourceUpsertCypher:          "DEPLOYMENT_SOURCE",
		batchRuntimePlatformRunsOnEdgeUpsertCypher: "RUNS_ON",
	}

	for _, call := range calls {
		rows, ok := call.Parameters["rows"].([]map[string]any)
		if !ok {
			continue
		}
		switch call.Cypher {
		case batchWorkloadInstanceNodeUpsertCypher:
			for _, row := range rows {
				id, _ := row["instance_id"].(string)
				if id != "" {
					nodes[id] = true
				}
			}
		case batchWorkloadInstanceRetractCypher:
			for _, row := range rows {
				id, _ := row["instance_id"].(string)
				if id == "" {
					continue
				}
				delete(nodes, id)
				for key := range edges {
					if key.instanceID == id {
						delete(edges, key)
					}
				}
			}
		default:
			if edgeType, ok := edgeCyphers[call.Cypher]; ok {
				for _, row := range rows {
					id, _ := row["instance_id"].(string)
					if id != "" {
						edges[instanceEdgeKey{instanceID: id, edgeType: edgeType}] = true
					}
				}
			}
		}
	}
}

// TestWorkloadMaterializationHandlerRetractsSupersededPreCanonicalInstance is
// the regression test for the #5473 environment-alias-contract P1: before the
// fix, environment.Canonical("production") == "prod" changed the durable
// WorkloadInstance id (workload-instance:api:production ->
// workload-instance:api:prod) but WorkloadInstance writes are MERGE-only, so
// the pre-canonical node and its INSTANCE_OF/DEPLOYMENT_SOURCE/RUNS_ON edges
// survived forever alongside the new canonical node — duplicate deployment and
// runtime truth. It seeds a pre-canonical instance (with all three edge types)
// as already-materialized graph truth, runs the handler for a candidate whose
// resolved environment is the canonical "prod", and asserts both that the
// canonical instance (and its edges) exist AND that the pre-canonical instance
// (and its edges) are gone.
func TestWorkloadMaterializationHandlerRetractsSupersededPreCanonicalInstance(t *testing.T) {
	t.Parallel()

	const (
		oldInstanceID = "workload-instance:api:production"
		newInstanceID = "workload-instance:api:prod"
	)

	lookup := &fakeWorkloadInstanceRetractionLookup{
		instances: []ExistingWorkloadInstance{
			{RepoID: "repo-api", InstanceID: oldInstanceID},
		},
	}
	// Seed the pre-canonical instance and its three edge types as
	// already-materialized graph truth — the state a repo that finalized before
	// the #5473 environment-alias contract shipped would be in.
	nodes := map[string]bool{oldInstanceID: true}
	edges := map[instanceEdgeKey]bool{
		{instanceID: oldInstanceID, edgeType: "INSTANCE_OF"}:       true,
		{instanceID: oldInstanceID, edgeType: "DEPLOYMENT_SOURCE"}: true,
		{instanceID: oldInstanceID, edgeType: "RUNS_ON"}:           true,
	}

	inputLoader := &stubWorkloadProjectionInputLoader{
		candidates: []WorkloadCandidate{
			{
				RepoID:           "repo-api",
				RepoName:         "api",
				WorkloadName:     "api",
				ResourceKinds:    []string{"deployment"},
				DeploymentRepoID: "repo-api-deploy",
				Classification:   "service",
				Confidence:       0.95,
				Provenance:       []string{"k8s_resource"},
			},
		},
		// deploymentEnvironments already carries the canonicalized environment
		// name — ExtractOverlayEnvironments (projection.go) applies
		// environment.Canonical upstream of this map in the real
		// CorrelatedWorkloadProjectionInputLoader path; a pre-correlated loader
		// like this stub supplies the already-canonical value directly.
		deploymentEnvironments: map[string][]string{
			"repo-api-deploy": {"prod"},
		},
	}
	executor := &fakeNeo4jExecutor{}

	handler := WorkloadMaterializationHandler{
		FactLoader:               &stubFactLoader{},
		InputLoader:              inputLoader,
		Materializer:             NewWorkloadMaterializer(executor),
		InstanceRetractionLookup: lookup,
	}

	now := time.Now().UTC()
	intent := Intent{
		IntentID:        "intent-wm-instance-retract",
		ScopeID:         "scope-api",
		GenerationID:    "gen-2",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-api"},
		RelatedScopeIDs: []string{"scope-api"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want succeeded", result.Status)
	}

	// The lookup must have been scoped to exactly the repository this pass
	// materialized, tagged with the workload materialization evidence source.
	if got, want := len(lookup.calledRepoIDs), 1; got != want || lookup.calledRepoIDs[0] != "repo-api" {
		t.Fatalf("lookup.calledRepoIDs = %v, want [repo-api]", lookup.calledRepoIDs)
	}
	if got, want := lookup.calledSource, EvidenceSourceWorkloads; got != want {
		t.Fatalf("lookup.calledSource = %q, want %q", got, want)
	}

	// The canonical instance MERGE must have been issued.
	if !containsCypher(executor.calls, "MERGE (i:WorkloadInstance {id: row.instance_id})") {
		t.Fatal("missing canonical WorkloadInstance MERGE cypher")
	}
	newRows := rowsForCypher(t, executor.calls, "MERGE (i:WorkloadInstance {id: row.instance_id})")
	foundNew := false
	for _, row := range newRows {
		if row["instance_id"] == newInstanceID {
			foundNew = true
		}
	}
	if !foundNew {
		t.Fatalf("canonical instance %q not written; rows = %#v", newInstanceID, newRows)
	}

	// The pre-canonical instance must have been retracted via DETACH DELETE.
	if !containsCypher(executor.calls, "DETACH DELETE i") {
		t.Fatal("missing DETACH DELETE retract cypher for superseded instance")
	}
	retractRows := rowsForCypher(t, executor.calls, "DETACH DELETE i")
	foundOld := false
	for _, row := range retractRows {
		if row["instance_id"] == oldInstanceID {
			foundOld = true
		}
	}
	if !foundOld {
		t.Fatalf("superseded instance %q not retracted; rows = %#v", oldInstanceID, retractRows)
	}

	// The retract call must carry the delete-time ownership predicate params
	// (CRITICAL 2): repo_ids scoped to exactly this pass's repository, and the
	// workload materialization evidence source.
	for _, call := range executor.calls {
		if call.Cypher != batchWorkloadInstanceRetractCypher {
			continue
		}
		gotRepoIDs, ok := call.Parameters["repo_ids"].([]string)
		if !ok || len(gotRepoIDs) != 1 || gotRepoIDs[0] != "repo-api" {
			t.Fatalf("retract call repo_ids = %#v, want [repo-api]", call.Parameters["repo_ids"])
		}
		if got := call.Parameters["evidence_source"]; got != EvidenceSourceWorkloads {
			t.Fatalf("retract call evidence_source = %#v, want %q", got, EvidenceSourceWorkloads)
		}
	}

	// Ordering: retraction must be issued strictly after the canonical write is
	// confirmed, never interleaved before it.
	newWriteIndex, retractIndex := -1, -1
	for i, call := range executor.calls {
		switch call.Cypher {
		case batchWorkloadInstanceNodeUpsertCypher:
			if newWriteIndex == -1 {
				newWriteIndex = i
			}
		case batchWorkloadInstanceRetractCypher:
			if retractIndex == -1 {
				retractIndex = i
			}
		}
	}
	if newWriteIndex == -1 || retractIndex == -1 {
		t.Fatalf("expected both a write and a retract call, got newWriteIndex=%d retractIndex=%d", newWriteIndex, retractIndex)
	}
	if retractIndex < newWriteIndex {
		t.Fatalf("retract call (index %d) ran before the new-generation write (index %d)", retractIndex, newWriteIndex)
	}

	// Replay the actual recorded Cypher sequence into the seeded in-memory
	// model and assert on the resulting state: this is the strongest available
	// proof, short of a live graph backend, that the superseded instance and
	// its edges are gone while the canonical instance and its edges exist.
	replayWorkloadInstanceGraphModel(t, executor.calls, nodes, edges)

	if !nodes[newInstanceID] {
		t.Fatalf("canonical instance %q missing from replayed graph model", newInstanceID)
	}
	if nodes[oldInstanceID] {
		t.Fatalf("superseded instance %q still present in replayed graph model", oldInstanceID)
	}
	for _, edgeType := range []string{"INSTANCE_OF", "DEPLOYMENT_SOURCE", "RUNS_ON"} {
		if edges[instanceEdgeKey{instanceID: oldInstanceID, edgeType: edgeType}] {
			t.Fatalf("superseded instance %q still has a %s edge in replayed graph model", oldInstanceID, edgeType)
		}
	}
	if !edges[instanceEdgeKey{instanceID: newInstanceID, edgeType: "INSTANCE_OF"}] {
		t.Fatalf("canonical instance %q missing its INSTANCE_OF edge in replayed graph model", newInstanceID)
	}

	if got := result.SubDurations["instance_retract"]; got < 0 {
		t.Fatalf("SubDurations[instance_retract] = %v, want >= 0", got)
	}
}

// TestWorkloadMaterializationHandlerNoInstanceRetractionLookupIsNoop confirms
// that leaving InstanceRetractionLookup nil (the default) never issues a
// retract call, keeping the hot workload materialization path byte-identical
// for callers that have not wired the lookup yet.
func TestWorkloadMaterializationHandlerNoInstanceRetractionLookupIsNoop(t *testing.T) {
	t.Parallel()

	inputLoader := &stubWorkloadProjectionInputLoader{
		candidates: []WorkloadCandidate{
			{
				RepoID:         "repo-api",
				RepoName:       "api",
				WorkloadName:   "api",
				ResourceKinds:  []string{"deployment"},
				Classification: "service",
				Confidence:     0.95,
				Provenance:     []string{"k8s_resource"},
			},
		},
		deploymentEnvironments: map[string][]string{
			"repo-api": {"prod"},
		},
	}
	executor := &fakeNeo4jExecutor{}
	handler := WorkloadMaterializationHandler{
		FactLoader:   &stubFactLoader{},
		InputLoader:  inputLoader,
		Materializer: NewWorkloadMaterializer(executor),
	}

	now := time.Now().UTC()
	intent := Intent{
		IntentID:        "intent-wm-instance-retract-noop",
		ScopeID:         "scope-api",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-api"},
		RelatedScopeIDs: []string{"scope-api"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if containsCypher(executor.calls, "DETACH DELETE i") {
		t.Fatal("retract cypher issued despite nil InstanceRetractionLookup")
	}
}
