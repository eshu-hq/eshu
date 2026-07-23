// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// tfstate_dispatch_order_live_test.go is the real-dispatch counterpart to
// #5680: two independent defects in PhaseGroupExecutor's
// executeGroupedChunksWithDrain (go/internal/storage/nornicdb/phase_group_executor_retract.go)
// that only manifest when a terraform_state canonical write is dispatched
// through the REAL PhaseGroupExecutor with a REAL GroupExecutor, exactly as
// cmd/reducer wires it in production.
//
// go/internal/storage/cypher/tfstate_canonical_writer_retract_live_test.go's
// TestTerraformStateResourceMigrationLive is NOT a substitute for this: it
// drives buildTerraformStateStatements one statement at a time through
// runner.runCypherSingle, bypassing PhaseGroupExecutor entirely. Every
// statement there runs autocommit regardless of Operation or Drain, so that
// test would pass identically whether or not the dispatcher bug in this file
// exists -- a false-green for exactly the bug #5680 fixes. These tests
// instead drive the same production Cypher through
// *cypher.CanonicalNodeWriter.Write (the real production entry point), which
// dispatches through storagenornicdb.PhaseGroupExecutor wired with a real
// Bolt-backed GroupExecutor via openDeltaLiveBackend / livePhaseGroupExecutor
// (executor_test.go) -- the only way to actually exercise
// executeGroupedChunksWithDrain's ge.ExecuteGroup vs autocommit dispatch
// split.
//
// SKIPs cleanly unless ESHU_REPLAY_TIER_LIVE=1 and Bolt env is configured
// (ESHU_GRAPH_BACKEND, NEO4J_URI, ESHU_NEO4J_DATABASE), matching every other
// live test in this package.

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestTfstateResourceRetractDispatchLive proves #5680 defect 1: a non-Drain
// OperationCanonicalRetract statement (the terraform_state resource-sweep
// DETACH DELETE, tfstate_canonical_writer_retract.go) was bundled into
// executeGroupedChunksWithDrain's single ge.ExecuteGroup call for "remaining"
// statements. Every retract DELETE is ExecuteGroup-unsafe on the pinned
// NornicDB v1.1.11 -- it silently under-applies inside a managed transaction,
// even alone in a one-statement group
// (docs/public/reference/nornicdb-query-pitfalls.md, "retract DELETEs run
// through Execute, never ExecuteGroup") -- so a stale terraform_state
// resource never actually got retracted.
//
// Seeds a "destroyed" TerraformStateResource (absent from this generation's
// batch) and a "survivor" (present in the batch, so the upsert refreshes its
// generation_id) both under an OLD generation_id, then runs one
// reconciliation generation. The destroyed resource's retract-sweep statement
// MUST delete it.
func TestTfstateResourceRetractDispatchLive(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the tfstate dispatch-order tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		scopeID      = "tf-scope-5680-retract-dispatch"
		oldGen       = "tf-generation-5680-old"
		newGen       = "tf-generation-5680-new"
		destroyedUID = "tf-resource-5680-destroyed"
		survivorUID  = "tf-resource-5680-survivor"
	)

	exec, writer := openDeltaLiveBackend(ctx, t)

	cleanup := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		for _, uid := range []string{destroyedUID, survivorUID} {
			for _, label := range []string{"TerraformStateResource", "TerraformResource"} {
				if err := exec.Execute(cleanCtx, cypher.Statement{
					Cypher:     "MATCH (r:" + label + " {uid: $uid}) DETACH DELETE r",
					Parameters: map[string]any{"uid": uid},
				}); err != nil {
					t.Errorf("cleanup %s uid=%s: %v", label, uid, err)
				}
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	// Seed both resources under the CURRENT label with an OLD generation_id,
	// as if a prior generation wrote them both.
	seedCypher := `MERGE (r:TerraformStateResource {uid: $uid})
SET r.address = $address,
    r.evidence_source = 'projector/tfstate',
    r.scope_id = $scope_id,
    r.generation_id = $generation_id`
	for _, seed := range []struct{ uid, address string }{
		{destroyedUID, "aws_instance.destroyed_5680"},
		{survivorUID, "aws_instance.survivor_5680"},
	} {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: seedCypher,
			Parameters: map[string]any{
				"uid":           seed.uid,
				"address":       seed.address,
				"scope_id":      scopeID,
				"generation_id": oldGen,
			},
		}); err != nil {
			t.Fatalf("seed uid=%s: %v", seed.uid, err)
		}
	}

	before, err := exec.count(
		ctx,
		`MATCH (r:TerraformStateResource) WHERE r.uid IN $uids RETURN count(r)`,
		map[string]any{"uids": []string{destroyedUID, survivorUID}},
	)
	if err != nil {
		t.Fatalf("verify seed: %v", err)
	}
	if before != 2 {
		t.Fatalf("seed verification: got %d TerraformStateResource nodes, want 2", before)
	}

	// Run one reconciliation generation. Only the survivor reappears in this
	// batch -- the destroyed resource's absence is exactly what should
	// trigger terraformStateResourceRetractStatements' generation-gated
	// DETACH DELETE. ResourceType is deliberately NOT in
	// terraformAttributePromotionAllowlist so the writer skips the
	// attribute-remove phase, isolating this test to the resource-sweep
	// retract statement under test.
	mat := projector.CanonicalMaterialization{
		ScopeID:      scopeID,
		GenerationID: newGen,
		TerraformStateResources: []projector.TerraformStateResourceRow{{
			UID:              survivorUID,
			Address:          "aws_instance.survivor_5680",
			Mode:             "managed",
			ResourceType:     "tf_dispatch_test_resource_5680",
			Name:             "survivor_5680",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
		}},
	}
	if err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("writer.Write: %v", err)
	}

	// ASSERTION 1 (the #5680 defect-1 proof): the destroyed resource must be
	// GONE. Before the fix, its retract statement ran bundled into
	// ge.ExecuteGroup and silently under-applied -- the node survived.
	destroyedCount, err := exec.count(
		ctx,
		`MATCH (r:TerraformStateResource {uid: $uid}) RETURN count(r)`,
		map[string]any{"uid": destroyedUID},
	)
	if err != nil {
		t.Fatalf("count destroyed resource: %v", err)
	}
	if destroyedCount != 0 {
		t.Fatalf(
			"STALE RESOURCE SURVIVED (#5680 defect 1): destroyed TerraformStateResource uid=%s still exists after a reconciliation generation that did not include it -- the retract-sweep statement was dispatched through ge.ExecuteGroup and silently under-applied on NornicDB v1.1.11",
			destroyedUID,
		)
	}

	// ASSERTION 2 (sanity): the survivor must still exist with its
	// generation_id refreshed to the new generation, proving the mixed group
	// still upserted correctly around the fixed retract dispatch.
	survivorRows, err := exec.Run(
		ctx,
		`MATCH (r:TerraformStateResource {uid: $uid}) RETURN r.generation_id AS generation_id`,
		map[string]any{"uid": survivorUID},
	)
	if err != nil {
		t.Fatalf("read survivor: %v", err)
	}
	if len(survivorRows) != 1 {
		t.Fatalf("survivor resource missing after write: got %d rows, want 1", len(survivorRows))
	}
	if got, want := survivorRows[0]["generation_id"], newGen; got != want {
		t.Fatalf("survivor generation_id = %#v, want %q", got, want)
	}
}

