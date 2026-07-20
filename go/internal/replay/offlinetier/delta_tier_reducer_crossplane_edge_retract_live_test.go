// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Crossplane Claim -> XRD SATISFIED_BY edge retract coverage (issue #5347,
// replay-depth taxonomy gap specs/replay-depth-requirements.v1.yaml
// retractable_edge_types:SATISFIED_BY).
//
// SATISFIED_BY was added to edgetype.RetractableEdgeTypes when
// CrossplaneSatisfiedByEdgeWriter landed, but no delta_tombstone replay
// scenario proved the retract Cypher actually deletes on a real backend. This
// test drives the REAL production writer
// (cypher.NewCrossplaneSatisfiedByEdgeWriter) against liveExecutor, which
// implements GroupExecutor exactly like production's reducerNeo4jExecutor, so
// a retract that regressed to ExecuteGroup (the NornicDB v1.1.11 managed-
// transaction-DELETE under-apply pitfall; see
// docs/public/reference/nornicdb-pitfalls.md) would reproduce here.
//
// Two scenarios exercise the two ways a Claim stops resolving to an XRD:
//   - claim removed: the Claim's K8sResource row disappears (a real delete
//     re-runs the correlation with one fewer candidate); simulated directly by
//     retracting the SATISFIED_BY edge by scope, mirroring what the reducer
//     handler does on the Claim's next generation.
//   - XRD claimNames changed: the XRD's (group, claim_kind) join key no longer
//     matches, so the edge from the XRD's prior generation must retract even
//     though the K8sResource claim node itself is untouched.
//
// Both are asserted by rel.scope_id + evidence_source (the retract Cypher's
// own predicate, matching retractCrossplaneSatisfiedByEdgesCypher) and never
// by endpoint-node properties -- K8sResource and CrossplaneXRD are
// cross-generation canonical nodes with no reducer scope_id of their own, so
// a node-scoped assertion would not prove the edge-scoped retract.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, eshu-diagnostic-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	crossplaneEdgeMarker = "replay-crossplane-edge"

	crossplaneEvidenceSource = "reducer/crossplane-satisfied-by"

	crossplaneClaimGenOneIn  = "replay-crossplane-edge:claim-removed:claim-in"
	crossplaneClaimGenOneOut = "replay-crossplane-edge:claim-removed:claim-out"
	crossplaneXRDGenOneIn    = "replay-crossplane-edge:claim-removed:xrd-in"
	crossplaneXRDGenOneOut   = "replay-crossplane-edge:claim-removed:xrd-out"
	crossplaneScopeClaimIn   = "replay-crossplane-edge:claim-removed:scope-in"
	crossplaneScopeClaimOut  = "replay-crossplane-edge:claim-removed:scope-out"

	crossplaneClaimGenTwoIn  = "replay-crossplane-edge:xrd-relabeled:claim-in"
	crossplaneClaimGenTwoOut = "replay-crossplane-edge:xrd-relabeled:claim-out"
	crossplaneXRDGenTwoIn    = "replay-crossplane-edge:xrd-relabeled:xrd-in"
	crossplaneXRDGenTwoOut   = "replay-crossplane-edge:xrd-relabeled:xrd-out"
	crossplaneScopeXRDIn     = "replay-crossplane-edge:xrd-relabeled:scope-in"
	crossplaneScopeXRDOut    = "replay-crossplane-edge:xrd-relabeled:scope-out"

	crossplaneEdgeGenerationID = "gen-1"
)

