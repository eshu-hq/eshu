// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func canPerformResourcePolicyEnvelope(
	resourceARN string,
	resourceType string,
	effect string,
	actions []string,
	principalARNs []string,
	opts ...func(map[string]any),
) facts.Envelope {
	payload := map[string]any{
		"account_id":            iamEscAccount,
		"region":                "us-east-1",
		"resource_arn":          resourceARN,
		"resource_type":         resourceType,
		"policy_source":         "resource",
		"effect":                effect,
		"actions":               toAnySlice(actions),
		"not_actions":           []any{},
		"resources":             toAnySlice([]string{resourceARN}),
		"not_resources":         []any{},
		"condition_keys":        []any{},
		"principal_account_ids": []any{},
		"principal_arns":        toAnySlice(principalARNs),
		"principal_types":       toAnySlice([]string{"aws"}),
		"has_conditions":        false,
		"is_wildcard_action":    containsAny(actions, "*"),
		"is_wildcard_resource":  false,
		"is_public":             false,
		"is_cross_account":      false,
	}
	for _, opt := range opts {
		opt(payload)
	}
	return facts.Envelope{FactKind: facts.AWSResourcePolicyPermissionFactKind, Payload: payload}
}

func withPublicResourcePolicyPrincipal() func(map[string]any) {
	return func(p map[string]any) {
		p["principal_arns"] = []any{}
		p["principal_types"] = toAnySlice([]string{"aws"})
		p["is_public"] = true
	}
}

func withResourcePolicyNotResources(notResources ...string) func(map[string]any) {
	return func(p map[string]any) { p["not_resources"] = toAnySlice(notResources) }
}

func withResourcePolicyResources(resources ...string) func(map[string]any) {
	return func(p map[string]any) { p["resources"] = toAnySlice(resources) }
}

// TestIAMCanPerformResourcePolicyExactPrincipalEmitsEdge proves a normalized
// resource-policy permission fact grants CAN_PERFORM only when its grantee
// principal resolves to an already-scanned IAM CloudResource node.
func TestIAMCanPerformResourcePolicyExactPrincipalEmitsEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(
			canPerformBucketARN,
			iamCanPerformResourceTypeS3Bucket,
			"Allow",
			[]string{"s3:getobject"},
			[]string{attackerUserARN},
		),
	}

	result, err := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("expected resource-policy CAN_PERFORM edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if got := edge["actions"].([]string); len(got) != 1 || got[0] != "s3:getobject" {
		t.Fatalf("actions = %v, want [s3:getobject]", got)
	}
	if got := edge["grant_sources"].([]string); len(got) != 1 || got[0] != iamCanPerformGrantSourceResourcePolicy {
		t.Fatalf("grant_sources = %v, want [resource_policy]", got)
	}
	if edge["evaluation_scope"] != iamCanPerformEvaluationScopeResourcePolicyOnly {
		t.Fatalf("evaluation_scope = %v, want %q", edge["evaluation_scope"], iamCanPerformEvaluationScopeResourcePolicyOnly)
	}
	if result.EdgesByMode[iamCanPerformResolutionExactARN] != 1 {
		t.Fatalf("edges-by-mode = %v, want one exact_arn", result.EdgesByMode)
	}
}

// TestIAMCanPerformPublicResourcePolicyPrincipalIsSkipped proves public resource
// policy principals do not fabricate a principal endpoint and are counted.
func TestIAMCanPerformPublicResourcePolicyPrincipalIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(
			canPerformBucketARN,
			iamCanPerformResourceTypeS3Bucket,
			"Allow",
			[]string{"s3:getobject"},
			nil,
			withPublicResourcePolicyPrincipal(),
		),
	}

	result, err := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("public resource-policy principal must not create CAN_PERFORM; got %v", result.Edges)
	}
	if result.Tally.skippedAmbiguous != 1 {
		t.Fatalf("skippedAmbiguous = %d, want 1 (tally=%+v)", result.Tally.skippedAmbiguous, result.Tally)
	}
}

