// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// SECRETS_IAM_USES_SERVICE_ACCOUNT edge retract coverage (C-14 #4367
// retract-depth backfill).
//
// Governance (ADR #1314): the secrets/IAM graph reducer PROJECTION is gated
// OFF by default via ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED, and
// the B-12 golden-corpus snapshot asserts zero secrets/IAM counts for that
// reason. That flag gates only cmd/reducer's own wiring
// (go/cmd/reducer/secrets_iam_graph_wiring.go:
// secretsIAMGraphProjectionWriter returns a nil writer, so
// DomainSecretsIAMGraphProjection never registers, unless the flag is set) --
// it does not gate the cypher.SecretsIAMGraphWriter type itself, which has no
// awareness of the flag or of cmd/reducer. This test drives
// cypher.NewSecretsIAMGraphWriter directly (the same writer-level pattern as
// the pre-existing ADR #1314 Section 11 conformance proof,
// go/internal/storage/cypher/secrets_iam_graph_live_test.go), never touches
// cmd/reducer's intent dispatch or domain registry, and asserts no flag or
// default changes. This is NOT production activation.
//
// SecretsIAMGraphWriter.RetractScope already dispatches every retract
// statement through dispatchSequential (plain Execute, never ExecuteGroup) --
// unlike the sibling writers audited for #4367 and tracked in #5152, this one
// was correct from its original commit, so no production fix is needed here.
// This test is the missing replay-coverage-manifest proof, not a bug fix.
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
	secretsIAMEdgeMarker = "replay-secrets-iam-edge"

	secretsIAMEdgeEvidenceSource = "reducer/secrets-iam-graph"
	secretsIAMWorkloadIn         = "replay-secrets-iam-edge:workload-in"
	secretsIAMWorkloadOut        = "replay-secrets-iam-edge:workload-out"
	secretsIAMServiceAccountIn   = "replay-secrets-iam-edge:service-account-in"
	secretsIAMServiceAccountOut  = "replay-secrets-iam-edge:service-account-out"
	secretsIAMScopeIn            = "replay-secrets-iam-edge:scope-in"
	secretsIAMScopeOut           = "replay-secrets-iam-edge:scope-out"
)

