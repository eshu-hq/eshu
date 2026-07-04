// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
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

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
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

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
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

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
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

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
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

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
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

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
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

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
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

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
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

	result, err := ExtractIAMCanPerformEdges(nil, nil)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 || result.Tally.total() != 0 {
		t.Fatalf("empty input must be a no-op; got %+v", result)
	}
}
