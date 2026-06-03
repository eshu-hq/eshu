package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// withPolicySource overrides the policy_source on an aws_iam_permission fixture so
// a fixture can stand in for a permission-boundary statement (policy_source =
// "boundary") versus an identity-policy statement (the default "inline").
func withPolicySource(source string) func(map[string]any) {
	return func(p map[string]any) { p["policy_source"] = source }
}

// boundaryPermissionEnvelope builds one aws_iam_permission statement fact carrying
// policy_source = "boundary" for the named principal, the metadata-only projection
// of a statement from that principal's permission-boundary policy document.
func boundaryPermissionEnvelope(principalARN, effect string, actions, resources []string, opts ...func(map[string]any)) facts.Envelope {
	allOpts := append([]func(map[string]any){withPolicySource(iamCanPerformPolicySourceBoundary)}, opts...)
	return escalationPermissionEnvelope(principalARN, effect, actions, resources, allOpts...)
}

// TestIAMCanPerformIdentityAllowAndBoundaryAllowEmitsEdge proves the intersection:
// a principal WITH a boundary that ALSO allows the catalogued action on a covering
// resource keeps the identity-policy edge, and the edge records that the boundary
// layer was evaluated.
func TestIAMCanPerformIdentityAllowAndBoundaryAllowEmitsEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("identity-allow intersected with boundary-allow must keep the edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if got, ok := edge["boundary_evaluated"].(bool); !ok || !got {
		t.Fatalf("boundary_evaluated = %v, want true on a bounded principal's edge", edge["boundary_evaluated"])
	}
	if edge["evaluation_scope"] != iamCanPerformEvaluationScopeIdentityPolicyBoundary {
		t.Fatalf("evaluation_scope = %v, want %q", edge["evaluation_scope"], iamCanPerformEvaluationScopeIdentityPolicyBoundary)
	}
}

// TestIAMCanPerformIdentityAllowNoBoundaryAllowSuppressesEdge proves a principal
// WITH a boundary that does NOT allow the catalogued action makes the identity
// allow non-effective: no edge, counted skipped_boundary_no_allow.
func TestIAMCanPerformIdentityAllowNoBoundaryAllowSuppressesEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		// Boundary allows a different (also catalogued) action, not s3:getobject.
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:putobject"}, []string{canPerformBucketARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("a boundary that does not allow the action must suppress the edge; got %v", result.Edges)
	}
	if result.Tally.skippedBoundaryNoAllow != 1 {
		t.Fatalf("skippedBoundaryNoAllow = %d, want 1 (tally=%+v)", result.Tally.skippedBoundaryNoAllow, result.Tally)
	}
}

// TestIAMCanPerformBoundaryExplicitDenySuppressesEdge proves a boundary explicit
// Deny on the catalogued action removes the candidate edge, counted
// skipped_boundary_deny, even though the boundary also has an allow for it.
func TestIAMCanPerformBoundaryExplicitDenySuppressesEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Deny", []string{"s3:getobject"}, []string{"*"}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("a boundary explicit Deny must suppress the edge; got %v", result.Edges)
	}
	if result.Tally.skippedBoundaryDeny != 1 {
		t.Fatalf("skippedBoundaryDeny = %d, want 1 (tally=%+v)", result.Tally.skippedBoundaryDeny, result.Tally)
	}
}

// TestIAMCanPerformBoundaryAllowsActionButNotResourceSuppressesEdge proves the
// boundary allow must cover the SAME resolved resource: a boundary that allows the
// action only on a different resource ARN suppresses the edge.
func TestIAMCanPerformBoundaryAllowsActionButNotResourceSuppressesEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		// Boundary allows s3:getobject but only on an unrelated bucket.
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{"arn:aws:s3:::other-bucket"}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("a boundary allow on a different resource must not cover this edge; got %v", result.Edges)
	}
	if result.Tally.skippedBoundaryNoAllow != 1 {
		t.Fatalf("skippedBoundaryNoAllow = %d, want 1 (tally=%+v)", result.Tally.skippedBoundaryNoAllow, result.Tally)
	}
}

// TestIAMCanPerformBoundaryWildcardResourceCoversEdge proves a boundary allow whose
// resource is "*" (or a covering glob) on the catalogued action covers the resolved
// resource and keeps the edge.
func TestIAMCanPerformBoundaryWildcardResourceCoversEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{"*"}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("a wildcard-resource boundary allow must cover the edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if got, ok := edge["boundary_evaluated"].(bool); !ok || !got {
		t.Fatalf("boundary_evaluated = %v, want true", edge["boundary_evaluated"])
	}
}

// TestIAMCanPerformBoundaryServiceWildcardActionCoversEdge proves a boundary allow
// using a service wildcard action (s3:*) covers a concrete catalogued action in
// that service, mirroring the identity-grant allows() precedence.
func TestIAMCanPerformBoundaryServiceWildcardActionCoversEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:*"}, []string{canPerformBucketARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("a service-wildcard boundary allow must cover the concrete action; rows=%v tally=%+v", result.Edges, result.Tally)
	}
}

