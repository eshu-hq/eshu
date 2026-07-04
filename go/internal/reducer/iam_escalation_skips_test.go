// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestIAMEscalationUnscannedTargetIsSkipped proves a target ARN that was not
// scanned (including cross-account) produces no edge, counted skipped_unresolved.
func TestIAMEscalationUnscannedTargetIsSkipped(t *testing.T) {
	t.Parallel()

	// Target policy is NOT in the resource set, and a cross-account policy too.
	crossAccountPolicy := "arn:aws:iam::999988887777:policy/foreign"
	resources := []facts.Envelope{attackerNode()}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow",
		[]string{"iam:createpolicyversion"}, []string{crossAccountPolicy})}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("unscanned/cross-account target must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedUnresolved != 1 {
		t.Fatalf("skippedUnresolved = %d, want 1 (tally=%+v)", result.Tally.skippedUnresolved, result.Tally)
	}
}

// TestIAMEscalationStsAssumeRoleIsDeferred proves sts:AssumeRole is recognized and
// deferred to CAN_ASSUME — no CAN_ESCALATE_TO edge, counted deferred_can_assume.
func TestIAMEscalationStsAssumeRoleIsDeferred(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(iamResourceTypeRole, targetRoleARN)}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow",
		[]string{"sts:assumerole"}, []string{targetRoleARN})}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("sts:AssumeRole must not produce a CAN_ESCALATE_TO edge; got %v", result.Edges)
	}
	if result.Tally.deferredCanAssume != 1 {
		t.Fatalf("deferredCanAssume = %d, want 1 (tally=%+v)", result.Tally.deferredCanAssume, result.Tally)
	}
}

// TestIAMEscalationSelfEscalationDropped proves a principal escalating to itself
// (CreateAccessKey on self) is dropped without a skip count and without an edge.
func TestIAMEscalationSelfEscalationDropped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode()}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow",
		[]string{"iam:createaccesskey"}, []string{attackerUserARN})}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("self-escalation must not produce an edge; got %v", result.Edges)
	}
}

// TestIAMEscalationUnscannedPrincipalIsSkipped proves a principal that was not
// scanned has no source node and emits no edge, counted skipped_unresolved once.
func TestIAMEscalationUnscannedPrincipalIsSkipped(t *testing.T) {
	t.Parallel()

	// Only the target is scanned, not the principal.
	resources := []facts.Envelope{iamNodeEnvelope(iamResourceTypePolicy, targetPolicyARN)}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow",
		[]string{"iam:createpolicyversion"}, []string{targetPolicyARN})}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("unscanned principal must not produce an edge; got %v", result.Edges)
	}
	if result.Tally.skippedUnresolved != 1 {
		t.Fatalf("skippedUnresolved = %d, want 1 (tally=%+v)", result.Tally.skippedUnresolved, result.Tally)
	}
}

// TestIAMEscalationMultiplePrimitivesMergeIntoOneEdge proves two primitives that
// reach the same (principal, target) converge on ONE edge with a sorted merged
// primitives list (the keying decision from catalog doc §5).
func TestIAMEscalationMultiplePrimitivesMergeIntoOneEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(iamResourceTypeRole, targetRoleARN)}
	// Both AttachRolePolicy and PutRolePolicy target the same role.
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:attachrolepolicy", "iam:putrolepolicy"}, []string{targetRoleARN}),
	}
	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	edge := edgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), uidOf(iamResourceTypeRole, targetRoleARN))
	if edge == nil {
		t.Fatalf("expected one merged edge; rows=%v", result.Edges)
	}
	got := edge["primitives"].([]string)
	want := []string{"iam_attach_role_policy", "iam_put_role_policy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged primitives = %v, want %v (sorted, deduped)", got, want)
	}
	if edge["primitive_count"].(int) != 2 {
		t.Fatalf("primitive_count = %v, want 2", edge["primitive_count"])
	}
	if len(result.Edges) != 1 {
		t.Fatalf("two primitives to one target must merge to ONE edge; got %d", len(result.Edges))
	}
}

// TestIAMEscalationServiceWildcardCoversAction proves iam:* (a service wildcard)
// arms an iam: primitive — the conservative wildcard expansion of catalog §2.
func TestIAMEscalationServiceWildcardCoversAction(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(iamResourceTypeRole, targetRoleARN)}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:*"}, []string{targetRoleARN})}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	edge := edgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), uidOf(iamResourceTypeRole, targetRoleARN))
	if edge == nil {
		t.Fatalf("iam:* must arm iam: role primitives; rows=%v", result.Edges)
	}
	// iam:* covers attachrolepolicy, putrolepolicy, updateassumerolepolicy -> all
	// resolve to the same role -> merged into one edge.
	got := edge["primitives"].([]string)
	want := []string{"iam_attach_role_policy", "iam_put_role_policy", "iam_update_assume_role_policy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("iam:* role primitives = %v, want %v", got, want)
	}
}

// TestIAMEscalationDeterministicAndIdempotent proves the same input yields a
// byte-stable row set (idempotent reproject) regardless of input ordering.
func TestIAMEscalationDeterministicAndIdempotent(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		attackerNode(),
		iamNodeEnvelope(iamResourceTypePolicy, targetPolicyARN),
		iamNodeEnvelope(iamResourceTypeRole, targetRoleARN),
	}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:createpolicyversion"}, []string{targetPolicyARN}),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:putrolepolicy"}, []string{targetRoleARN}),
	}
	firstResult, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	first := firstResult.Edges
	secondResult, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	second := secondResult.Edges
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("extraction is not deterministic:\n%v\n%v", first, second)
	}
	if len(first) != 2 {
		t.Fatalf("len(edges) = %d, want 2", len(first))
	}
}

// TestIAMEscalationEmptyInputIsNoOp proves empty permission facts produce no edges
// and no panic (empty/stale state).
func TestIAMEscalationEmptyInputIsNoOp(t *testing.T) {
	t.Parallel()

	result, err := ExtractIAMEscalationEdges(nil, nil)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 || result.Tally.total() != 0 {
		t.Fatalf("empty input must be a no-op; got %+v", result)
	}
}
