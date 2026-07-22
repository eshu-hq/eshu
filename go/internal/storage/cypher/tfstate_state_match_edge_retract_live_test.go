// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeLive is the end-to-end
// real-backend regression test for the #5443 P1 review finding: "no
// statement DELETES the pre-existing edge" when a state backend's resolved
// owning repo changes. Runs the FULL production path
// (CanonicalNodeWriter.Write) against a real NornicDB (or Neo4j-compatible)
// backend across two generations for the SAME scope and the SAME
// TerraformStateResource uid:
//
//  1. Generation 1 (FirstGeneration=true): the state resource resolves to
//     ownerRepoA, which has a matching TerraformResource declaring the same
//     address. The writer creates the MATCHES_STATE edge.
//  2. Generation 2 (FirstGeneration=false): the SAME state resource uid is
//     refreshed, but this time its resolved OwningRepoID is ownerRepoB --
//     which has no TerraformResource declaring that address at all. Before
//     the #5443 P1 fix, terraformStateMatchesConfigEdgeStatements' MERGE
//     finds no (ownerRepoB, address) candidate to match and simply skips
//     writing a new edge, but the GENERATION-1 edge to ownerRepoA's
//     TerraformResource survives untouched -- a state resource left pointing
//     at a config resource it no longer matches. Node retraction never
//     catches this because both endpoints (the state resource and the
//     ownerRepoA TerraformResource) still exist.
//
// Opt-in, matching every other _live_test.go in this package: set
// ESHU_CYPHER_BOLT_DSN (and optionally ESHU_CYPHER_BOLT_DATABASE) to run it
// against a running graph backend. Skipped otherwise.
func TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeLive(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })

	const (
		ownerRepoA = "repo-5443-p1a-live-owner-a"
		ownerRepoB = "repo-5443-p1a-live-owner-b"
		address    = "aws_instance.web"
		stateUID   = "tf-5443-p1a-live-state"
		scopeID    = "tf-scope-5443-p1a-live"
	)
	ctx := context.Background()

	cleanup := func() {
		if err := boltWriteStatement(
			context.Background(), runner,
			`MATCH (n) WHERE (n:TerraformResource AND n.repo_id IN $repo_ids) OR (n:TerraformStateResource AND n.uid = $uid) DETACH DELETE n`,
			map[string]any{
				"repo_ids": []string{ownerRepoA, ownerRepoB},
				"uid":      stateUID,
			},
		); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}
	t.Cleanup(cleanup)
	cleanup()

	// Only ownerRepoA declares this address; ownerRepoB never does, so a
	// resource resolved to ownerRepoB in generation 2 has no config match at
	// all -- the exact "target no longer matches" case this fix covers.
	if err := boltWriteStatement(ctx, runner,
		`CREATE (c:TerraformResource {repo_id: $repo_id, name: $name, path: $path, line_number: 1})`,
		map[string]any{"repo_id": ownerRepoA, "name": address, "path": "envs/a/main.tf"},
	); err != nil {
		t.Fatalf("seed ownerRepoA TerraformResource: %v", err)
	}

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 500, nil)

	genOneRow := projector.TerraformStateResourceRow{
		UID: stateUID, Address: address, Mode: "managed", ResourceType: "aws_instance",
		Name: "web", SourceConfidence: facts.SourceConfidenceObserved, CollectorKind: "terraform_state",
		OwningRepoID: ownerRepoA,
	}
	genOne := projector.CanonicalMaterialization{
		ScopeID:                 scopeID,
		GenerationID:            "tf-generation-5443-p1a-live-1",
		FirstGeneration:         true,
		TerraformStateResources: []projector.TerraformStateResourceRow{genOneRow},
	}
	if err := writer.Write(ctx, genOne); err != nil {
		t.Fatalf("Write (generation 1) error: %v", err)
	}

	// No read runs between generation 1's write and generation 2's write
	// below -- deliberately. The pinned local NornicDB image this test runs
	// against (docker-compose.yaml's eshu-nornicdb-pr261 build) silently
	// drops a write that follows an interleaved read session on the same
	// node within this test process (confirmed with a minimal repro outside
	// this fix: CREATE, then a read-back, then MATCH+SET -- the SET never
	// commits; CREATE then MATCH+SET with no interleaved read commits
	// correctly every time). That is a local test-image write-loss defect
	// unrelated to this fix's Cypher, and it would falsely fail this test by
	// swallowing generation 2's own writes, not by exercising the retract
	// logic. generation 1 creating the edge is already covered by
	// TestCanonicalNodeWriterSkipsAmbiguousMatchesStateEdgeLive
	// (tfstate_state_match_edge_live_test.go); this test only needs to prove
	// generation 2 removes the stale edge, which the final read block below
	// does.
	//
	// Generation 2: same uid, but ownership now resolves to ownerRepoB, which
	// has no matching TerraformResource. This is the P1-A repro: before the
	// fix, the generation-1 edge to ownerRepoA survives this refresh even
	// though the resource no longer resolves there.
	genTwoRow := genOneRow
	genTwoRow.OwningRepoID = ownerRepoB
	genTwo := projector.CanonicalMaterialization{
		ScopeID:                 scopeID,
		GenerationID:            "tf-generation-5443-p1a-live-2",
		FirstGeneration:         false,
		DeltaProjection:         false,
		TerraformStateResources: []projector.TerraformStateResourceRow{genTwoRow},
	}
	if err := writer.Write(ctx, genTwo); err != nil {
		t.Fatalf("Write (generation 2) error: %v", err)
	}

	staleEdges, err := runner.runCypher(
		ctx,
		`MATCH (c:TerraformResource {repo_id: $repo_id, name: $name})-[e:MATCHES_STATE]->(s:TerraformStateResource {uid: $uid}) RETURN c.path AS path`,
		map[string]any{"repo_id": ownerRepoA, "name": address, "uid": stateUID},
	)
	if err != nil {
		t.Fatalf("read stale MATCHES_STATE edge after generation 2: %v", err)
	}
	if len(staleEdges) != 0 {
		t.Fatalf(
			"STALE EDGE SURVIVED: got %d MATCHES_STATE edge(s) from the state resource to its GENERATION-1 owner after ownership resolved to a different repo in generation 2, want 0 (#5443 P1 review finding): %#v",
			len(staleEdges), staleEdges,
		)
	}

	// The state resource node itself must still exist -- this proves the
	// edge disappeared because it was explicitly retracted, not because the
	// node was deleted and its relationships cascaded away with it.
	stateNodes, err := runner.runCypher(
		ctx,
		`MATCH (s:TerraformStateResource {uid: $uid}) RETURN s.uid AS uid`,
		map[string]any{"uid": stateUID},
	)
	if err != nil {
		t.Fatalf("read TerraformStateResource node after generation 2: %v", err)
	}
	if len(stateNodes) != 1 {
		t.Fatalf("TerraformStateResource nodes after generation 2 = %d, want 1 (the node must survive; only the stale edge should be gone)", len(stateNodes))
	}
}
