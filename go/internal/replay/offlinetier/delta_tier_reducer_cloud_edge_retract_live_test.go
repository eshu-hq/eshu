// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Cloud-correlation edge retract coverage (C-14 #4367 retract-depth backfill):
// RUNS_IMAGE, LOGS_TO, GRANTS_ACCESS_TO, HAS_ROLE.
//
// Before this fix, KubernetesCorrelationEdgeWriter.RetractKubernetesCorrelationEdges,
// S3LogsToEdgeWriter.RetractS3LogsToEdges, S3ExternalPrincipalGrantWriter.
// RetractS3ExternalPrincipalGrants, and IAMInstanceProfileRoleEdgeWriter.
// RetractIAMInstanceProfileRoleEdges each dispatched their single retract
// DELETE statement through the shared dispatch() helper, which routes through
// ExecuteGroup (a managed Bolt transaction) whenever the executor implements
// GroupExecutor -- exactly the executor shape cmd/reducer wires in production
// (reducerNeo4jExecutor.ExecuteGroup, unconditional for every graph backend).
// On the pinned NornicDB v1.1.11, a DELETE dispatched through ExecuteGroup
// under-applies even for a single statement (see
// docs/public/reference/nornicdb-pitfalls.md, "Node-Label Disjunction" pitfall
// and its managed-transaction-DELETE refinement); the identical statement run
// as an auto-commit transaction (Execute) deletes correctly. The fix adds a
// dispatchRetract helper per writer (SecurityGroupReachabilityWriter already
// had one for its ledger-anchored path; this reuses it) that routes every
// retract through sequential Execute, never ExecuteGroup.
//
// The test drives the REAL production writer constructors and methods
// (cypher.NewKubernetesCorrelationEdgeWriter, cypher.NewS3LogsToEdgeWriter,
// cypher.NewS3ExternalPrincipalGrantWriter,
// cypher.NewIAMInstanceProfileRoleEdgeWriter) against liveExecutor, which
// implements GroupExecutor exactly like production's reducerNeo4jExecutor, so
// a retract that still routed through ExecuteGroup would reproduce the
// under-apply here. Each of the four edge types is written and retracted with
// an out-of-scope survivor control (same evidence_source, different scope_id)
// plus endpoint node-survival assertions. GRANTS_ACCESS_TO additionally
// proves its ExternalPrincipal node survives retract by design (global
// identity, never DETACH-deleted by the edge retract).
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
	cloudEdgeMarker = "replay-cloud-edge"

	kcEvidenceSource = "reducer/kubernetes-correlation"
	kcWorkloadIn     = "replay-cloud-edge:k8s:workload-in"
	kcWorkloadOut    = "replay-cloud-edge:k8s:workload-out"
	kcSourceIn       = "replay-cloud-edge:k8s:oci-source-in"
	kcSourceOut      = "replay-cloud-edge:k8s:oci-source-out"
	kcScopeIn        = "replay-cloud-edge:k8s:scope-in"
	kcScopeOut       = "replay-cloud-edge:k8s:scope-out"

	s3LogsEvidenceSource = "reducer/s3-logs-to"
	s3LogsSourceIn       = "replay-cloud-edge:s3logs:source-in"
	s3LogsTargetIn       = "replay-cloud-edge:s3logs:target-in"
	s3LogsSourceOut      = "replay-cloud-edge:s3logs:source-out"
	s3LogsTargetOut      = "replay-cloud-edge:s3logs:target-out"
	s3LogsScopeIn        = "replay-cloud-edge:s3logs:scope-in"
	s3LogsScopeOut       = "replay-cloud-edge:s3logs:scope-out"

	grantEvidenceSource = "reducer/s3-external-principal-grant"
	grantSourceIn       = "replay-cloud-edge:grant:source-in"
	grantSourceOut      = "replay-cloud-edge:grant:source-out"
	grantPrincipalIn    = "replay-cloud-edge:grant:principal-in"
	grantPrincipalOut   = "replay-cloud-edge:grant:principal-out"
	grantScopeIn        = "replay-cloud-edge:grant:scope-in"
	grantScopeOut       = "replay-cloud-edge:grant:scope-out"

	iamRoleEvidenceSource = "reducer/iam-instance-profile-role"
	iamProfileIn          = "replay-cloud-edge:iam:profile-in"
	iamProfileOut         = "replay-cloud-edge:iam:profile-out"
	iamRoleIn             = "replay-cloud-edge:iam:role-in"
	iamRoleOut            = "replay-cloud-edge:iam:role-out"
	iamScopeIn            = "replay-cloud-edge:iam:scope-in"
	iamScopeOut           = "replay-cloud-edge:iam:scope-out"

	cloudEdgeGenerationID = "gen-1"
)

