// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// iamEscAccount and iamEscRegion are the fixed scope every fixture principal and
// target share so the in-memory join index resolves them under the trust
// boundary. IAM ARNs are global (empty region) on the AWS side, but the node uid
// folds region, so the fixtures use a stable region to key consistently.
const (
	iamEscAccount = "111122223333"
	iamEscRegion  = ""
)

// iamNodeEnvelope builds an aws_resource fact for an IAM node (principal or
// target) so the extractor resolves it to a CloudResource uid through the shared
// join index. The ARN is used as both resource_id and arn so byARN resolves it.
func iamNodeEnvelope(resourceType, arn string) facts.Envelope {
	return resourceEnvelope(iamEscAccount, iamEscRegion, resourceType, arn, arn)
}

// escalationPermissionEnvelope builds one merged aws_iam_permission statement
// fact (actions lowercase, mirroring PR1). Named distinctly from the sibling
// can_assume test helper iamPermissionEnvelope so both IAM edge domains coexist.
func escalationPermissionEnvelope(principalARN, effect string, actions, resources []string, opts ...func(map[string]any)) facts.Envelope {
	payload := map[string]any{
		"account_id":           iamEscAccount,
		"region":               iamEscRegion,
		"principal_arn":        principalARN,
		"principal_type":       "user",
		"policy_source":        "inline",
		"effect":               effect,
		"actions":              toAnySlice(actions),
		"not_actions":          []any{},
		"resources":            toAnySlice(resources),
		"not_resources":        []any{},
		"condition_keys":       []any{},
		"assume_principals":    []any{},
		"has_conditions":       false,
		"is_wildcard_action":   containsAny(actions, "*"),
		"is_wildcard_resource": containsAny(resources, "*"),
	}
	for _, opt := range opts {
		opt(payload)
	}
	return facts.Envelope{FactKind: facts.AWSIAMPermissionFactKind, Payload: payload}
}

func withConditions() func(map[string]any) {
	return func(p map[string]any) {
		p["condition_keys"] = []any{"aws:MultiFactorAuthPresent"}
		p["has_conditions"] = true
	}
}

func withNotActions(notActions ...string) func(map[string]any) {
	return func(p map[string]any) { p["not_actions"] = toAnySlice(notActions) }
}

func toAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

