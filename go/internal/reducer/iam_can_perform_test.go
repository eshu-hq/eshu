// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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

	result := ExtractIAMCanPerformEdges(resources, perms)
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

	result := ExtractIAMCanPerformEdges(resources, perms)
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

	result := ExtractIAMCanPerformEdges(resources, perms)
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

	result := ExtractIAMCanPerformEdges(resources, perms)
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

	result := ExtractIAMCanPerformEdges(resources, perms)
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
		[]facts.Envelope{
			escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"cloudwatch:getmetricdata"}, []string{canPerformBucketARN}),
		},
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

	result := ExtractIAMCanPerformEdges(resources, perms)
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

// TestIAMCanPerformDenyIsSkipped proves a Deny on a catalogued action removes the
// grant (no edge), counted skipped_deny.
func TestIAMCanPerformDenyIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		escalationPermissionEnvelope(attackerUserARN, "Deny", []string{"s3:getobject"}, []string{"*"}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("Deny must block the grant; got %v", result.Edges)
	}
	if result.Tally.skippedDeny != 1 {
		t.Fatalf("skippedDeny = %d, want 1 (tally=%+v)", result.Tally.skippedDeny, result.Tally)
	}
}

// TestIAMCanPerformConditionedStatementIsSkipped proves a condition-gated grant is
// not trusted (no edge), counted skipped_conditioned.
func TestIAMCanPerformConditionedStatementIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}, withConditions()),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("conditioned grant must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedConditioned != 1 {
		t.Fatalf("skippedConditioned = %d, want 1 (tally=%+v)", result.Tally.skippedConditioned, result.Tally)
	}
}

// TestIAMCanPerformNotActionStatementIsSkipped proves a NotAction-bearing grant is
// not trusted, counted skipped_not_action_resource.
func TestIAMCanPerformNotActionStatementIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}, withNotActions("s3:deletebucket")),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("NotAction grant must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedNotActionResource != 1 {
		t.Fatalf("skippedNotActionResource = %d, want 1 (tally=%+v)", result.Tally.skippedNotActionResource, result.Tally)
	}
}

// TestIAMCanPerformUnscannedResourceIsSkipped proves a resource ARN that was not
// scanned (including cross-account) produces no edge, counted skipped_unresolved.
func TestIAMCanPerformUnscannedResourceIsSkipped(t *testing.T) {
	t.Parallel()

	crossAccountBucket := "arn:aws:s3:::foreign-bucket"
	resources := []facts.Envelope{attackerNode()}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{crossAccountBucket}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("unscanned/cross-account resource must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedUnresolved != 1 {
		t.Fatalf("skippedUnresolved = %d, want 1 (tally=%+v)", result.Tally.skippedUnresolved, result.Tally)
	}
}

// TestIAMCanPerformWrongTypeMatchIsUnresolved proves an exact ARN that is scanned
// but classifies as a DIFFERENT service than the catalog action expects does not
// resolve — an s3:GetObject grant naming a KMS key ARN yields no edge.
func TestIAMCanPerformWrongTypeMatchIsUnresolved(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeKMSKey, canPerformKMSKeyARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformKMSKeyARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("a wrong-type ARN must not resolve; got %v", result.Edges)
	}
	if result.Tally.skippedUnresolved != 1 {
		t.Fatalf("skippedUnresolved = %d, want 1 (tally=%+v)", result.Tally.skippedUnresolved, result.Tally)
	}
}

// TestIAMCanPerformSelfLoopIsSkipped proves a grant whose resolved resource is the
// principal's own node is counted skipped_self_loop, never an edge. A principal
// CloudResource node sharing an ARN with a catalogued resource type is contrived
// but the self-loop guard must hold.
func TestIAMCanPerformSelfLoopIsSkipped(t *testing.T) {
	t.Parallel()

	// The principal node IS an s3 bucket ARN here (a contrived collision) so the
	// resolved resource uid equals the principal uid; the self-loop guard fires.
	selfARN := canPerformBucketARN
	resources := []facts.Envelope{
		canPerformNode(iamCanPerformResourceTypeS3Bucket, selfARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(selfARN, "Allow", []string{"s3:getobject"}, []string{selfARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("self-loop must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedSelfLoop != 1 {
		t.Fatalf("skippedSelfLoop = %d, want 1 (tally=%+v)", result.Tally.skippedSelfLoop, result.Tally)
	}
}

// TestIAMCanPerformUnscannedPrincipalIsSkipped proves a principal that was not
// scanned has no source node and emits no edge, counted skipped_unresolved once.
func TestIAMCanPerformUnscannedPrincipalIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("unscanned principal must not produce an edge; got %v", result.Edges)
	}
	if result.Tally.skippedUnresolved != 1 {
		t.Fatalf("skippedUnresolved = %d, want 1 (tally=%+v)", result.Tally.skippedUnresolved, result.Tally)
	}
}

// TestIAMCanPerformMultipleActionsMergeIntoOneEdge proves two catalogued actions
// that resolve to the SAME resource node converge on ONE edge with a sorted merged
// actions list (the keying decision: action set is an edge property, not the key).
func TestIAMCanPerformMultipleActionsMergeIntoOneEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:putobject", "s3:getobject"}, []string{canPerformBucketARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("expected one merged edge; rows=%v", result.Edges)
	}
	got := edge["actions"].([]string)
	want := []string{"s3:getobject", "s3:putobject"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged actions = %v, want %v (sorted, deduped)", got, want)
	}
	if edge["action_count"].(int) != 2 {
		t.Fatalf("action_count = %v, want 2", edge["action_count"])
	}
	if len(result.Edges) != 1 {
		t.Fatalf("two actions to one resource must merge to ONE edge; got %d", len(result.Edges))
	}
}

// TestIAMCanPerformServiceWildcardCoversAction proves s3:* (a service wildcard)
// arms catalogued s3: actions — the conservative wildcard expansion the shared
// grant helper already supports.
func TestIAMCanPerformServiceWildcardCoversAction(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:*"}, []string{canPerformBucketARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("s3:* must arm catalogued s3: actions; rows=%v", result.Edges)
	}
	// s3:* covers the reviewed bucket-target S3 catalog actions -> all to the
	// same bucket -> merged into one edge.
	got := edge["actions"].([]string)
	want := []string{"s3:deletebucket", "s3:getobject", "s3:listbucket", "s3:putobject"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("s3:* bucket actions = %v, want %v", got, want)
	}
}

// TestIAMCanPerformEmptyInputIsNoOp proves empty permission facts produce no edges
// and no panic (empty/stale state).
func TestIAMCanPerformEmptyInputIsNoOp(t *testing.T) {
	t.Parallel()

	if result := ExtractIAMCanPerformEdges(nil, nil); len(result.Edges) != 0 || result.Tally.total() != 0 {
		t.Fatalf("empty input must be a no-op; got %+v", result)
	}
}
