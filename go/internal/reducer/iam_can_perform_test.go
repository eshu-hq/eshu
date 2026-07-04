// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// iamPermissionStatementsForTest decodes aws_iam_permission envelopes into the
// iamPermissionStatement values the grant builders now accept, exercising the
// real decode seam so a test fixture that omits a required field fails loudly
// rather than silently building a zero-value statement.
func iamPermissionStatementsForTest(t *testing.T, envelopes ...facts.Envelope) []iamPermissionStatement {
	t.Helper()
	statements := make([]iamPermissionStatement, 0, len(envelopes))
	for _, env := range envelopes {
		permission, err := decodeAWSIAMPermission(env)
		if err != nil {
			t.Fatalf("decodeAWSIAMPermission() error = %v, want nil", err)
		}
		statements = append(statements, iamPermissionStatement{factID: env.FactID, permission: permission})
	}
	return statements
}

// CAN_PERFORM fixtures share the escalation slice's account/region so the
// in-memory join index resolves principal and resource under the trust boundary.
// Resource ARNs use the real per-service ARN shapes so the ARN classifier keys
// them to the catalog-expected resource_type.
const (
	canPerformBucketARN = "arn:aws:s3:::prod-data-bucket"
	canPerformKMSKeyARN = "arn:aws:kms:us-east-1:111122223333:key/abc-123"
	canPerformSecretARN = "arn:aws:secretsmanager:us-east-1:111122223333:secret:db-creds"
	canPerformTableARN  = "arn:aws:dynamodb:us-east-1:111122223333:table/orders"
)

// canPerformNode builds an aws_resource fact for a non-IAM resource node so the
// extractor resolves it to a CloudResource uid through the shared join index. The
// ARN is used as both resource_id and arn so byARN resolves it. account/region
// mirror the escalation fixtures so the principal and resource share a scope.
func canPerformNode(resourceType, arn string) facts.Envelope {
	return resourceEnvelope(iamEscAccount, "us-east-1", resourceType, arn, arn)
}

// canPerformUID returns the resource uid a non-IAM node fixture resolves to.
func canPerformUID(resourceType, arn string) string {
	return cloudResourceUID(iamEscAccount, "us-east-1", resourceType, arn)
}

// canPerformEdgeFor returns the single CAN_PERFORM edge row for a
// (principal, resource) pair, or nil. CAN_PERFORM rows key the second endpoint on
// resource_uid (not the escalation slice's target_uid), so it needs its own lookup.
func canPerformEdgeFor(rows []map[string]any, principalUID, resourceUID string) map[string]any {
	for _, row := range rows {
		if row["principal_uid"] == principalUID && row["resource_uid"] == resourceUID {
			return row
		}
	}
	return nil
}

// TestIAMCanPerformPositiveExactARN proves a catalogued action whose resource is
// an exact scanned ARN of the expected type emits one edge carrying that action,
// the identity_policy_only scope, and exact_arn resolution mode.
func TestIAMCanPerformPositiveExactARN(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("expected one CAN_PERFORM edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if got := edge["actions"].([]string); len(got) != 1 || got[0] != "s3:getobject" {
		t.Fatalf("actions = %v, want [s3:getobject]", got)
	}
	if edge["action_count"].(int) != 1 {
		t.Fatalf("action_count = %v, want 1", edge["action_count"])
	}
	if edge["evaluation_scope"] != iamCanPerformEvaluationScope {
		t.Fatalf("evaluation_scope = %v, want %q", edge["evaluation_scope"], iamCanPerformEvaluationScope)
	}
	if got := edge["grant_sources"].([]string); len(got) != 1 || got[0] != iamCanPerformGrantSourceIdentityPolicy {
		t.Fatalf("grant_sources = %v, want [identity_policy]", got)
	}
	if result.EdgesByMode[iamCanPerformResolutionExactARN] != 1 {
		t.Fatalf("edges-by-mode = %v, want one exact_arn", result.EdgesByMode)
	}
}

// TestIAMCanPerformSingleGlobMatchEmitsEdge proves a glob resource that matches
// EXACTLY one scanned node of the expected type resolves to a confident edge
// labelled single_glob.
func TestIAMCanPerformSingleGlobMatchEmitsEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{"arn:aws:s3:::prod-data-*"}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("single glob match must emit an edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if result.EdgesByMode[iamCanPerformResolutionSingleGlob] != 1 {
		t.Fatalf("edges-by-mode = %v, want one single_glob", result.EdgesByMode)
	}
}