func containsAny(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// arns the fixtures reuse.
const (
	attackerUserARN = "arn:aws:iam::111122223333:user/attacker"
	victimUserARN   = "arn:aws:iam::111122223333:user/victim"
	targetPolicyARN = "arn:aws:iam::111122223333:policy/team-policy"
	targetRoleARN   = "arn:aws:iam::111122223333:role/exec-role"
	targetGroupARN  = "arn:aws:iam::111122223333:group/admins"
)

func attackerNode() facts.Envelope { return iamNodeEnvelope(iamResourceTypeUser, attackerUserARN) }

// edgeFor returns the single edge row for a (principal,target) pair, or nil.
func edgeFor(rows []map[string]any, principalUID, targetUID string) map[string]any {
	for _, row := range rows {
		if row["principal_uid"] == principalUID && row["target_uid"] == targetUID {
			return row
		}
	}
	return nil
}

func uidOf(resourceType, arn string) string {
	return cloudResourceUID(iamEscAccount, iamEscRegion, resourceType, arn)
}

// TestIAMEscalationSingleActionPrimitivesEmitEdge proves each single-action
// policy-mutation primitive resolves to an edge to the right target node with the
// right primitive token. This is the positive case of the proof matrix.
func TestIAMEscalationSingleActionPrimitivesEmitEdge(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		action    string
		target    string
		targetTyp string
		primitive string
	}{
		{"create policy version", "iam:createpolicyversion", targetPolicyARN, iamResourceTypePolicy, "iam_create_policy_version"},
		{"set default policy version", "iam:setdefaultpolicyversion", targetPolicyARN, iamResourceTypePolicy, "iam_set_default_policy_version"},
		{"attach user policy", "iam:attachuserpolicy", victimUserARN, iamResourceTypeUser, "iam_attach_user_policy"},
		{"attach role policy", "iam:attachrolepolicy", targetRoleARN, iamResourceTypeRole, "iam_attach_role_policy"},
		{"attach group policy", "iam:attachgrouppolicy", targetGroupARN, iamResourceTypeGroup, "iam_attach_group_policy"},
		{"put user policy", "iam:putuserpolicy", victimUserARN, iamResourceTypeUser, "iam_put_user_policy"},
		{"put role policy", "iam:putrolepolicy", targetRoleARN, iamResourceTypeRole, "iam_put_role_policy"},
		{"put group policy", "iam:putgrouppolicy", targetGroupARN, iamResourceTypeGroup, "iam_put_group_policy"},
		{"update assume role policy", "iam:updateassumerolepolicy", targetRoleARN, iamResourceTypeRole, "iam_update_assume_role_policy"},
		{"create access key", "iam:createaccesskey", victimUserARN, iamResourceTypeUser, "iam_create_access_key"},
		{"create login profile", "iam:createloginprofile", victimUserARN, iamResourceTypeUser, "iam_create_login_profile"},
		{"update login profile", "iam:updateloginprofile", victimUserARN, iamResourceTypeUser, "iam_update_login_profile"},
		{"add user to group", "iam:addusertogroup", targetGroupARN, iamResourceTypeGroup, "iam_add_user_to_group"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(tc.targetTyp, tc.target)}
			perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow", []string{tc.action}, []string{tc.target})}

			result, err := ExtractIAMEscalationEdges(resources, perms)
			if err != nil {
				t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
			}
			edge := edgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), uidOf(tc.targetTyp, tc.target))
			if edge == nil {
				t.Fatalf("expected one CAN_ESCALATE_TO edge for %s, got rows=%v skips=%d", tc.action, result.Edges, result.Tally.total())
			}
			got := edge["primitives"].([]string)
			if len(got) != 1 || got[0] != tc.primitive {
				t.Fatalf("primitives = %v, want [%s]", got, tc.primitive)
			}
		})
	}
}

// TestIAMEscalationMultiActionRequiresAllActions proves a PassRole-family
// primitive arms ONLY when every required action is present. lambda needs
// passrole + createfunction + invokefunction; two of three must not arm.
func TestIAMEscalationMultiActionRequiresAllActions(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(iamResourceTypeRole, targetRoleARN)}

	// Only two of the three lambda actions: must NOT produce an edge.
	partial := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:passrole", "lambda:createfunction"}, []string{targetRoleARN}),
	}
	partialResult, err := ExtractIAMEscalationEdges(resources, partial)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if rows := partialResult.Edges; len(rows) != 0 {
		t.Fatalf("incomplete lambda primitive must not arm; got %v", rows)
	}

	// All three present (passrole carries the role resource): one edge.
	complete := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:passrole"}, []string{targetRoleARN}),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"lambda:createfunction", "lambda:invokefunction"}, []string{"*"}),
	}
	result, err := ExtractIAMEscalationEdges(resources, complete)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	edge := edgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), uidOf(iamResourceTypeRole, targetRoleARN))
	if edge == nil {
		t.Fatalf("complete lambda primitive must arm; rows=%v", result.Edges)
	}
	if got := edge["primitives"].([]string); len(got) != 1 || got[0] != "passrole_lambda" {
		t.Fatalf("primitives = %v, want [passrole_lambda]", got)
	}
}

// TestIAMEscalationWildcardResourceIsSkippedAmbiguous proves a dangerous action
// on Resource:"*" is recorded skipped-ambiguous, NOT promoted to an edge.
func TestIAMEscalationWildcardResourceIsSkippedAmbiguous(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(iamResourceTypePolicy, targetPolicyARN)}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:createpolicyversion"}, []string{"*"})}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("wildcard resource must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedAmbiguous != 1 {
		t.Fatalf("skippedAmbiguous = %d, want 1 (tally=%+v)", result.Tally.skippedAmbiguous, result.Tally)
	}
}