// TestReducerCloudEdgeRetractGraphTruth proves the RUNS_IMAGE, LOGS_TO,
// GRANTS_ACCESS_TO, and HAS_ROLE retract paths delete only the in-scope edges
// on a real NornicDB, and never dispatch through ExecuteGroup.
func TestReducerCloudEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the cloud-edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupCloudEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupCloudEdgeScope(cleanCtx, t, exec)
	})

	seedCloudEdgeNodes(ctx, t, exec)

	kcWriter := cypher.NewKubernetesCorrelationEdgeWriter(exec, 0)
	s3LogsWriter := cypher.NewS3LogsToEdgeWriter(exec, 0)
	grantWriter := cypher.NewS3ExternalPrincipalGrantWriter(exec, 0)
	iamWriter := cypher.NewIAMInstanceProfileRoleEdgeWriter(exec, 0)

	// --- Write in-scope and out-of-scope rows, one call per scope (scope_id is
	// stamped per-call onto every row in the batch). ---
	if err := kcWriter.WriteKubernetesCorrelationEdges(ctx, []map[string]any{{
		"workload_uid": kcWorkloadIn, "source_uid": kcSourceIn, "source_label": "OciImageManifest",
		"resolution_mode": "digest", "image_ref": "registry/repo:in", "source_digest": "sha256:in",
	}}, kcScopeIn, cloudEdgeGenerationID, kcEvidenceSource); err != nil {
		t.Fatalf("WriteKubernetesCorrelationEdges(in): %v", err)
	}
	if err := kcWriter.WriteKubernetesCorrelationEdges(ctx, []map[string]any{{
		"workload_uid": kcWorkloadOut, "source_uid": kcSourceOut, "source_label": "OciImageManifest",
		"resolution_mode": "digest", "image_ref": "registry/repo:out", "source_digest": "sha256:out",
	}}, kcScopeOut, cloudEdgeGenerationID, kcEvidenceSource); err != nil {
		t.Fatalf("WriteKubernetesCorrelationEdges(out): %v", err)
	}

	if err := s3LogsWriter.WriteS3LogsToEdges(ctx, []map[string]any{{
		"source_uid": s3LogsSourceIn, "target_uid": s3LogsTargetIn,
		"relationship_type": "LOGS_TO", "resolution_mode": "name",
	}}, s3LogsScopeIn, cloudEdgeGenerationID, s3LogsEvidenceSource); err != nil {
		t.Fatalf("WriteS3LogsToEdges(in): %v", err)
	}
	if err := s3LogsWriter.WriteS3LogsToEdges(ctx, []map[string]any{{
		"source_uid": s3LogsSourceOut, "target_uid": s3LogsTargetOut,
		"relationship_type": "LOGS_TO", "resolution_mode": "name",
	}}, s3LogsScopeOut, cloudEdgeGenerationID, s3LogsEvidenceSource); err != nil {
		t.Fatalf("WriteS3LogsToEdges(out): %v", err)
	}

	if err := grantWriter.WriteS3ExternalPrincipalGrants(ctx, []map[string]any{{
		"source_uid": grantSourceIn, "principal_uid": grantPrincipalIn,
		"principal_kind": "aws_account", "principal_value": "111111111111",
		"principal_account_id": "111111111111", "principal_partition": "aws", "principal_service": "",
		"relationship_type": "GRANTS_ACCESS_TO", "grant_outcome": "cross_account",
		"is_public": false, "is_cross_account": true, "is_service_principal": false,
		"resolution_mode": "bucket_policy",
	}}, grantScopeIn, cloudEdgeGenerationID, grantEvidenceSource); err != nil {
		t.Fatalf("WriteS3ExternalPrincipalGrants(in): %v", err)
	}
	if err := grantWriter.WriteS3ExternalPrincipalGrants(ctx, []map[string]any{{
		"source_uid": grantSourceOut, "principal_uid": grantPrincipalOut,
		"principal_kind": "aws_account", "principal_value": "222222222222",
		"principal_account_id": "222222222222", "principal_partition": "aws", "principal_service": "",
		"relationship_type": "GRANTS_ACCESS_TO", "grant_outcome": "cross_account",
		"is_public": false, "is_cross_account": true, "is_service_principal": false,
		"resolution_mode": "bucket_policy",
	}}, grantScopeOut, cloudEdgeGenerationID, grantEvidenceSource); err != nil {
		t.Fatalf("WriteS3ExternalPrincipalGrants(out): %v", err)
	}

	if err := iamWriter.WriteIAMInstanceProfileRoleEdges(ctx, []map[string]any{{
		"profile_uid": iamProfileIn, "role_uid": iamRoleIn,
		"relationship_type": "HAS_ROLE", "resolution_mode": "role_arn",
	}}, iamScopeIn, cloudEdgeGenerationID, iamRoleEvidenceSource); err != nil {
		t.Fatalf("WriteIAMInstanceProfileRoleEdges(in): %v", err)
	}
	if err := iamWriter.WriteIAMInstanceProfileRoleEdges(ctx, []map[string]any{{
		"profile_uid": iamProfileOut, "role_uid": iamRoleOut,
		"relationship_type": "HAS_ROLE", "resolution_mode": "role_arn",
	}}, iamScopeOut, cloudEdgeGenerationID, iamRoleEvidenceSource); err != nil {
		t.Fatalf("WriteIAMInstanceProfileRoleEdges(out): %v", err)
	}

	runsImageQ := "MATCH (:KubernetesWorkload {uid: $w})-[r:RUNS_IMAGE]->(:OciImageManifest {uid: $s}) RETURN count(r)"
	logsToQ := "MATCH (:CloudResource {uid: $s})-[r:LOGS_TO]->(:CloudResource {uid: $t}) RETURN count(r)"
	grantsQ := "MATCH (:CloudResource {uid: $s})-[r:GRANTS_ACCESS_TO]->(:ExternalPrincipal {uid: $p}) RETURN count(r)"
	hasRoleQ := "MATCH (:CloudResource {uid: $p})-[r:HAS_ROLE]->(:CloudResource {uid: $ro}) RETURN count(r)"

	inRunsImage := map[string]any{"w": kcWorkloadIn, "s": kcSourceIn}
	outRunsImage := map[string]any{"w": kcWorkloadOut, "s": kcSourceOut}
	inLogsTo := map[string]any{"s": s3LogsSourceIn, "t": s3LogsTargetIn}
	outLogsTo := map[string]any{"s": s3LogsSourceOut, "t": s3LogsTargetOut}
	inGrants := map[string]any{"s": grantSourceIn, "p": grantPrincipalIn}
	outGrants := map[string]any{"s": grantSourceOut, "p": grantPrincipalOut}
	inHasRole := map[string]any{"p": iamProfileIn, "ro": iamRoleIn}
	outHasRole := map[string]any{"p": iamProfileOut, "ro": iamRoleOut}

	assertEdgeCount(ctx, t, exec, runsImageQ, inRunsImage, 1, "write: in-scope RUNS_IMAGE present")
	assertEdgeCount(ctx, t, exec, runsImageQ, outRunsImage, 1, "write: out-of-scope RUNS_IMAGE present")
	assertEdgeCount(ctx, t, exec, logsToQ, inLogsTo, 1, "write: in-scope LOGS_TO present")
	assertEdgeCount(ctx, t, exec, logsToQ, outLogsTo, 1, "write: out-of-scope LOGS_TO present")
	assertEdgeCount(ctx, t, exec, grantsQ, inGrants, 1, "write: in-scope GRANTS_ACCESS_TO present")
	assertEdgeCount(ctx, t, exec, grantsQ, outGrants, 1, "write: out-of-scope GRANTS_ACCESS_TO present")
	assertEdgeCount(ctx, t, exec, hasRoleQ, inHasRole, 1, "write: in-scope HAS_ROLE present")
	assertEdgeCount(ctx, t, exec, hasRoleQ, outHasRole, 1, "write: out-of-scope HAS_ROLE present")

	// --- Retract only the in-scope scope_id for each domain. ---
	if err := kcWriter.RetractKubernetesCorrelationEdges(ctx, []string{kcScopeIn}, cloudEdgeGenerationID, kcEvidenceSource); err != nil {
		t.Fatalf("RetractKubernetesCorrelationEdges: %v", err)
	}
	if err := s3LogsWriter.RetractS3LogsToEdges(ctx, []string{s3LogsScopeIn}, cloudEdgeGenerationID, s3LogsEvidenceSource); err != nil {
		t.Fatalf("RetractS3LogsToEdges: %v", err)
	}
	if err := grantWriter.RetractS3ExternalPrincipalGrants(ctx, []string{grantScopeIn}, cloudEdgeGenerationID, grantEvidenceSource); err != nil {
		t.Fatalf("RetractS3ExternalPrincipalGrants: %v", err)
	}
	if err := iamWriter.RetractIAMInstanceProfileRoleEdges(ctx, []string{iamScopeIn}, cloudEdgeGenerationID, iamRoleEvidenceSource); err != nil {
		t.Fatalf("RetractIAMInstanceProfileRoleEdges: %v", err)
	}

	assertEdgeCount(ctx, t, exec, runsImageQ, inRunsImage, 0, "retract: in-scope RUNS_IMAGE gone")
	assertEdgeCount(ctx, t, exec, logsToQ, inLogsTo, 0, "retract: in-scope LOGS_TO gone")
	assertEdgeCount(ctx, t, exec, grantsQ, inGrants, 0, "retract: in-scope GRANTS_ACCESS_TO gone")
	assertEdgeCount(ctx, t, exec, hasRoleQ, inHasRole, 0, "retract: in-scope HAS_ROLE gone")

	// Scoped retracts, not wipes: out-of-scope edges survive.
	assertEdgeCount(ctx, t, exec, runsImageQ, outRunsImage, 1, "retract: out-of-scope RUNS_IMAGE survives")
	assertEdgeCount(ctx, t, exec, logsToQ, outLogsTo, 1, "retract: out-of-scope LOGS_TO survives")
	assertEdgeCount(ctx, t, exec, grantsQ, outGrants, 1, "retract: out-of-scope GRANTS_ACCESS_TO survives")
	assertEdgeCount(ctx, t, exec, hasRoleQ, outHasRole, 1, "retract: out-of-scope HAS_ROLE survives")

	// Endpoint nodes always survive an edge retract.
	for _, q := range []struct {
		cypherText string
		key        string
	}{
		{"MATCH (n:KubernetesWorkload {uid: $u}) RETURN count(n)", kcWorkloadIn},
		{"MATCH (n:OciImageManifest {uid: $u}) RETURN count(n)", kcSourceIn},
		{"MATCH (n:CloudResource {uid: $u}) RETURN count(n)", s3LogsSourceIn},
		{"MATCH (n:CloudResource {uid: $u}) RETURN count(n)", s3LogsTargetIn},
		{"MATCH (n:CloudResource {uid: $u}) RETURN count(n)", grantSourceIn},
		{"MATCH (n:CloudResource {uid: $u}) RETURN count(n)", iamProfileIn},
		{"MATCH (n:CloudResource {uid: $u}) RETURN count(n)", iamRoleIn},
	} {
		assertEdgeCount(ctx, t, exec, q.cypherText, map[string]any{"u": q.key}, 1, "node survives: "+q.key)
	}
	// ExternalPrincipal is a global identity: the grant retract deletes only the
	// GRANTS_ACCESS_TO edge, never the principal node (by design, unlike the
	// bounded EvidenceArtifact nodes other retracts DETACH-delete).
	assertEdgeCount(ctx, t, exec,
		"MATCH (n:ExternalPrincipal {uid: $u}) RETURN count(n)",
		map[string]any{"u": grantPrincipalIn}, 1, "ExternalPrincipal node survives retract (global identity)")
}