// TestReducerCrossplaneSatisfiedByEdgeRetractGraphTruth proves the
// SATISFIED_BY retract path deletes only the in-scope edges on a real
// NornicDB, survives an out-of-scope control, never dispatches the retract
// through ExecuteGroup, and never deletes the K8sResource/CrossplaneXRD
// endpoint nodes. Two independently-scoped pairs cover the two delta
// triggers: a claim removed from its repo, and an XRD whose claimNames
// changed (both surface identically to the writer -- a fresh generation's
// RetractCrossplaneSatisfiedByEdges call for the scope, followed by a write
// that omits the no-longer-matching row).
func TestReducerCrossplaneSatisfiedByEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the Crossplane SATISFIED_BY retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupCrossplaneEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupCrossplaneEdgeScope(cleanCtx, t, exec)
	})

	seedCrossplaneEdgeNodes(ctx, t, exec)

	writer := cypher.NewCrossplaneSatisfiedByEdgeWriter(exec, 0)

	// --- Write in-scope and out-of-scope rows for both scenarios, one call
	// per scope (scope_id is stamped per-call onto every row in the batch). ---
	writeCrossplaneEdge := func(claimUID, xrdUID, scopeID string) {
		t.Helper()
		if err := writer.WriteCrossplaneSatisfiedByEdges(ctx, []map[string]any{{
			"claim_uid":       claimUID,
			"xrd_uid":         xrdUID,
			"resolution_mode": "group_claim_kind",
			"claim_group":     "db.acme.internal",
			"claim_kind":      "AcmeDatabase",
		}}, scopeID, crossplaneEdgeGenerationID, crossplaneEvidenceSource); err != nil {
			t.Fatalf("WriteCrossplaneSatisfiedByEdges(%s, %s, %s): %v", claimUID, xrdUID, scopeID, err)
		}
	}
	writeCrossplaneEdge(crossplaneClaimGenOneIn, crossplaneXRDGenOneIn, crossplaneScopeClaimIn)
	writeCrossplaneEdge(crossplaneClaimGenOneOut, crossplaneXRDGenOneOut, crossplaneScopeClaimOut)
	writeCrossplaneEdge(crossplaneClaimGenTwoIn, crossplaneXRDGenTwoIn, crossplaneScopeXRDIn)
	writeCrossplaneEdge(crossplaneClaimGenTwoOut, crossplaneXRDGenTwoOut, crossplaneScopeXRDOut)

	edgeQ := "MATCH (:K8sResource {uid: $c})-[r:SATISFIED_BY]->(:CrossplaneXRD {uid: $x}) RETURN count(r)"

	claimInParams := map[string]any{"c": crossplaneClaimGenOneIn, "x": crossplaneXRDGenOneIn}
	claimOutParams := map[string]any{"c": crossplaneClaimGenOneOut, "x": crossplaneXRDGenOneOut}
	xrdInParams := map[string]any{"c": crossplaneClaimGenTwoIn, "x": crossplaneXRDGenTwoIn}
	xrdOutParams := map[string]any{"c": crossplaneClaimGenTwoOut, "x": crossplaneXRDGenTwoOut}

	assertEdgeCount(ctx, t, exec, edgeQ, claimInParams, 1, "write: in-scope SATISFIED_BY present (claim-removed scenario)")
	assertEdgeCount(ctx, t, exec, edgeQ, claimOutParams, 1, "write: out-of-scope SATISFIED_BY present (claim-removed scenario)")
	assertEdgeCount(ctx, t, exec, edgeQ, xrdInParams, 1, "write: in-scope SATISFIED_BY present (xrd-relabeled scenario)")
	assertEdgeCount(ctx, t, exec, edgeQ, xrdOutParams, 1, "write: out-of-scope SATISFIED_BY present (xrd-relabeled scenario)")

	// --- Retract only the in-scope scope_id for each scenario -- mirroring
	// the reducer handler's next-generation RetractCrossplaneSatisfiedByEdges
	// call, whether the Claim disappeared or the XRD's claimNames drifted. ---
	if err := writer.RetractCrossplaneSatisfiedByEdges(ctx, []string{crossplaneScopeClaimIn}, crossplaneEdgeGenerationID, crossplaneEvidenceSource); err != nil {
		t.Fatalf("RetractCrossplaneSatisfiedByEdges(claim-removed): %v", err)
	}
	if err := writer.RetractCrossplaneSatisfiedByEdges(ctx, []string{crossplaneScopeXRDIn}, crossplaneEdgeGenerationID, crossplaneEvidenceSource); err != nil {
		t.Fatalf("RetractCrossplaneSatisfiedByEdges(xrd-relabeled): %v", err)
	}

	assertEdgeCount(ctx, t, exec, edgeQ, claimInParams, 0, "retract: in-scope SATISFIED_BY gone (claim-removed scenario)")
	assertEdgeCount(ctx, t, exec, edgeQ, xrdInParams, 0, "retract: in-scope SATISFIED_BY gone (xrd-relabeled scenario)")

	// Scoped retracts, not wipes: out-of-scope edges survive.
	assertEdgeCount(ctx, t, exec, edgeQ, claimOutParams, 1, "retract: out-of-scope SATISFIED_BY survives (claim-removed scenario)")
	assertEdgeCount(ctx, t, exec, edgeQ, xrdOutParams, 1, "retract: out-of-scope SATISFIED_BY survives (xrd-relabeled scenario)")

	// Endpoint nodes always survive an edge retract: K8sResource and
	// CrossplaneXRD are cross-generation canonical nodes the retract Cypher
	// never touches (rel.scope_id lives on the edge, not either endpoint).
	for _, q := range []struct {
		cypherText string
		key        string
	}{
		{"MATCH (n:K8sResource {uid: $u}) RETURN count(n)", crossplaneClaimGenOneIn},
		{"MATCH (n:CrossplaneXRD {uid: $u}) RETURN count(n)", crossplaneXRDGenOneIn},
		{"MATCH (n:K8sResource {uid: $u}) RETURN count(n)", crossplaneClaimGenTwoIn},
		{"MATCH (n:CrossplaneXRD {uid: $u}) RETURN count(n)", crossplaneXRDGenTwoIn},
	} {
		assertEdgeCount(ctx, t, exec, q.cypherText, map[string]any{"u": q.key}, 1, "node survives: "+q.key)
	}
}

// seedCrossplaneEdgeNodes creates every K8sResource/CrossplaneXRD endpoint
// node the write template MATCHes for both scenarios.
func seedCrossplaneEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE
       (:K8sResource {uid: $claimOneIn, marker: $marker}),
       (:K8sResource {uid: $claimOneOut, marker: $marker}),
       (:CrossplaneXRD {uid: $xrdOneIn, marker: $marker}),
       (:CrossplaneXRD {uid: $xrdOneOut, marker: $marker}),
       (:K8sResource {uid: $claimTwoIn, marker: $marker}),
       (:K8sResource {uid: $claimTwoOut, marker: $marker}),
       (:CrossplaneXRD {uid: $xrdTwoIn, marker: $marker}),
       (:CrossplaneXRD {uid: $xrdTwoOut, marker: $marker})`,
		Parameters: map[string]any{
			"claimOneIn": crossplaneClaimGenOneIn, "claimOneOut": crossplaneClaimGenOneOut,
			"xrdOneIn": crossplaneXRDGenOneIn, "xrdOneOut": crossplaneXRDGenOneOut,
			"claimTwoIn": crossplaneClaimGenTwoIn, "claimTwoOut": crossplaneClaimGenTwoOut,
			"xrdTwoIn": crossplaneXRDGenTwoIn, "xrdTwoOut": crossplaneXRDGenTwoOut,
			"marker": crossplaneEdgeMarker,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed crossplane-edge nodes: %v", err)
	}
}

// cleanupCrossplaneEdgeScope removes every node this test creates.
func cleanupCrossplaneEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
		Parameters: map[string]any{"marker": crossplaneEdgeMarker},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("cleanup crossplane-edge scope: %v", err)
	}
}
