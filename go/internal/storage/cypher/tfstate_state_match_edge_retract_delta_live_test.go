// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeOnDeltaCycleLive is the
// #5623 real-backend regression for the P0 review finding on the scoped-token
// infra predicate fix: terraformStateMatchesConfigEdgeRetractStatements used
// to skip entirely on a delta cycle (mat.DeltaProjection == true), matching
// terraformStateResourceRetractStatements' own DeltaProjection skip even
// though the two statements' danger profiles are NOT the same (see this
// test's sibling doc comment on terraformStateMatchesConfigEdgeRetractStatements,
// tfstate_state_match_edge_retract.go). The practical effect: when a state
// resource's resolved OwningRepoID changes to a DIFFERENT repo (not just
// becomes unmatched) on a delta cycle, terraformStateMatchesConfigEdgeStatements'
// MERGE (which has no DeltaProjection guard) writes the NEW edge immediately,
// but the retract that would delete the OLD edge was skipped, so the state
// resource carried MATCHES_STATE edges to TWO different repos simultaneously
// until the next full reconciliation generation (hours later per
// ESHU_REPO_RECONCILE_INTERVAL_HOURS). infra_scope_grant.go's scope predicate
// assumes at most one such edge and admits the resource for EITHER repo's
// scoped grant during that window -- a tenant-visibility leak.
//
// Runs the FULL production path (CanonicalNodeWriter.Write) across three
// generations for the SAME scope and the SAME TerraformStateResource uid,
// unlike the sibling TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeLive
// (which only covers the already-safe non-delta generation-2 case and the
// "no longer matches anything" case, not "matches a DIFFERENT repo" on a
// DELTA cycle -- exactly the gap this test closes):
//
//  1. Generation 1 (FirstGeneration=true, DeltaProjection=false): the state
//     resource resolves to ownerRepoC. ownerRepoC declares a matching
//     TerraformResource at the same address. The writer creates the
//     MATCHES_STATE edge to ownerRepoC.
//  2. Generation 2 (FirstGeneration=false, DeltaProjection=TRUE): the SAME
//     state resource uid is refreshed on a DELTA cycle, but this time its
//     resolved OwningRepoID is ownerRepoA -- a DIFFERENT repo that ALSO
//     declares a matching TerraformResource at the same address (so the MERGE
//     genuinely succeeds and writes a second, real edge -- this is the
//     "reassigned to a different repo," not "orphaned," case; both edges
//     point at real, matching TerraformResource nodes).
//  3. Generation 3 (an exact repeat of generation 2, including
//     DeltaProjection=true and the SAME GenerationID): re-runs the identical
//     write, simulating a partial-failure retry of generation 2's own
//     retract statement. buildTerraformStateStatements' own "Partial-failure
//     note" says every phase, including this retract, must be independently
//     idempotent; this proves it for the newly-enabled delta-cycle path
//     specifically.
//
// No read runs between any of the three writes, matching this file's sibling
// TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeLive's own documented
// reason: the pinned local NornicDB image can silently drop a write that
// follows an interleaved read session on the same node within one test
// process. All assertions run in a single read block after every write
// completes.
//
// Opt-in, matching every other _live_test.go in this package: set
// ESHU_CYPHER_BOLT_DSN (and optionally ESHU_CYPHER_BOLT_DATABASE) to run it
// against a running graph backend. Skipped otherwise.
func TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeOnDeltaCycleLive(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })

	const (
		ownerRepoC = "repo-5623-p0-live-owner-c-fresh-gen1"
		ownerRepoA = "repo-5623-p0-live-owner-a-fresh-gen2-delta"
		address    = "aws_instance.web"
		stateUID   = "tf-5623-p0-live-state"
		scopeID    = "tf-scope-5623-p0-live"
	)
	ctx := context.Background()

	cleanup := func() {
		if err := boltWriteStatement(
			context.Background(), runner,
			`MATCH (n) WHERE (n:TerraformResource AND n.repo_id IN $repo_ids) OR (n:TerraformStateResource AND n.uid = $uid) DETACH DELETE n`,
			map[string]any{
				"repo_ids": []string{ownerRepoC, ownerRepoA},
				"uid":      stateUID,
			},
		); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}
	t.Cleanup(cleanup)
	cleanup()

	// Both repos declare the SAME address, so both generations' MERGE finds a
	// real match -- this is the "reassigned to a different owner," not
	// "orphaned," case.
	if err := boltWriteStatement(
		ctx, runner,
		`CREATE (c:TerraformResource {repo_id: $repo_id, name: $name, path: $path, line_number: 1})`,
		map[string]any{"repo_id": ownerRepoC, "name": address, "path": "envs/c/main.tf"},
	); err != nil {
		t.Fatalf("seed ownerRepoC TerraformResource: %v", err)
	}
	if err := boltWriteStatement(
		ctx, runner,
		`CREATE (c:TerraformResource {repo_id: $repo_id, name: $name, path: $path, line_number: 1})`,
		map[string]any{"repo_id": ownerRepoA, "name": address, "path": "envs/a/main.tf"},
	); err != nil {
		t.Fatalf("seed ownerRepoA TerraformResource: %v", err)
	}

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 500, nil)

	baseRow := projector.TerraformStateResourceRow{
		UID: stateUID, Address: address, Mode: "managed", ResourceType: "aws_instance",
		Name: "web", SourceConfidence: facts.SourceConfidenceObserved, CollectorKind: "terraform_state",
	}

	genOneRow := baseRow
	genOneRow.OwningRepoID = ownerRepoC
	genOne := projector.CanonicalMaterialization{
		ScopeID:                 scopeID,
		GenerationID:            "tf-generation-5623-p0-live-1",
		FirstGeneration:         true,
		TerraformStateResources: []projector.TerraformStateResourceRow{genOneRow},
	}
	if err := writer.Write(ctx, genOne); err != nil {
		t.Fatalf("Write (generation 1, full) error: %v", err)
	}

	// No read runs between generation 1's write and generation 2's write below
	// -- deliberately, mirroring TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeLive's
	// own documented reason: the pinned local NornicDB image silently drops a
	// write that follows an interleaved read session on the same node within
	// this test process. Generation 1 creating the edge is already covered by
	// TestCanonicalNodeWriterSkipsAmbiguousMatchesStateEdgeLive and this
	// test's own sibling; reading here would risk silently swallowing
	// generation 2 or 3's writes rather than exercising the retract logic.
	//
	// Generation 2: same uid, resolved ownership REASSIGNED to ownerRepoA, on
	// a DELTA cycle. Before the #5623 fix, terraformStateMatchesConfigEdgeRetractStatements
	// skipped entirely here (mat.DeltaProjection == true), so the generation-1
	// edge to ownerRepoC would survive alongside the new generation-2 edge to
	// ownerRepoA -- the P0 leak window.
	genTwoRow := baseRow
	genTwoRow.OwningRepoID = ownerRepoA
	genTwo := projector.CanonicalMaterialization{
		ScopeID:                 scopeID,
		GenerationID:            "tf-generation-5623-p0-live-2",
		FirstGeneration:         false,
		DeltaProjection:         true,
		TerraformStateResources: []projector.TerraformStateResourceRow{genTwoRow},
	}
	if err := writer.Write(ctx, genTwo); err != nil {
		t.Fatalf("Write (generation 2, delta) error: %v", err)
	}

	// Generation 3: re-run generation 2's exact write (same GenerationID, no
	// read in between -- same write-loss avoidance as above), simulating a
	// partial-failure retry. buildTerraformStateStatements' own
	// "Partial-failure note" requires every phase, including this retract, to
	// be independently idempotent; this proves it for the newly-enabled
	// delta-cycle path specifically. The end state after this retry must be
	// identical to the end state after generation 2 alone.
	genThree := genTwo
	if err := writer.Write(ctx, genThree); err != nil {
		t.Fatalf("Write (generation 2 retry) error: %v", err)
	}

	// All reads run here, after every write completes, matching the sibling
	// test's own read-only-at-the-end structure.
	edgeCount := func(repoID string) int64 {
		t.Helper()
		n, err := boltCount(
			ctx, runner,
			`MATCH (c:TerraformResource {repo_id: $repo_id, name: $name})-[e:MATCHES_STATE]->(s:TerraformStateResource {uid: $uid}) RETURN count(e) AS count`,
			map[string]any{"repo_id": repoID, "name": address, "uid": stateUID},
		)
		if err != nil {
			t.Fatalf("count MATCHES_STATE edges from %s: %v", repoID, err)
		}
		return n
	}

	if got := edgeCount(ownerRepoA); got != 1 {
		t.Fatalf("after generation 2 (delta) + generation-2 retry: MATCHES_STATE edges from ownerRepoA = %d, want 1 (the reassigned edge must exist, and a retry must not duplicate it)", got)
	}
	if got := edgeCount(ownerRepoC); got != 0 {
		t.Fatalf(
			"P0 LEAK REPRODUCED: after generation 2 (delta) reassigned ownership to ownerRepoA, MATCHES_STATE edges from the GENERATION-1 owner ownerRepoC = %d, want 0. "+
				"A surviving stale edge here means the state resource carries edges to two different repos simultaneously, and infra_scope_grant.go's scope "+
				"predicate would admit it for ownerRepoC's grant even though ownership has moved to ownerRepoA (#5623 P0 review finding)", got,
		)
	}

	stateNodes, err := runner.runCypher(
		ctx,
		`MATCH (s:TerraformStateResource {uid: $uid}) RETURN s.uid AS uid`,
		map[string]any{"uid": stateUID},
	)
	if err != nil {
		t.Fatalf("read TerraformStateResource node after generation 2: %v", err)
	}
	if len(stateNodes) != 1 {
		t.Fatalf("TerraformStateResource nodes after generation 2 = %d, want 1 (the node must survive across the ownership reassignment)", len(stateNodes))
	}
}