// seedCloudEdgeNodes creates every endpoint node the four write templates
// MATCH: KubernetesWorkload/OciImageManifest pairs, and CloudResource pairs
// for the S3 logs-to, grant-source, and IAM profile/role edges. The
// ExternalPrincipal and SecurityGroupRule-family nodes are MERGEd by their own
// write templates and are not seeded here.
func seedCloudEdgeNodes(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	stmt := cypher.Statement{
		Cypher: `CREATE
       (:KubernetesWorkload {uid: $kcWlIn, marker: $marker}),
       (:KubernetesWorkload {uid: $kcWlOut, marker: $marker}),
       (:OciImageManifest {uid: $kcSrcIn, marker: $marker}),
       (:OciImageManifest {uid: $kcSrcOut, marker: $marker}),
       (:CloudResource {uid: $s3SrcIn, marker: $marker}),
       (:CloudResource {uid: $s3TgtIn, marker: $marker}),
       (:CloudResource {uid: $s3SrcOut, marker: $marker}),
       (:CloudResource {uid: $s3TgtOut, marker: $marker}),
       (:CloudResource {uid: $grantSrcIn, marker: $marker}),
       (:CloudResource {uid: $grantSrcOut, marker: $marker}),
       (:CloudResource {uid: $iamProfileIn, marker: $marker}),
       (:CloudResource {uid: $iamProfileOut, marker: $marker}),
       (:CloudResource {uid: $iamRoleIn, marker: $marker}),
       (:CloudResource {uid: $iamRoleOut, marker: $marker})`,
		Parameters: map[string]any{
			"kcWlIn": kcWorkloadIn, "kcWlOut": kcWorkloadOut,
			"kcSrcIn": kcSourceIn, "kcSrcOut": kcSourceOut,
			"s3SrcIn": s3LogsSourceIn, "s3TgtIn": s3LogsTargetIn,
			"s3SrcOut": s3LogsSourceOut, "s3TgtOut": s3LogsTargetOut,
			"grantSrcIn": grantSourceIn, "grantSrcOut": grantSourceOut,
			"iamProfileIn": iamProfileIn, "iamProfileOut": iamProfileOut,
			"iamRoleIn": iamRoleIn, "iamRoleOut": iamRoleOut,
			"marker": cloudEdgeMarker,
		},
	}
	if err := exec.Execute(ctx, stmt); err != nil {
		t.Fatalf("seed cloud-edge nodes: %v", err)
	}
}

// cleanupCloudEdgeScope removes every node this test creates, including the
// write-MERGEd ExternalPrincipal nodes.
func cleanupCloudEdgeScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, stmt := range []cypher.Statement{
		{
			Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
			Parameters: map[string]any{"marker": cloudEdgeMarker},
		},
		{
			Cypher:     `MATCH (a:ExternalPrincipal) WHERE a.uid IN $uids DETACH DELETE a`,
			Parameters: map[string]any{"uids": []string{grantPrincipalIn, grantPrincipalOut}},
		},
	} {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup cloud-edge scope: %v", err)
		}
	}
}