// TestIAMCanPerformManyMatchingTargetsIsAmbiguous proves a glob that matches more
// than one scanned node of the expected type is skipped_ambiguous, not an edge.
func TestIAMCanPerformManyMatchingTargetsIsAmbiguous(t *testing.T) {
	t.Parallel()

	bucketA := "arn:aws:s3:::team-a"
	bucketB := "arn:aws:s3:::team-b"
	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, bucketA),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, bucketB),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{"arn:aws:s3:::team-*"}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("glob matching many nodes must be ambiguous; got %v", result.Edges)
	}
	if result.Tally.skippedAmbiguous != 1 {
		t.Fatalf("skippedAmbiguous = %d, want 1 (tally=%+v)", result.Tally.skippedAmbiguous, result.Tally)
	}
}

// TestIAMCanPerformWildcardResourceIsSkippedAmbiguous proves Resource:"*" names no
// single node and is recorded skipped_ambiguous, not promoted to an edge.
func TestIAMCanPerformWildcardResourceIsSkippedAmbiguous(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{"*"}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("wildcard resource must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedAmbiguous != 1 {
		t.Fatalf("skippedAmbiguous = %d, want 1 (tally=%+v)", result.Tally.skippedAmbiguous, result.Tally)
	}
}

// TestIAMCanPerformUncataloguedActionIsSkipped proves an action that is granted
// but not in the closed catalog produces no edge, is counted, and does not stop
// a catalogued action in the same statement from resolving. This asserts the
// closed-vocabulary boundary.
func TestIAMCanPerformUncataloguedActionIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	// cloudwatch:getmetricdata is NOT in the catalog; s3:getobject is. Only
	// getobject resolves.
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"cloudwatch:getmetricdata", "s3:getobject"}, []string{canPerformBucketARN}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("the catalogued action must still resolve; rows=%v", result.Edges)
	}
	if got := edge["actions"].([]string); len(got) != 1 || got[0] != "s3:getobject" {
		t.Fatalf("only the catalogued action belongs on the edge; got %v", got)
	}
	if result.Tally.skippedUncatalogued != 1 {
		t.Fatalf("the uncatalogued action must be counted skipped_uncatalogued_action; tally=%+v", result.Tally)
	}
	if result.Tally.total() != 1 {
		t.Fatalf("the uncatalogued skip is the only tallied refusal; tally=%+v", result.Tally)
	}
}

// TestBuildIAMCanPerformGrantCountsUncataloguedActions proves the grant builder
// owns skipped_uncatalogued_action accounting for trusted Allow statements,
// rather than relying on a later catalog loop that never visits uncatalogued
// actions.
func TestBuildIAMCanPerformGrantCountsUncataloguedActions(t *testing.T) {
	t.Parallel()

	var tally iamCanPerformTally
	grant := buildIAMCanPerformGrant(
		iamPermissionStatementsForTest(
			t,
			escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"cloudwatch:getmetricdata"}, []string{canPerformBucketARN}),
		),
		&tally,
	)

	if grant.allows("cloudwatch:getmetricdata") {
		t.Fatal("uncatalogued action must not arm the CAN_PERFORM grant")
	}
	if tally.skippedUncatalogued != 1 {
		t.Fatalf("skippedUncatalogued = %d, want 1 (tally=%+v)", tally.skippedUncatalogued, tally)
	}
	if tally.total() != 1 {
		t.Fatalf("the uncatalogued skip is the only tallied refusal; tally=%+v", tally)
	}
}

// TestIAMCanPerformUncataloguedExplicitlyCounted proves an out-of-catalog granted
// action emits no edge AND is counted skipped_uncatalogued_action, so the
// closed-vocabulary boundary is visible in metrics/logs (design §3) rather than a
// silent zero.
func TestIAMCanPerformUncataloguedExplicitlyCounted(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"cloudwatch:getmetricdata"}, []string{canPerformBucketARN}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("uncatalogued action must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedUncatalogued != 1 {
		t.Fatalf("an uncatalogued granted action must be counted skipped_uncatalogued_action; tally=%+v", result.Tally)
	}
	if result.Tally.total() != 1 {
		t.Fatalf("the uncatalogued skip is the only tallied refusal; tally=%+v", result.Tally)
	}
}
