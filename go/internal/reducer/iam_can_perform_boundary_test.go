// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const canPerformBoundaryPolicyARN = "arn:aws:iam::111122223333:policy/developer-boundary"

func canPerformBoundaryEnvelope(principalARN string) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSIAMPermissionBoundaryFactKind,
		Payload: map[string]any{
			"account_id":          iamEscAccount,
			"region":              iamEscRegion,
			"principal_arn":       principalARN,
			"principal_type":      "user",
			"boundary_policy_arn": canPerformBoundaryPolicyARN,
			"boundary_type":       "PermissionsBoundaryPolicy",
		},
	}
}

func canPerformBoundaryPermissionEnvelope(principalARN, effect string, actions, resources []string, opts ...func(map[string]any)) facts.Envelope {
	merged := append([]func(map[string]any){
		func(p map[string]any) {
			p["policy_source"] = "permission_boundary"
			p["policy_arn"] = canPerformBoundaryPolicyARN
		},
	}, opts...)
	return escalationPermissionEnvelope(principalARN, effect, actions, resources, merged...)
}

func withBoundaryNotResources(notResources ...string) func(map[string]any) {
	return func(p map[string]any) { p["not_resources"] = toAnySlice(notResources) }
}

func TestIAMCanPerformPermissionBoundaryAllowsIdentityGrant(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		canPerformBoundaryEnvelope(attackerUserARN),
		canPerformBoundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("identity grant allowed by boundary must emit CAN_PERFORM edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if got := edge["actions"].([]string); len(got) != 1 || got[0] != "s3:getobject" {
		t.Fatalf("actions = %v, want [s3:getobject]", got)
	}
	if edge["evaluation_scope"] != "identity_policy_with_permission_boundary" {
		t.Fatalf("evaluation_scope = %v, want identity_policy_with_permission_boundary", edge["evaluation_scope"])
	}
	if got := edge["grant_sources"].([]string); !reflect.DeepEqual(got, []string{iamCanPerformGrantSourceIdentityPolicy}) {
		t.Fatalf("grant_sources = %v, want [identity_policy]", got)
	}
	if result.Tally.total() != 0 {
		t.Fatalf("allowing boundary should not record skips; tally=%+v", result.Tally)
	}
}

func TestIAMCanPerformPermissionBoundaryMissingAllowSuppressesGrant(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		canPerformBoundaryEnvelope(attackerUserARN),
		canPerformBoundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:putobject"}, []string{canPerformBucketARN}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("identity grant without matching boundary allow must not emit edge; got %v", result.Edges)
	}
	if result.Tally.skippedPermissionBoundary != 1 {
		t.Fatalf("skippedPermissionBoundary = %d, want 1 (tally=%+v)", result.Tally.skippedPermissionBoundary, result.Tally)
	}
	if result.Tally.total() != 1 {
		t.Fatalf("missing boundary allow must be operator-visible; tally=%+v", result.Tally)
	}
}

func TestIAMCanPerformPermissionBoundaryDenySuppressesGrant(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		canPerformBoundaryEnvelope(attackerUserARN),
		canPerformBoundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		canPerformBoundaryPermissionEnvelope(attackerUserARN, "Deny", []string{"s3:getobject"}, []string{"*"}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("boundary Deny must suppress the identity grant; got %v", result.Edges)
	}
	if result.Tally.skippedDeny != 1 {
		t.Fatalf("skippedDeny = %d, want 1 (tally=%+v)", result.Tally.skippedDeny, result.Tally)
	}
}

func TestIAMCanPerformPermissionBoundaryMissingDocumentIsUnresolved(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		canPerformBoundaryEnvelope(attackerUserARN),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("boundary attachment without a document must not emit edge; got %v", result.Edges)
	}
	if result.Tally.skippedUnresolved != 1 {
		t.Fatalf("skippedUnresolved = %d, want 1 (tally=%+v)", result.Tally.skippedUnresolved, result.Tally)
	}
}

func TestIAMCanPerformPermissionBoundaryConditionedAllowSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		canPerformBoundaryEnvelope(attackerUserARN),
		canPerformBoundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}, withConditions()),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("conditioned boundary allow must not emit edge; got %v", result.Edges)
	}
	if result.Tally.skippedConditioned != 1 {
		t.Fatalf("skippedConditioned = %d, want 1 (tally=%+v)", result.Tally.skippedConditioned, result.Tally)
	}
}

func TestIAMCanPerformPermissionBoundaryNotResourceSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		canPerformBoundaryEnvelope(attackerUserARN),
		canPerformBoundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}, withBoundaryNotResources("arn:aws:s3:::restricted/*")),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("boundary NotResource statement must not emit edge; got %v", result.Edges)
	}
	if result.Tally.skippedNotActionResource != 1 {
		t.Fatalf("skippedNotActionResource = %d, want 1 (tally=%+v)", result.Tally.skippedNotActionResource, result.Tally)
	}
}

func TestIAMCanPerformPermissionBoundaryDuplicateFactsConverge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		canPerformBoundaryEnvelope(attackerUserARN),
		canPerformBoundaryEnvelope(attackerUserARN),
		canPerformBoundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		canPerformBoundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 1 {
		t.Fatalf("duplicate boundary facts must converge to one edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if result.Edges[0]["evaluation_scope"] != "identity_policy_with_permission_boundary" {
		t.Fatalf("evaluation_scope = %v, want identity_policy_with_permission_boundary", result.Edges[0]["evaluation_scope"])
	}
	if result.Tally.total() != 0 {
		t.Fatalf("duplicate boundary evidence should not record skips; tally=%+v", result.Tally)
	}
}

func TestIAMCanPerformPermissionBoundaryStatementWithoutAttachmentDoesNotGrant(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		canPerformBoundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("boundary policy statement without an identity grant must not emit edge; got %v", result.Edges)
	}
	if result.Tally.total() != 0 {
		t.Fatalf("unused boundary policy statement should be ignored, not tallied; tally=%+v", result.Tally)
	}
}

func TestIAMCanPerformNoPermissionBoundaryKeepsIdentityScope(t *testing.T) {
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
		t.Fatalf("identity grant without boundary should still emit edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if edge["evaluation_scope"] != iamCanPerformEvaluationScopeIdentityPolicyOnly {
		t.Fatalf("evaluation_scope = %v, want %q", edge["evaluation_scope"], iamCanPerformEvaluationScopeIdentityPolicyOnly)
	}
}
