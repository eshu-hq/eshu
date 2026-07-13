// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// IAM privilege edge retract coverage (C-14 #4367 retract-depth backfill):
// CAN_ASSUME, CAN_ESCALATE_TO, CAN_PERFORM.
//
// Before this fix, IAMCanAssumeEdgeWriter.RetractIAMCanAssumeEdges,
// IAMEscalationEdgeWriter.RetractIAMEscalationEdges, and
// IAMCanPerformEdgeWriter.RetractIAMCanPerformEdges each dispatched their
// single retract DELETE statement through the shared dispatch() helper, which
// routes through ExecuteGroup (a managed Bolt transaction) whenever the
// executor implements GroupExecutor -- exactly the executor shape cmd/reducer
// wires in production (reducerNeo4jExecutor.ExecuteGroup, unconditional for
// every graph backend including NornicDB). On the pinned NornicDB v1.1.11, a
// DELETE dispatched through ExecuteGroup under-applies even for a single
// statement (see docs/public/reference/nornicdb-pitfalls.md,
// "Node-Label Disjunction" pitfall and its managed-transaction-DELETE
// refinement); the identical statement run as an auto-commit transaction
// (Execute) deletes correctly. The fix adds a dispatchRetract helper per
// writer that routes every retract through sequential Execute, never
// ExecuteGroup -- the same pattern as the five #4367 cloud-correlation
// writers (delta_tier_reducer_cloud_edge_retract_live_test.go) and the nine
// writers tracked in #5152.
//
// The test drives the REAL production writer constructors and methods
// (cypher.NewIAMCanAssumeEdgeWriter, cypher.NewIAMEscalationEdgeWriter,
// cypher.NewIAMCanPerformEdgeWriter) against liveExecutor, which implements
// GroupExecutor exactly like production's reducerNeo4jExecutor, so a retract
// that still routed through ExecuteGroup would reproduce the under-apply
// here. Each of the three edge types is written and retracted with an
// out-of-scope survivor control (same evidence_source, different scope_id)
// plus endpoint node-survival assertions.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	iamEdgeMarker = "replay-iam-edge"

	canAssumeEvidenceSource = "reducer/iam-can-assume"
	canAssumePrincipalIn    = "replay-iam-edge:can-assume:principal-in"
	canAssumeRoleIn         = "replay-iam-edge:can-assume:role-in"
	canAssumePrincipalOut   = "replay-iam-edge:can-assume:principal-out"
	canAssumeRoleOut        = "replay-iam-edge:can-assume:role-out"
	canAssumeScopeIn        = "replay-iam-edge:can-assume:scope-in"
	canAssumeScopeOut       = "replay-iam-edge:can-assume:scope-out"

	escalationEvidenceSource = "reducer/iam-escalation"
	escalationPrincipalIn    = "replay-iam-edge:escalate:principal-in"
	escalationTargetIn       = "replay-iam-edge:escalate:target-in"
	escalationPrincipalOut   = "replay-iam-edge:escalate:principal-out"
	escalationTargetOut      = "replay-iam-edge:escalate:target-out"
	escalationScopeIn        = "replay-iam-edge:escalate:scope-in"
	escalationScopeOut       = "replay-iam-edge:escalate:scope-out"

	canPerformEvidenceSource = "reducer/iam-can-perform"
	canPerformPrincipalIn    = "replay-iam-edge:can-perform:principal-in"
	canPerformResourceIn     = "replay-iam-edge:can-perform:resource-in"
	canPerformPrincipalOut   = "replay-iam-edge:can-perform:principal-out"
	canPerformResourceOut    = "replay-iam-edge:can-perform:resource-out"
	canPerformScopeIn        = "replay-iam-edge:can-perform:scope-in"
	canPerformScopeOut       = "replay-iam-edge:can-perform:scope-out"

	iamEdgeGenerationID = "gen-1"
)