// TestReducerSecretsIAMEdgeRetractGraphTruth proves the
// SECRETS_IAM_USES_SERVICE_ACCOUNT edge (and its owning SecretsIAMServiceAccount
// node) write and scoped-retract correctly on a real NornicDB, driving the
// writer directly (never cmd/reducer, never the gated projection flag).
func TestReducerSecretsIAMEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the secrets/IAM edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupSecretsIAMEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupSecretsIAMEdgeScope(cleanCtx, t, exec)
	})

	seedSecretsIAMEdgeNodes(ctx, t, exec)

	writer := cypher.NewSecretsIAMGraphWriter(exec, 0)

	// --- Write in-scope and out-of-scope ServiceAccount nodes + edges. ---
	if err := writer.WriteServiceAccountNodes(ctx, []map[string]any{{
		"uid": secretsIAMServiceAccountIn, "scope_id": secretsIAMScopeIn,
		"generation_id": iamEdgeGenerationID, "evidence_source": secretsIAMEdgeEvidenceSource, "confidence": "exact",
	}}); err != nil {
		t.Fatalf("WriteServiceAccountNodes(in): %v", err)
	}
	if err := writer.WriteServiceAccountNodes(ctx, []map[string]any{{
		"uid": secretsIAMServiceAccountOut, "scope_id": secretsIAMScopeOut,
		"generation_id": iamEdgeGenerationID, "evidence_source": secretsIAMEdgeEvidenceSource, "confidence": "exact",
	}}); err != nil {
		t.Fatalf("WriteServiceAccountNodes(out): %v", err)
	}
	if err := writer.WriteUsesServiceAccountEdges(ctx, []map[string]any{{
		"workload_uid": secretsIAMWorkloadIn, "service_account_uid": secretsIAMServiceAccountIn,
		"scope_id": secretsIAMScopeIn, "generation_id": iamEdgeGenerationID,
		"evidence_source": secretsIAMEdgeEvidenceSource, "confidence": "exact", "evidence_fact_ids": []string{"f1"},
	}}); err != nil {
		t.Fatalf("WriteUsesServiceAccountEdges(in): %v", err)
	}
	if err := writer.WriteUsesServiceAccountEdges(ctx, []map[string]any{{
		"workload_uid": secretsIAMWorkloadOut, "service_account_uid": secretsIAMServiceAccountOut,
		"scope_id": secretsIAMScopeOut, "generation_id": iamEdgeGenerationID,
		"evidence_source": secretsIAMEdgeEvidenceSource, "confidence": "exact", "evidence_fact_ids": []string{"f1"},
	}}); err != nil {
		t.Fatalf("WriteUsesServiceAccountEdges(out): %v", err)
	}

	edgeQ := "MATCH (:KubernetesWorkload {uid: $w})-[r:SECRETS_IAM_USES_SERVICE_ACCOUNT]->(:SecretsIAMServiceAccount {uid: $s}) RETURN count(r)"
	nodeQ := "MATCH (n:SecretsIAMServiceAccount {uid: $u}) RETURN count(n)"

	inEdge := map[string]any{"w": secretsIAMWorkloadIn, "s": secretsIAMServiceAccountIn}
	outEdge := map[string]any{"w": secretsIAMWorkloadOut, "s": secretsIAMServiceAccountOut}

	assertEdgeCount(ctx, t, exec, edgeQ, inEdge, 1, "write: in-scope SECRETS_IAM_USES_SERVICE_ACCOUNT present")
	assertEdgeCount(ctx, t, exec, edgeQ, outEdge, 1, "write: out-of-scope SECRETS_IAM_USES_SERVICE_ACCOUNT present")
	assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": secretsIAMServiceAccountIn}, 1, "write: in-scope ServiceAccount node present")
	assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": secretsIAMServiceAccountOut}, 1, "write: out-of-scope ServiceAccount node present")

	// --- Retract only the in-scope scope_id. ---
	if err := writer.RetractScope(ctx, []string{secretsIAMScopeIn}, secretsIAMEdgeEvidenceSource); err != nil {
		t.Fatalf("RetractScope: %v", err)
	}

	assertEdgeCount(ctx, t, exec, edgeQ, inEdge, 0, "retract: in-scope SECRETS_IAM_USES_SERVICE_ACCOUNT gone")
	assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": secretsIAMServiceAccountIn}, 0, "retract: in-scope ServiceAccount node gone")

	// Scoped retract, not a wipe: out-of-scope edge and node survive.
	assertEdgeCount(ctx, t, exec, edgeQ, outEdge, 1, "retract: out-of-scope SECRETS_IAM_USES_SERVICE_ACCOUNT survives")
	assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": secretsIAMServiceAccountOut}, 1, "retract: out-of-scope ServiceAccount node survives")

	// The retained KubernetesWorkload endpoints always survive an edge/node retract.
	for _, uid := range []string{secretsIAMWorkloadIn, secretsIAMWorkloadOut} {
		assertEdgeCount(ctx, t, exec, "MATCH (n:KubernetesWorkload {uid: $u}) RETURN count(n)", map[string]any{"u": uid}, 1, "workload node survives: "+uid)
	}
}

// seedSecretsIAMEdgeNodes creates the retained KubernetesWorkload endpoints
// the edge write template MATCHes. The SecretsIAMServiceAccount nodes are
// MERGEd by WriteServiceAccountNodes itself, not seeded here.
func seedSecretsIAMEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE
       (:KubernetesWorkload {uid: $wIn, marker: $marker}),
       (:KubernetesWorkload {uid: $wOut, marker: $marker})`,
		Parameters: map[string]any{
			"wIn": secretsIAMWorkloadIn, "wOut": secretsIAMWorkloadOut,
			"marker": secretsIAMEdgeMarker,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed secrets/IAM edge nodes: %v", err)
	}
}

// cleanupSecretsIAMEdgeScope removes every node this test creates, including
// the write-MERGEd SecretsIAMServiceAccount nodes.
func cleanupSecretsIAMEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, stmt := range []cypher.Statement{
		{
			Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
			Parameters: map[string]any{"marker": secretsIAMEdgeMarker},
		},
		{
			Cypher: `MATCH (a:SecretsIAMServiceAccount) WHERE a.uid IN $uids DETACH DELETE a`,
			Parameters: map[string]any{
				"uids": []string{secretsIAMServiceAccountIn, secretsIAMServiceAccountOut},
			},
		},
	} {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup secrets/IAM edge scope: %v", err)
		}
	}
}