// TestIAMCanPerformResourcePolicyDenyBlocksAllow proves a Deny in the resource
// policy removes the matching resource-policy grant for that exact principal.
func TestIAMCanPerformResourcePolicyDenyBlocksAllow(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(canPerformBucketARN, iamCanPerformResourceTypeS3Bucket, "Allow", []string{"s3:getobject"}, []string{attackerUserARN}),
		canPerformResourcePolicyEnvelope(canPerformBucketARN, iamCanPerformResourceTypeS3Bucket, "Deny", []string{"s3:getobject"}, []string{attackerUserARN}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("resource-policy Deny must block the grant; got %v", result.Edges)
	}
	if result.Tally.skippedDeny != 1 {
		t.Fatalf("skippedDeny = %d, want 1 (tally=%+v)", result.Tally.skippedDeny, result.Tally)
	}
}

// TestIAMCanPerformResourcePolicyConditionedStatementIsSkipped proves resource
// policy condition-key summaries remain non-exact and do not promote an edge.
func TestIAMCanPerformResourcePolicyConditionedStatementIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(
			canPerformBucketARN,
			iamCanPerformResourceTypeS3Bucket,
			"Allow",
			[]string{"s3:getobject"},
			[]string{attackerUserARN},
			withConditions(),
		),
	}

	result, err := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("conditioned resource-policy grant must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedConditioned != 1 {
		t.Fatalf("skippedConditioned = %d, want 1 (tally=%+v)", result.Tally.skippedConditioned, result.Tally)
	}
}

// TestIAMCanPerformResourcePolicyNotResourceStatementIsSkipped proves resource
// policy NotResource is not treated as a positive exact grant.
func TestIAMCanPerformResourcePolicyNotResourceStatementIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(
			canPerformBucketARN,
			iamCanPerformResourceTypeS3Bucket,
			"Allow",
			[]string{"s3:getobject"},
			[]string{attackerUserARN},
			withResourcePolicyNotResources("arn:aws:s3:::restricted/*"),
		),
	}

	result, err := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("NotResource resource-policy grant must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedNotActionResource != 1 {
		t.Fatalf("skippedNotActionResource = %d, want 1 (tally=%+v)", result.Tally.skippedNotActionResource, result.Tally)
	}
}

// TestIAMCanPerformResourcePolicyWrongResourcePatternIsUnresolved proves the
// statement Resource patterns must apply to the attached resource before the
// reducer promotes the attached resource as the CAN_PERFORM target.
func TestIAMCanPerformResourcePolicyWrongResourcePatternIsUnresolved(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(
			canPerformBucketARN,
			iamCanPerformResourceTypeS3Bucket,
			"Allow",
			[]string{"s3:getobject"},
			[]string{attackerUserARN},
			withResourcePolicyResources("arn:aws:s3:::other-bucket/*"),
		),
	}

	result, err := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("resource policy naming a different resource must not create edge; got %v", result.Edges)
	}
	if result.Tally.skippedUnresolved != 1 {
		t.Fatalf("skippedUnresolved = %d, want 1 (tally=%+v)", result.Tally.skippedUnresolved, result.Tally)
	}
}

// TestIAMCanPerformResourcePolicyObjectPatternTargetsAttachedBucket proves S3
// object-prefix resources on a bucket policy resolve to the attached bucket node,
// not to a fabricated object-level resource.
func TestIAMCanPerformResourcePolicyObjectPatternTargetsAttachedBucket(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(
			canPerformBucketARN,
			iamCanPerformResourceTypeS3Bucket,
			"Allow",
			[]string{"s3:getobject"},
			[]string{attackerUserARN},
			withResourcePolicyResources(canPerformBucketARN+"/private/*"),
		),
	}

	result, err := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("object-prefix bucket policy must resolve to the attached bucket; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if got := edge["actions"].([]string); len(got) != 1 || got[0] != "s3:getobject" {
		t.Fatalf("actions = %v, want [s3:getobject]", got)
	}
}

// TestIAMCanPerformResourcePolicyKMSWildcardResourceTargetsAttachedKey proves a
// KMS key policy's Resource:"*" is scoped back to the attached key, not treated
// as a global wildcard over every scanned key.
func TestIAMCanPerformResourcePolicyKMSWildcardResourceTargetsAttachedKey(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeKMSKey, canPerformKMSKeyARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(
			canPerformKMSKeyARN,
			iamCanPerformResourceTypeKMSKey,
			"Allow",
			[]string{"kms:decrypt"},
			[]string{attackerUserARN},
			withResourcePolicyResources("*"),
		),
	}

	result, err := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeKMSKey, canPerformKMSKeyARN))
	if edge == nil {
		t.Fatalf("KMS wildcard resource must resolve to the attached key; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if edge["evaluation_scope"] != iamCanPerformEvaluationScopeResourcePolicyOnly {
		t.Fatalf("evaluation_scope = %v, want %q", edge["evaluation_scope"], iamCanPerformEvaluationScopeResourcePolicyOnly)
	}
}

