// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestTerraformStateResourceMigrationLive is the real-backend counterpart to
// terraformStateResourceMigrationCypher's doc comment (#5443): it runs the
// actual production Cypher (buildTerraformStateStatements) through a real
// Bolt-connected NornicDB (or Neo4j-compatible) backend, proving the
// pre-#5443 -> post-#5443 label migration and the paired retraction actually
// execute as designed, not merely that they parse. Required by this
// repository's Prove-The-Theory-First / Cypher rigor discipline: a Cypher
// shape that parses is not a Cypher shape that executes (see #5441 review
// round 9's fused REMOVE, which passed an in-memory fixture while corrupting
// data on the real backend).
//
// Covers two resources seeded under the LEGACY TerraformResource label with
// evidence_source = 'projector/tfstate', mirroring what the pre-#5443 writer
// produced:
//
//   - "still present": its uid reappears in this generation's batch, so
//     migration must relabel it to TerraformStateResource (not delete it) and
//     the upsert must refresh its properties under the new label.
//   - "removed from state": its uid does NOT reappear in this generation's
//     batch (the resource was destroyed, config removed, or simply not seen
//     again), so migration never touches it and the legacy-label retraction
//     statement must DETACH DELETE it -- it must not survive under either
//     label.
//
// IDENTITY, not just properties: a review finding on this test proved a
// property-only assertion ("the still-present resource's address/
// generation_id/evidence_source look right afterward") is a false-green for
// the bug this test claims to guard. Relabel-then-DELETE-then-recreate
// produces IDENTICAL final properties to an in-place SET+REMOVE relabel --
// both end with a TerraformStateResource node carrying the same uid and the
// same refreshed fields. This test previously could not have failed even
// under the P0 retract-before-upsert bug this repository shipped and fixed
// (see terraformStateResourceRetractStatements's doc comment): under that
// bug, migration relabeled the still-present node in place, but the
// retraction statement that ran immediately after it (before the upsert
// refreshed generation_id) DETACH DELETEd that very node because it still
// carried the OLD generation_id -- and the upsert then MERGEd a BRAND NEW
// node under the same uid. The final property read looked identical either
// way. To make this distinguishable, this test attaches a relationship to
// the still-present node BEFORE running the writer and asserts that
// relationship survives afterward: DETACH DELETE unconditionally severs
// every relationship on the node it removes, and a MERGE-by-uid on a
// brand-new node never recreates an edge nothing asked it to write. A
// surviving relationship is direct proof of node-identity preservation
// across the whole statement sequence, not merely equal property values.
//
// Opt-in, matching every other _live_test.go in this package: set
// ESHU_CYPHER_BOLT_DSN (and optionally ESHU_CYPHER_BOLT_DATABASE) to run it
// against a running graph backend. Skipped otherwise.
func TestTerraformStateResourceMigrationLive(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })

	const (
		stillPresentUID  = "tf-resource-5443-live-still-present"
		removedUID       = "tf-resource-5443-live-removed"
		scopeID          = "tf-scope-5443-live"
		identityMarkerID = "tf-resource-5443-live-identity-marker"
	)
	ctx := context.Background()
	t.Cleanup(func() {
		if err := boltWriteStatement(
			context.Background(),
			runner,
			"MATCH (m:TestIdentityMarker {marker_id: $marker_id}) DETACH DELETE m",
			map[string]any{"marker_id": identityMarkerID},
		); err != nil {
			t.Errorf("cleanup identity marker node: %v", err)
		}
		for _, uid := range []string{stillPresentUID, removedUID} {
			for _, label := range []string{"TerraformResource", "TerraformStateResource"} {
				if err := boltWriteStatement(
					context.Background(),
					runner,
					"MATCH (r:"+label+" {uid: $uid}) DETACH DELETE r",
					map[string]any{"uid": uid},
				); err != nil {
					t.Errorf("cleanup live %s uid=%s: %v", label, uid, err)
				}
			}
		}
	})

	// Seed both resources under the LEGACY label, exactly as the pre-#5443
	// writer would have left them: evidence_source = 'projector/tfstate',
	// an OLD generation_id, and the same scope_id this test's materialization
	// will use.
	seedLegacyCypher := `MERGE (r:TerraformResource {uid: $uid})
SET r.address = $address,
    r.evidence_source = 'projector/tfstate',
    r.scope_id = $scope_id,
    r.generation_id = 'tf-generation-5443-live-old'`
	for _, seed := range []struct{ uid, address string }{
		{stillPresentUID, "aws_instance.still_present"},
		{removedUID, "aws_instance.removed"},
	} {
		if err := boltWriteStatement(ctx, runner, seedLegacyCypher, map[string]any{
			"uid":      seed.uid,
			"address":  seed.address,
			"scope_id": scopeID,
		}); err != nil {
			t.Fatalf("seed legacy TerraformResource uid=%s: %v", seed.uid, err)
		}
	}

	// Attach an identity-probe relationship to the still-present node BEFORE
	// running the writer -- see the identity-preservation doc above for why
	// this, not a property read, is the assertion that actually proves the
	// node was relabeled in place rather than deleted and recreated.
	//
	// Two statements, not a single MERGE ... WITH ... MATCH ... MERGE chain:
	// an empirical probe against the pinned NornicDB backend showed that
	// chained-clause shape does not execute as written (the text following
	// the first MERGE's closing brace gets absorbed into the marker_id
	// property value instead of starting a new clause). The two-independent-
	// MATCH-clauses-then-MERGE shape below is this repository's own proven
	// precedent for anchoring an edge between two already-existing nodes
	// (canonicalTerraformStateMatchesConfigEdgeCypher in
	// tfstate_state_match_edge.go uses the identical MATCH ... MATCH ...
	// MERGE shape), so the marker node is created in its own statement first.
	if err := boltWriteStatement(ctx, runner, `MERGE (m:TestIdentityMarker {marker_id: $marker_id})`, map[string]any{
		"marker_id": identityMarkerID,
	}); err != nil {
		t.Fatalf("create identity marker node: %v", err)
	}
	attachMarkerCypher := `MATCH (r:TerraformResource {uid: $uid})
MATCH (m:TestIdentityMarker {marker_id: $marker_id})
MERGE (r)-[:TEST_IDENTITY_PROBE]->(m)`
	if err := boltWriteStatement(ctx, runner, attachMarkerCypher, map[string]any{
		"marker_id": identityMarkerID,
		"uid":       stillPresentUID,
	}); err != nil {
		t.Fatalf("attach identity-probe relationship to still-present resource: %v", err)
	}

	// Confirm the seed (including the identity probe) landed as expected
	// before exercising the writer.
	rows, err := runner.runCypher(ctx, `MATCH (r:TerraformResource) WHERE r.uid IN $uids RETURN r.uid AS uid`, map[string]any{
		"uids": []string{stillPresentUID, removedUID},
	})
	if err != nil {
		t.Fatalf("verify seed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("seed verification: got %d legacy TerraformResource nodes, want 2", len(rows))
	}
	probeSeedRows, err := runner.runCypher(
		ctx,
		`MATCH (r:TerraformResource {uid: $uid})-[:TEST_IDENTITY_PROBE]->(m:TestIdentityMarker {marker_id: $marker_id}) RETURN r.uid AS uid`,
		map[string]any{"uid": stillPresentUID, "marker_id": identityMarkerID},
	)
	if err != nil {
		t.Fatalf("verify identity-probe seed: %v", err)
	}
	if len(probeSeedRows) != 1 {
		t.Fatalf("identity-probe seed verification: got %d matching rows, want 1", len(probeSeedRows))
	}

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      scopeID,
		GenerationID: "tf-generation-5443-live-new",
		TerraformStateResources: []projector.TerraformStateResourceRow{{
			UID:              stillPresentUID,
			Address:          "aws_instance.still_present",
			Mode:             "managed",
			ResourceType:     "aws_instance",
			Name:             "still_present",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
		}},
	}

	for _, stmt := range writer.buildTerraformStateStatements(mat) {
		if err := runner.runCypherSingle(ctx, stmt); err != nil {
			t.Fatalf("statement failed: %v\ncypher: %s", err, stmt.Cypher)
		}
	}

	// ASSERTION 1: the still-present resource was relabeled, not deleted --
	// no TerraformResource node with this uid remains.
	legacyStillPresent, err := runner.runCypher(ctx, `MATCH (r:TerraformResource {uid: $uid}) RETURN r.uid AS uid`, map[string]any{"uid": stillPresentUID})
	if err != nil {
		t.Fatalf("read legacy still-present after migration: %v", err)
	}
	if len(legacyStillPresent) != 0 {
		t.Fatalf("still-present resource survived under the legacy TerraformResource label: %#v", legacyStillPresent)
	}

	// ASSERTION 2: it now exists under TerraformStateResource with current
	// properties (the upsert refreshed it after migration relabeled it).
	newStillPresent, err := runner.runCypher(
		ctx,
		`MATCH (r:TerraformStateResource {uid: $uid}) RETURN r.address AS address, r.generation_id AS generation_id, r.evidence_source AS evidence_source`,
		map[string]any{"uid": stillPresentUID},
	)
	if err != nil {
		t.Fatalf("read new-label still-present after migration: %v", err)
	}
	if len(newStillPresent) != 1 {
		t.Fatalf("still-present resource missing under TerraformStateResource after migration: got %d rows, want 1", len(newStillPresent))
	}
	if got, want := newStillPresent[0]["address"], "aws_instance.still_present"; got != want {
		t.Fatalf("relabeled node address = %#v, want %q", got, want)
	}
	if got, want := newStillPresent[0]["generation_id"], "tf-generation-5443-live-new"; got != want {
		t.Fatalf("relabeled node generation_id = %#v, want %q (upsert must refresh it after migration)", got, want)
	}
	if got, want := newStillPresent[0]["evidence_source"], "projector/tfstate"; got != want {
		t.Fatalf("relabeled node evidence_source = %#v, want %q", got, want)
	}

	// ASSERTION 3 (IDENTITY, not just properties): the identity-probe
	// relationship attached to the still-present node BEFORE the writer ran
	// must still exist afterward, now anchored on the NEW label. If the
	// still-present node was deleted (by a mis-ordered retract statement, as
	// this repository's P0 review finding proved happened) and a brand-new
	// node was recreated under the same uid by the upsert, this
	// relationship would be gone: DETACH DELETE severs every relationship on
	// the node it removes, and nothing in this writer ever recreates a
	// TEST_IDENTITY_PROBE edge. ASSERTION 2 above cannot distinguish these
	// two cases; this one can.
	identityProbeAfter, err := runner.runCypher(
		ctx,
		`MATCH (r:TerraformStateResource {uid: $uid})-[:TEST_IDENTITY_PROBE]->(m:TestIdentityMarker {marker_id: $marker_id}) RETURN r.uid AS uid`,
		map[string]any{"uid": stillPresentUID, "marker_id": identityMarkerID},
	)
	if err != nil {
		t.Fatalf("read identity-probe relationship after migration: %v", err)
	}
	if len(identityProbeAfter) != 1 {
		t.Fatalf(
			"IDENTITY PRESERVATION FAILED: still-present resource's pre-attached relationship did not survive under the new label -- got %d matching rows, want 1. This means the node was DELETED AND RECREATED rather than relabeled in place (relabel-then-delete-then-recreate produces identical properties in ASSERTION 2 but severs relationships): %#v",
			len(identityProbeAfter), identityProbeAfter,
		)
	}

	// ASSERTION 4: the removed resource does not survive under EITHER label
	// -- migration never touched it (its uid was absent from this batch), so
	// the legacy-label retraction statement must have DETACH DELETEd it.
	legacyRemoved, err := runner.runCypher(ctx, `MATCH (r:TerraformResource {uid: $uid}) RETURN r.uid AS uid`, map[string]any{"uid": removedUID})
	if err != nil {
		t.Fatalf("read legacy removed after migration: %v", err)
	}
	if len(legacyRemoved) != 0 {
		t.Fatalf("removed resource survived under the legacy TerraformResource label: %#v", legacyRemoved)
	}
	newRemoved, err := runner.runCypher(ctx, `MATCH (r:TerraformStateResource {uid: $uid}) RETURN r.uid AS uid`, map[string]any{"uid": removedUID})
	if err != nil {
		t.Fatalf("read new-label removed after migration: %v", err)
	}
	if len(newRemoved) != 0 {
		t.Fatalf("removed resource unexpectedly exists under TerraformStateResource: %#v", newRemoved)
	}
}
