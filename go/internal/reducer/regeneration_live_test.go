// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// T4 regeneration case of the #5385 migration test table: the same
// repository regenerates (a non-first, non-delta generation) and its
// repository-scoped Workload nodes survive together with a USES edge written
// by an AWS-scope generation and a DOCUMENTS edge written by a
// documentation-scope generation. Since #7285 the Repository node and its
// DEFINES also survive; only a stale DEFINES is retracted by
// workload_materialization. The scoped key is what makes survival exact:
// generation 2 derives the same workload:<repo_id>:<name> ids generation 1
// wrote, so the MERGEs re-attach instead of duplicating.
//
// Run against an isolated Neo4j container (the owner's graph-proof backend);
// see repository_retry_live_harness_test.go for the exact invocation. This
// file only writes the test: it runs in the flip wave's live session after
// the golden-corpus gate releases the backend ports.
package reducer_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestLiveWorkloadRegenerationSurvivesScopedIdentity(t *testing.T) {
	live := openRepoRetryLive(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := live.exec.Execute(ctx, cypher.Statement{
			Cypher:     `MATCH (n) WHERE n.repo_id STARTS WITH $repo_prefix DETACH DELETE n`,
			Parameters: map[string]any{"repo_prefix": live.repoID("")},
		}); err != nil {
			t.Errorf("clean regeneration workloads: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	const repo, workload = "regen", "checkout"
	repoID := live.repoID(repo)
	workloadID := "workload:" + repoID + ":" + workload
	instanceID := "workload-instance:" + repoID + ":" + workload + ":prod"

	regenCandidates := func() []reducer.WorkloadCandidate {
		return []reducer.WorkloadCandidate{{
			RepoID: repoID, RepoName: repo, WorkloadName: workload,
			Classification: "service", Confidence: 0.95,
			Namespaces:   []string{"prod"},
			APIEndpoints: []reducer.APIEndpointSignal{{Path: "/v1/checkout", Methods: []string{"GET"}}},
		}}
	}
	handleRegen := func(gen string, delta bool) {
		t.Helper()
		handler := reducer.WorkloadMaterializationHandler{
			FactLoader:           liveRepoFactLoader{repoIDs: []string{repoID}, delta: delta},
			InputLoader:          liveCandidateLoader{candidates: regenCandidates()},
			Materializer:         live.materializer(),
			RepositoryEdgeReader: live.edgeReader(),
		}
		now := time.Now().UTC()
		if _, err := handler.Handle(ctx, reducer.Intent{
			IntentID: fmt.Sprintf("intent-%s-%s", repo, gen), ScopeID: "git-repository-scope:" + repoID,
			GenerationID: gen, SourceSystem: "git", Domain: reducer.DomainWorkloadMaterialization,
			Cause: "facts projected", EntityKeys: []string{repoID}, EnqueuedAt: now, AvailableAt: now,
			Status: reducer.IntentStatusPending,
		}); err != nil {
			t.Fatalf("handle workload_materialization %s: %v", gen, err)
		}
	}

	// Production precondition: repo sync owns the Repository node and runs
	// before workload_materialization, whose DEFINES upsert only MATCHes it
	// (like every other template in materializer_templates.go). Without the
	// node the upsert and the stale-DEFINES seed below both no-op silently
	// on the MATCH, and the final DEFINES read is vacuously empty.
	if err := live.exec.Execute(ctx, cypher.Statement{
		Cypher:     `MERGE (r:Repository {id: $repo_id}) SET r.name = $name`,
		Parameters: map[string]any{"repo_id": repoID, "name": repo},
	}); err != nil {
		t.Fatalf("seed Repository node: %v", err)
	}

	// Generation 1 materializes the scoped node, instance, endpoint, and
	// DEFINES on the repository-scoped key.
	handleRegen("gen-1", false)
	gen1, err := live.exec.readRows(ctx, `MATCH (n) WHERE n.id = $id RETURN n.id AS id`,
		map[string]any{"id": workloadID})
	if err != nil {
		t.Fatalf("read gen-1 Workload: %v", err)
	}
	if len(gen1) != 1 {
		t.Fatalf("gen-1 Workload nodes with id %q = %d, want 1", workloadID, len(gen1))
	}

	// Cross-scope generations attach their edges to the scoped nodes: USES
	// from the AWS scope onto the instance, DOCUMENTS from the
	// documentation scope onto the workload.
	if err := live.exec.Execute(ctx, cypher.Statement{
		Cypher: `MATCH (i:WorkloadInstance {id: $instance_id})
			MERGE (res:CloudResource {id: $res_id})
			SET res.repo_id = $repo_id
			MERGE (i)-[:USES {evidence_source: 'aws'}]->(res)
			WITH i
			MATCH (w:Workload {id: $workload_id})
			MERGE (doc:Document {id: $doc_id})
			SET doc.repo_id = $repo_id
			MERGE (w)-[:DOCUMENTS {evidence_source: 'documentation'}]->(doc)`,
		Parameters: map[string]any{
			"instance_id": instanceID, "workload_id": workloadID, "repo_id": repoID,
			"res_id": "aws:res-" + live.prefix, "doc_id": "doc-" + live.prefix,
		},
	}); err != nil {
		t.Fatalf("seed cross-scope edges: %v", err)
	}

	// A stale DEFINES from a renamed workload must go; everything else stays.
	// It carries the materialization evidence source: the retract only owns
	// its own edges, so any other source would be invisible to it.
	staleWorkloadID := "workload:" + repoID + ":checkout-old"
	if err := live.exec.Execute(ctx, cypher.Statement{
		Cypher: `MATCH (r:Repository {id: $repo_id})
			MERGE (w:Workload {id: $stale_id})
			SET w.name = 'checkout-old', w.repo_id = $repo_id
			MERGE (r)-[:DEFINES {evidence_source: $evidence_source}]->(w)`,
		Parameters: map[string]any{"repo_id": repoID, "stale_id": staleWorkloadID, "evidence_source": reducer.EvidenceSourceWorkloads},
	}); err != nil {
		t.Fatalf("seed stale DEFINES: %v", err)
	}

	// Generation 2 regenerates the same repository: non-first, non-delta.
	handleRegen("gen-2", false)

	assertCount := func(label, what string, want int) {
		t.Helper()
		rows, err := live.exec.readRows(ctx, `MATCH (n) WHERE n.id = $id RETURN n.id AS id`,
			map[string]any{"id": label})
		_ = rows
		if err != nil {
			t.Fatalf("read %s: %v", what, err)
		}
		if got := len(rows); got != want {
			t.Fatalf("%s nodes = %d, want %d", what, got, want)
		}
	}
	assertCount(workloadID, "scoped Workload", 1)
	assertCount(instanceID, "scoped WorkloadInstance", 1)

	edges, err := live.exec.readRows(ctx, `MATCH (w:Workload {id: $id})-[rel]->(other) RETURN type(rel) AS t, rel.evidence_source AS src`,
		map[string]any{"id": workloadID})
	if err != nil {
		t.Fatalf("read workload edges: %v", err)
	}
	seen := map[string]bool{}
	for _, edge := range edges {
		seen[edge["t"].(string)+"/"+edge["src"].(string)] = true
	}
	for _, want := range []string{"DOCUMENTS/documentation"} {
		if !seen[want] {
			t.Fatalf("workload edges = %v, want %q to survive regeneration", edges, want)
		}
	}
	instanceEdges, err := live.exec.readRows(ctx, `MATCH (i:WorkloadInstance {id: $id})-[rel]->(other) RETURN type(rel) AS t, rel.evidence_source AS src`,
		map[string]any{"id": instanceID})
	if err != nil {
		t.Fatalf("read instance edges: %v", err)
	}
	foundUses := false
	for _, edge := range instanceEdges {
		if edge["t"] == "USES" && edge["src"] == "aws" {
			foundUses = true
		}
	}
	if !foundUses {
		t.Fatalf("instance edges = %v, want the AWS USES edge to survive regeneration", instanceEdges)
	}

	defines, err := live.exec.readRows(ctx, `MATCH (:Repository {id: $repo_id})-[rel:DEFINES]->(w:Workload) RETURN w.id AS id`,
		map[string]any{"repo_id": repoID})
	if err != nil {
		t.Fatalf("read DEFINES: %v", err)
	}
	if len(defines) != 1 {
		t.Fatalf("DEFINES targets = %v, want only the surviving scoped workload", defines)
	}
	if defines[0]["id"] != workloadID {
		t.Fatalf("DEFINES target = %v, want %q (stale DEFINES retracted)", defines[0]["id"], workloadID)
	}
}