// TestIAMCanPerformResourcePolicyServiceWildcardOnlyEvaluatesAttachedType proves
// s3:* on an S3 bucket policy does not count every non-S3 catalog action as an
// unresolved target.
func TestIAMCanPerformResourcePolicyServiceWildcardOnlyEvaluatesAttachedType(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(
			canPerformBucketARN,
			iamCanPerformResourceTypeS3Bucket,
			"Allow",
			[]string{"s3:*"},
			[]string{attackerUserARN},
		),
	}

	result, err := ExtractIAMCanPerformEdges(resources, nil, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 1 {
		t.Fatalf("s3:* resource policy should produce one bucket edge, got %v", result.Edges)
	}
	if result.Tally.total() != 0 {
		t.Fatalf("s3:* should not count non-S3 catalog actions as skips; tally=%+v", result.Tally)
	}
}

// TestIAMCanPerformResourcePolicyUsesProvidedCatalog proves resource-policy
// extraction uses its caller-supplied catalog consistently, rather than
// consulting the package global catalog while matching or accounting statements.
func TestIAMCanPerformResourcePolicyUsesProvidedCatalog(t *testing.T) {
	t.Parallel()

	action := "custom:readresource"
	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	catalog := map[string]iamCanPerformAction{
		action: {Action: action, ExpectedResourceType: iamCanPerformResourceTypeS3Bucket},
	}
	edges := make(map[edgeKey]*iamCanPerformEdgeAccumulator)
	var tally iamCanPerformTally

	index, err := buildCloudResourceJoinIndex(resources)
	if err != nil {
		t.Fatalf("buildCloudResourceJoinIndex() error = %v, want nil", err)
	}
	addIAMCanPerformResourcePolicyEdges(
		index,
		[]facts.Envelope{
			canPerformResourcePolicyEnvelope(
				canPerformBucketARN,
				iamCanPerformResourceTypeS3Bucket,
				"Allow",
				[]string{action},
				[]string{attackerUserARN},
			),
		},
		catalog,
		edges,
		&tally,
	)
	rows := buildIAMCanPerformEdgeRows(edges, make(map[string]int))

	edge := canPerformEdgeFor(rows, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("custom catalog action should emit a resource-policy edge; rows=%v tally=%+v", rows, tally)
	}
	if got := edge["actions"].([]string); len(got) != 1 || got[0] != action {
		t.Fatalf("actions = %v, want [%s]", got, action)
	}
	if tally.total() != 0 {
		t.Fatalf("custom catalog action must not be counted as a skip; tally=%+v", tally)
	}
}

// TestIAMCanPerformIdentityAndResourcePolicySourcesMerge proves identity and
// resource policy grants to the same edge identity merge actions and source
// labels without putting the source into the relationship MERGE key.
func TestIAMCanPerformIdentityAndResourcePolicySourcesMerge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}
	resourcePolicies := []facts.Envelope{
		canPerformResourcePolicyEnvelope(canPerformBucketARN, iamCanPerformResourceTypeS3Bucket, "Allow", []string{"s3:getobject"}, []string{attackerUserARN}),
	}

	result, err := ExtractIAMCanPerformEdges(resources, perms, resourcePolicies)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges() error = %v, want nil", err)
	}
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("expected merged identity/resource-policy edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if got := edge["actions"].([]string); len(got) != 1 || got[0] != "s3:getobject" {
		t.Fatalf("actions = %v, want [s3:getobject]", got)
	}
	wantSources := []string{iamCanPerformGrantSourceIdentityPolicy, iamCanPerformGrantSourceResourcePolicy}
	if got := edge["grant_sources"].([]string); !reflect.DeepEqual(got, wantSources) {
		t.Fatalf("grant_sources = %v, want %v", got, wantSources)
	}
	if edge["evaluation_scope"] != iamCanPerformEvaluationScopeIdentityAndResourcePolicy {
		t.Fatalf("evaluation_scope = %v, want %q", edge["evaluation_scope"], iamCanPerformEvaluationScopeIdentityAndResourcePolicy)
	}
	if len(result.Edges) != 1 {
		t.Fatalf("identity/resource-policy grants must merge to one edge; got %d", len(result.Edges))
	}
}