// TestIAMEscalationManyMatchingTargetsIsAmbiguous proves a glob that matches more
// than one scanned node is ambiguous, not an edge.
func TestIAMEscalationManyMatchingTargetsIsAmbiguous(t *testing.T) {
	t.Parallel()

	policyA := "arn:aws:iam::111122223333:policy/team-a"
	policyB := "arn:aws:iam::111122223333:policy/team-b"
	resources := []facts.Envelope{
		attackerNode(),
		iamNodeEnvelope(iamResourceTypePolicy, policyA),
		iamNodeEnvelope(iamResourceTypePolicy, policyB),
	}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow",
		[]string{"iam:createpolicyversion"}, []string{"arn:aws:iam::111122223333:policy/team-*"})}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("glob matching many nodes must be ambiguous; got %v", result.Edges)
	}
	if result.Tally.skippedAmbiguous != 1 {
		t.Fatalf("skippedAmbiguous = %d, want 1", result.Tally.skippedAmbiguous)
	}
}

// TestIAMEscalationSingleGlobMatchEmitsEdge proves a glob that matches EXACTLY one
// scanned node resolves to a confident edge.
func TestIAMEscalationSingleGlobMatchEmitsEdge(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(iamResourceTypePolicy, targetPolicyARN)}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow",
		[]string{"iam:createpolicyversion"}, []string{"arn:aws:iam::111122223333:policy/team-*"})}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if edge := edgeFor(result.Edges, uidOf(iamResourceTypeUser, attackerUserARN), uidOf(iamResourceTypePolicy, targetPolicyARN)); edge == nil {
		t.Fatalf("single glob match must emit an edge; rows=%v tally=%+v", result.Edges, result.Tally)
	}
}

// TestIAMEscalationDenyIsSkipped proves a Deny on a primitive's action removes
// the principal from that primitive (no edge), counted skipped_deny.
func TestIAMEscalationDenyIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(iamResourceTypePolicy, targetPolicyARN)}
	perms := []facts.Envelope{
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:createpolicyversion"}, []string{targetPolicyARN}),
		escalationPermissionEnvelope(attackerUserARN, "Deny", []string{"iam:createpolicyversion"}, []string{"*"}),
	}
	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("Deny must block the primitive; got %v", result.Edges)
	}
	if result.Tally.skippedDeny != 1 {
		t.Fatalf("skippedDeny = %d, want 1 (tally=%+v)", result.Tally.skippedDeny, result.Tally)
	}
}

// TestIAMEscalationConditionedStatementIsSkipped proves a condition-gated grant is
// not trusted (no edge), counted skipped_conditioned.
func TestIAMEscalationConditionedStatementIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(iamResourceTypePolicy, targetPolicyARN)}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow",
		[]string{"iam:createpolicyversion"}, []string{targetPolicyARN}, withConditions())}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("conditioned grant must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedConditioned != 1 {
		t.Fatalf("skippedConditioned = %d, want 1 (tally=%+v)", result.Tally.skippedConditioned, result.Tally)
	}
}

// TestIAMEscalationNotActionStatementIsSkipped proves a NotAction-bearing grant is
// not trusted, counted skipped_not_action_resource.
func TestIAMEscalationNotActionStatementIsSkipped(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{attackerNode(), iamNodeEnvelope(iamResourceTypePolicy, targetPolicyARN)}
	perms := []facts.Envelope{escalationPermissionEnvelope(attackerUserARN, "Allow",
		[]string{"iam:createpolicyversion"}, []string{targetPolicyARN}, withNotActions("iam:deleteuser"))}

	result, err := ExtractIAMEscalationEdges(resources, perms)
	if err != nil {
		t.Fatalf("ExtractIAMEscalationEdges() error = %v, want nil", err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("NotAction grant must not become an edge; got %v", result.Edges)
	}
	if result.Tally.skippedNotActionResource != 1 {
		t.Fatalf("skippedNotActionResource = %d, want 1 (tally=%+v)", result.Tally.skippedNotActionResource, result.Tally)
	}
}