// TestTfstateMatchesStateEdgeRetractDispatchLive proves #5680 defect 2: every
// Drain-marked statement was hoisted to run BEFORE any upsert in the phase,
// regardless of its emitted position. The tfstate MATCHES_STATE edge retract
// (tfstate_state_match_edge_retract.go) is Drain-marked and its predicate
// requires `s.generation_id = $generation_id`, but that property is only
// refreshed by the resource-upsert statement buildTerraformStateStatements
// emits BEFORE it. Hoisting the retract ahead of that upsert made the
// predicate match zero rows every cycle -- a silent no-op.
//
// Seeds a TerraformStateResource under an OLD generation_id plus a stale
// MATCHES_STATE edge from a TerraformResource config node, also stamped with
// the OLD generation_id. Runs one reconciliation generation that includes the
// state resource's uid (so the upsert refreshes its generation_id to NEW).
// No ownership/config-match resolver is wired, so
// terraformStateMatchesConfigEdgeStatements' own MERGE never fires this
// cycle (OwningRepoID stays empty) -- isolating this test to the retract
// statement alone: it must delete the pre-existing stale edge because, once
// the resource upsert has run, `s.generation_id = $generation_id` now holds
// and `e.generation_id <> $generation_id` still holds for the untouched edge.
func TestTfstateMatchesStateEdgeRetractDispatchLive(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the tfstate dispatch-order tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		scopeID  = "tf-scope-5680-edge-retract-dispatch"
		oldGen   = "tf-generation-5680-edge-old"
		newGen   = "tf-generation-5680-edge-new"
		stateUID = "tf-resource-5680-edge-state"
		configID = "tf-resource-5680-edge-config"
	)

	exec, writer := openDeltaLiveBackend(ctx, t)

	cleanup := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		statements := []cypher.Statement{
			{Cypher: `MATCH (r:TerraformStateResource {uid: $uid}) DETACH DELETE r`, Parameters: map[string]any{"uid": stateUID}},
			{Cypher: `MATCH (c:TerraformResource {uid: $uid}) DETACH DELETE c`, Parameters: map[string]any{"uid": configID}},
		}
		for _, stmt := range statements {
			if err := exec.Execute(cleanCtx, stmt); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	// Seed the state resource under an OLD generation_id -- this generation's
	// upsert (part of the SAME mat below) must refresh it to newGen before
	// the retract predicate can match.
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher: `MERGE (r:TerraformStateResource {uid: $uid})
SET r.address = $address,
    r.evidence_source = 'projector/tfstate',
    r.scope_id = $scope_id,
    r.generation_id = $generation_id`,
		Parameters: map[string]any{
			"uid":           stateUID,
			"address":       "aws_instance.edge_5680",
			"scope_id":      scopeID,
			"generation_id": oldGen,
		},
	}); err != nil {
		t.Fatalf("seed state resource: %v", err)
	}

	// Seed a minimal config-side TerraformResource node to anchor the stale
	// edge's other endpoint.
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MERGE (c:TerraformResource {uid: $uid}) SET c.name = 'edge_5680_config'`,
		Parameters: map[string]any{"uid": configID},
	}); err != nil {
		t.Fatalf("seed config resource: %v", err)
	}

	// Seed the stale MATCHES_STATE edge itself, stamped with the OLD
	// generation_id -- this is exactly the shape
	// canonicalTerraformStateMatchesConfigEdgeRetractCypher targets: an edge
	// whose generation_id no longer matches the current cycle, pointing at a
	// state resource that DOES get refreshed this cycle.
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher: `MATCH (c:TerraformResource {uid: $config_uid})
MATCH (s:TerraformStateResource {uid: $state_uid})
MERGE (c)-[e:MATCHES_STATE]->(s)
SET e.evidence_source = 'projector/tfstate',
    e.generation_id = $generation_id`,
		Parameters: map[string]any{
			"config_uid":    configID,
			"state_uid":     stateUID,
			"generation_id": oldGen,
		},
	}); err != nil {
		t.Fatalf("seed stale MATCHES_STATE edge: %v", err)
	}

	edgeBefore, err := exec.count(
		ctx,
		`MATCH (:TerraformResource {uid: $config_uid})-[e:MATCHES_STATE]->(:TerraformStateResource {uid: $state_uid}) RETURN count(e)`,
		map[string]any{"config_uid": configID, "state_uid": stateUID},
	)
	if err != nil {
		t.Fatalf("verify stale edge seed: %v", err)
	}
	if edgeBefore != 1 {
		t.Fatalf("stale-edge seed verification: got %d MATCHES_STATE edges, want 1", edgeBefore)
	}

	// Run one reconciliation generation that refreshes the state resource
	// (its uid is in this batch) but wires no ownership/config-match
	// resolver, so no new MATCHES_STATE edge is written this cycle -- the
	// only thing that can change the edge count is the retract statement.
	mat := projector.CanonicalMaterialization{
		ScopeID:      scopeID,
		GenerationID: newGen,
		TerraformStateResources: []projector.TerraformStateResourceRow{{
			UID:              stateUID,
			Address:          "aws_instance.edge_5680",
			Mode:             "managed",
			ResourceType:     "tf_dispatch_test_resource_5680",
			Name:             "edge_5680",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
		}},
	}
	if err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("writer.Write: %v", err)
	}

	// ASSERTION (the #5680 defect-2 proof): the stale edge must be GONE.
	// Before the fix, the Drain-marked retract ran hoisted BEFORE the
	// resource upsert above, so `s.generation_id = $generation_id` matched
	// zero rows (the state node still carried oldGen at that moment) and the
	// edge silently survived.
	edgeAfter, err := exec.count(
		ctx,
		`MATCH (:TerraformResource {uid: $config_uid})-[e:MATCHES_STATE]->(:TerraformStateResource {uid: $state_uid}) RETURN count(e)`,
		map[string]any{"config_uid": configID, "state_uid": stateUID},
	)
	if err != nil {
		t.Fatalf("count MATCHES_STATE edge after write: %v", err)
	}
	if edgeAfter != 0 {
		t.Fatalf(
			"STALE MATCHES_STATE EDGE SURVIVED (#5680 defect 2): the Drain-marked retract ran hoisted before the resource upsert refreshed s.generation_id, so its predicate matched zero rows every cycle",
		)
	}

	// Sanity: the state resource itself must have been refreshed to newGen.
	stateRows, err := exec.Run(
		ctx,
		`MATCH (r:TerraformStateResource {uid: $uid}) RETURN r.generation_id AS generation_id`,
		map[string]any{"uid": stateUID},
	)
	if err != nil {
		t.Fatalf("read state resource: %v", err)
	}
	if len(stateRows) != 1 {
		t.Fatalf("state resource missing after write: got %d rows, want 1", len(stateRows))
	}
	if got, want := stateRows[0]["generation_id"], newGen; got != want {
		t.Fatalf("state resource generation_id = %#v, want %q", got, want)
	}
}