// TestReducerIAMEdgeRetractGraphTruth proves the CAN_ASSUME, CAN_ESCALATE_TO,
// and CAN_PERFORM retract paths delete only the in-scope edges on a real
// NornicDB, and never dispatch through ExecuteGroup.
func TestReducerIAMEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the IAM-edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupIAMEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupIAMEdgeScope(cleanCtx, t, exec)
	})

	seedIAMEdgeNodes(ctx, t, exec)

	canAssumeWriter := cypher.NewIAMCanAssumeEdgeWriter(exec, 0)
	escalationWriter := cypher.NewIAMEscalationEdgeWriter(exec, 0)
	canPerformWriter := cypher.NewIAMCanPerformEdgeWriter(exec, 0)

	// --- Write in-scope and out-of-scope rows, one call per scope. ---
	if err := canAssumeWriter.WriteIAMCanAssumeEdges(ctx, []map[string]any{{
		"principal_uid": canAssumePrincipalIn, "role_uid": canAssumeRoleIn,
		"relationship_type": "CAN_ASSUME", "principal_kind": "role", "resolution_mode": "trust_policy",
	}}, canAssumeScopeIn, iamEdgeGenerationID, canAssumeEvidenceSource); err != nil {
		t.Fatalf("WriteIAMCanAssumeEdges(in): %v", err)
	}
	if err := canAssumeWriter.WriteIAMCanAssumeEdges(ctx, []map[string]any{{
		"principal_uid": canAssumePrincipalOut, "role_uid": canAssumeRoleOut,
		"relationship_type": "CAN_ASSUME", "principal_kind": "role", "resolution_mode": "trust_policy",
	}}, canAssumeScopeOut, iamEdgeGenerationID, canAssumeEvidenceSource); err != nil {
		t.Fatalf("WriteIAMCanAssumeEdges(out): %v", err)
	}

	if err := escalationWriter.WriteIAMEscalationEdges(ctx, []map[string]any{{
		"principal_uid": escalationPrincipalIn, "target_uid": escalationTargetIn,
		"primitives": []string{"iam:PassRole"}, "primitive_count": 1,
	}}, escalationScopeIn, iamEdgeGenerationID, escalationEvidenceSource); err != nil {
		t.Fatalf("WriteIAMEscalationEdges(in): %v", err)
	}
	if err := escalationWriter.WriteIAMEscalationEdges(ctx, []map[string]any{{
		"principal_uid": escalationPrincipalOut, "target_uid": escalationTargetOut,
		"primitives": []string{"iam:PassRole"}, "primitive_count": 1,
	}}, escalationScopeOut, iamEdgeGenerationID, escalationEvidenceSource); err != nil {
		t.Fatalf("WriteIAMEscalationEdges(out): %v", err)
	}

	if err := canPerformWriter.WriteIAMCanPerformEdges(ctx, []map[string]any{{
		"principal_uid": canPerformPrincipalIn, "resource_uid": canPerformResourceIn,
		"actions": []string{"s3:PutObject"}, "action_count": 1,
		"evaluation_scope": "identity", "grant_sources": []string{"identity_policy"},
	}}, canPerformScopeIn, iamEdgeGenerationID, canPerformEvidenceSource); err != nil {
		t.Fatalf("WriteIAMCanPerformEdges(in): %v", err)
	}
	if err := canPerformWriter.WriteIAMCanPerformEdges(ctx, []map[string]any{{
		"principal_uid": canPerformPrincipalOut, "resource_uid": canPerformResourceOut,
		"actions": []string{"s3:PutObject"}, "action_count": 1,
		"evaluation_scope": "identity", "grant_sources": []string{"identity_policy"},
	}}, canPerformScopeOut, iamEdgeGenerationID, canPerformEvidenceSource); err != nil {
		t.Fatalf("WriteIAMCanPerformEdges(out): %v", err)
	}

	canAssumeQ := "MATCH (:CloudResource {uid: $p})-[r:CAN_ASSUME]->(:CloudResource {uid: $ro}) RETURN count(r)"
	escalationQ := "MATCH (:CloudResource {uid: $p})-[r:CAN_ESCALATE_TO]->(:CloudResource {uid: $t}) RETURN count(r)"
	canPerformQ := "MATCH (:CloudResource {uid: $p})-[r:CAN_PERFORM]->(:CloudResource {uid: $re}) RETURN count(r)"

	inCanAssume := map[string]any{"p": canAssumePrincipalIn, "ro": canAssumeRoleIn}
	outCanAssume := map[string]any{"p": canAssumePrincipalOut, "ro": canAssumeRoleOut}
	inEscalation := map[string]any{"p": escalationPrincipalIn, "t": escalationTargetIn}
	outEscalation := map[string]any{"p": escalationPrincipalOut, "t": escalationTargetOut}
	inCanPerform := map[string]any{"p": canPerformPrincipalIn, "re": canPerformResourceIn}
	outCanPerform := map[string]any{"p": canPerformPrincipalOut, "re": canPerformResourceOut}

	assertEdgeCount(ctx, t, exec, canAssumeQ, inCanAssume, 1, "write: in-scope CAN_ASSUME present")
	assertEdgeCount(ctx, t, exec, canAssumeQ, outCanAssume, 1, "write: out-of-scope CAN_ASSUME present")
	assertEdgeCount(ctx, t, exec, escalationQ, inEscalation, 1, "write: in-scope CAN_ESCALATE_TO present")
	assertEdgeCount(ctx, t, exec, escalationQ, outEscalation, 1, "write: out-of-scope CAN_ESCALATE_TO present")
	assertEdgeCount(ctx, t, exec, canPerformQ, inCanPerform, 1, "write: in-scope CAN_PERFORM present")
	assertEdgeCount(ctx, t, exec, canPerformQ, outCanPerform, 1, "write: out-of-scope CAN_PERFORM present")

	// --- Retract only the in-scope scope_id for each domain. ---
	if err := canAssumeWriter.RetractIAMCanAssumeEdges(ctx, []string{canAssumeScopeIn}, iamEdgeGenerationID, canAssumeEvidenceSource); err != nil {
		t.Fatalf("RetractIAMCanAssumeEdges: %v", err)
	}
	if err := escalationWriter.RetractIAMEscalationEdges(ctx, []string{escalationScopeIn}, iamEdgeGenerationID, escalationEvidenceSource); err != nil {
		t.Fatalf("RetractIAMEscalationEdges: %v", err)
	}
	if err := canPerformWriter.RetractIAMCanPerformEdges(ctx, []string{canPerformScopeIn}, iamEdgeGenerationID, canPerformEvidenceSource); err != nil {
		t.Fatalf("RetractIAMCanPerformEdges: %v", err)
	}

	assertEdgeCount(ctx, t, exec, canAssumeQ, inCanAssume, 0, "retract: in-scope CAN_ASSUME gone")
	assertEdgeCount(ctx, t, exec, escalationQ, inEscalation, 0, "retract: in-scope CAN_ESCALATE_TO gone")
	assertEdgeCount(ctx, t, exec, canPerformQ, inCanPerform, 0, "retract: in-scope CAN_PERFORM gone")

	// Scoped retracts, not wipes: out-of-scope edges survive.
	assertEdgeCount(ctx, t, exec, canAssumeQ, outCanAssume, 1, "retract: out-of-scope CAN_ASSUME survives")
	assertEdgeCount(ctx, t, exec, escalationQ, outEscalation, 1, "retract: out-of-scope CAN_ESCALATE_TO survives")
	assertEdgeCount(ctx, t, exec, canPerformQ, outCanPerform, 1, "retract: out-of-scope CAN_PERFORM survives")

	// Endpoint CloudResource nodes always survive an edge retract.
	for _, uid := range []string{
		canAssumePrincipalIn, canAssumeRoleIn,
		escalationPrincipalIn, escalationTargetIn,
		canPerformPrincipalIn, canPerformResourceIn,
	} {
		assertEdgeCount(ctx, t, exec, "MATCH (n:CloudResource {uid: $u}) RETURN count(n)", map[string]any{"u": uid}, 1, "node survives: "+uid)
	}
}