// TestIAMCanPerformBoundaryConditionedAllowSuppressesEdge proves a boundary Allow
// statement that carries the catalogued action but is condition-gated is treated
// conservatively (not a permissive boundary): the edge is suppressed and counted
// skipped_boundary_conditioned.
func TestIAMCanPerformBoundaryConditionedAllowSuppressesEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}, withConditions()),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("a conditioned boundary allow is not conservatively permissive; got %v", result.Edges)
	}
	if result.Tally.skippedBoundaryConditioned != 1 {
		t.Fatalf("skippedBoundaryConditioned = %d, want 1 (tally=%+v)", result.Tally.skippedBoundaryConditioned, result.Tally)
	}
}

// TestIAMCanPerformBoundaryNotActionAllowSuppressesEdge proves a boundary Allow
// statement carrying the catalogued action via a NotAction/NotResource shape is
// treated conservatively: suppressed, counted skipped_boundary_not_action_resource.
func TestIAMCanPerformBoundaryNotActionAllowSuppressesEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}, withNotActions("s3:deletebucket")),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("a NotAction boundary allow is not conservatively permissive; got %v", result.Edges)
	}
	if result.Tally.skippedBoundaryNotActionResource != 1 {
		t.Fatalf("skippedBoundaryNotActionResource = %d, want 1 (tally=%+v)", result.Tally.skippedBoundaryNotActionResource, result.Tally)
	}
}

// TestIAMCanPerformBoundaryUnresolvedSuppressesEdge proves that when a principal is
// known to carry a permission boundary but the boundary document yielded no usable
// statements (an unresolved/empty-boundary case), the candidate edge is suppressed
// conservatively and counted skipped_boundary_unresolved. The boundary presence is
// signalled by a single tombstone-free boundary statement that carries no Allow on
// the action (here: an empty-action boundary marker statement).
func TestIAMCanPerformBoundaryUnresolvedSuppressesEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		// Boundary present but yields only an unrelated, non-catalogued allow: it
		// cannot make s3:getobject effective, so the bounded principal loses the edge.
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"logs:createloggroup"}, []string{"*"}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 0 {
		t.Fatalf("a boundary with no covering allow must suppress the edge; got %v", result.Edges)
	}
	if result.Tally.skippedBoundaryNoAllow != 1 {
		t.Fatalf("skippedBoundaryNoAllow = %d, want 1 (tally=%+v)", result.Tally.skippedBoundaryNoAllow, result.Tally)
	}
}

// TestIAMCanPerformNoBoundaryPrincipalUnaffected proves a principal with NO boundary
// statements is evaluated identity-only exactly as before: the edge is present and
// boundary_evaluated is false.
func TestIAMCanPerformNoBoundaryPrincipalUnaffected(t *testing.T) {
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
		t.Fatalf("an unbounded principal must keep the identity-only edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if got, ok := edge["boundary_evaluated"].(bool); !ok || got {
		t.Fatalf("boundary_evaluated = %v, want false for an unbounded principal", edge["boundary_evaluated"])
	}
	if edge["evaluation_scope"] != iamCanPerformEvaluationScopeIdentityPolicyOnly {
		t.Fatalf("evaluation_scope = %v, want %q", edge["evaluation_scope"], iamCanPerformEvaluationScopeIdentityPolicyOnly)
	}
}

// TestIAMCanPerformBoundaryDuplicateAllowIsIdempotent proves duplicate boundary
// Allow statements for the same action/resource do not over-count or change the
// single emitted edge.
func TestIAMCanPerformBoundaryDuplicateAllowIsIdempotent(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	if len(result.Edges) != 1 {
		t.Fatalf("duplicate boundary allows must converge on one edge; got %v", result.Edges)
	}
	if result.Tally.total() != 0 {
		t.Fatalf("a fully-covered bounded edge has no skips; tally=%+v", result.Tally)
	}
}

// TestIAMCanPerformBoundaryDeniesOneActionKeepsOther proves the intersection is
// per-action: a boundary that allows s3:getobject but denies s3:putobject keeps the
// get edge and suppresses the put, both correctly accounted.
func TestIAMCanPerformBoundaryDeniesOneActionKeepsOther(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject", "s3:putobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject", "s3:putobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Deny", []string{"s3:putobject"}, []string{"*"}),
	}

	result := ExtractIAMCanPerformEdges(resources, perms)
	edge := canPerformEdgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), canPerformUID(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN))
	if edge == nil {
		t.Fatalf("the boundary-allowed action must keep its edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
	if got := edge["actions"].([]string); len(got) != 1 || got[0] != "s3:getobject" {
		t.Fatalf("actions = %v, want [s3:getobject] (put suppressed by boundary deny)", got)
	}
	if result.Tally.skippedBoundaryDeny != 1 {
		t.Fatalf("skippedBoundaryDeny = %d, want 1 (tally=%+v)", result.Tally.skippedBoundaryDeny, result.Tally)
	}
}
