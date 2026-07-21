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
// Opt-in, matching every other _live_test.go in this package: set
// ESHU_CYPHER_BOLT_DSN (and optionally ESHU_CYPHER_BOLT_DATABASE) to run it
// against a running graph backend. Skipped otherwise.
func TestTerraformStateResourceMigrationLive(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })

	const (
		stillPresentUID = "tf-resource-5443-live-still-present"
		removedUID      = "tf-resource-5443-live-removed"
		scopeID         = "tf-scope-5443-live"
	)
	ctx := context.Background()
	t.Cleanup(func() {
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

	// Confirm the seed landed as expected before exercising the writer.
	rows, err := runner.runCypher(ctx, `MATCH (r:TerraformResource) WHERE r.uid IN $uids RETURN r.uid AS uid`, map[string]any{
		"uids": []string{stillPresentUID, removedUID},
	})
	if err != nil {
		t.Fatalf("verify seed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("seed verification: got %d legacy TerraformResource nodes, want 2", len(rows))
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

	// ASSERTION 3: the removed resource does not survive under EITHER label
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