// seedIAMEdgeNodes creates every CloudResource endpoint node the three write
// templates MATCH.
func seedIAMEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE
       (:CloudResource {uid: $caPrincipalIn, marker: $marker}),
       (:CloudResource {uid: $caRoleIn, marker: $marker}),
       (:CloudResource {uid: $caPrincipalOut, marker: $marker}),
       (:CloudResource {uid: $caRoleOut, marker: $marker}),
       (:CloudResource {uid: $escPrincipalIn, marker: $marker}),
       (:CloudResource {uid: $escTargetIn, marker: $marker}),
       (:CloudResource {uid: $escPrincipalOut, marker: $marker}),
       (:CloudResource {uid: $escTargetOut, marker: $marker}),
       (:CloudResource {uid: $cpPrincipalIn, marker: $marker}),
       (:CloudResource {uid: $cpResourceIn, marker: $marker}),
       (:CloudResource {uid: $cpPrincipalOut, marker: $marker}),
       (:CloudResource {uid: $cpResourceOut, marker: $marker})`,
		Parameters: map[string]any{
			"caPrincipalIn": canAssumePrincipalIn, "caRoleIn": canAssumeRoleIn,
			"caPrincipalOut": canAssumePrincipalOut, "caRoleOut": canAssumeRoleOut,
			"escPrincipalIn": escalationPrincipalIn, "escTargetIn": escalationTargetIn,
			"escPrincipalOut": escalationPrincipalOut, "escTargetOut": escalationTargetOut,
			"cpPrincipalIn": canPerformPrincipalIn, "cpResourceIn": canPerformResourceIn,
			"cpPrincipalOut": canPerformPrincipalOut, "cpResourceOut": canPerformResourceOut,
			"marker": iamEdgeMarker,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed IAM-edge nodes: %v", err)
	}
}

// cleanupIAMEdgeScope removes every node this test creates.
func cleanupIAMEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
		Parameters: map[string]any{"marker": iamEdgeMarker},
	}); err != nil {
		t.Fatalf("cleanup IAM-edge scope: %v", err)
	}
}
