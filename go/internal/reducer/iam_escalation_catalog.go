// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "sort"

// iamEscalationTargetKind names which IAM node family a primitive escalates to.
// It selects how the extractor reads the target identity from the contributing
// statement's resources and is documented in
// docs/internal/design/1134-iam-privilege-escalation-catalog.md §3.
type iamEscalationTargetKind string

const (
	// iamEscalationTargetPolicy escalates to the IAM policy named in resources
	// (CreatePolicyVersion / SetDefaultPolicyVersion).
	iamEscalationTargetPolicy iamEscalationTargetKind = "policy"
	// iamEscalationTargetUser escalates to the IAM user named in resources
	// (AttachUserPolicy / PutUserPolicy / CreateAccessKey / login profile).
	iamEscalationTargetUser iamEscalationTargetKind = "user"
	// iamEscalationTargetRole escalates to the IAM role named in resources
	// (AttachRolePolicy / PutRolePolicy / UpdateAssumeRolePolicy).
	iamEscalationTargetRole iamEscalationTargetKind = "role"
	// iamEscalationTargetGroup escalates to the IAM group named in resources
	// (AttachGroupPolicy / PutGroupPolicy / AddUserToGroup).
	iamEscalationTargetGroup iamEscalationTargetKind = "group"
	// iamEscalationTargetPassedRole escalates to the role the principal is allowed
	// to pass; the target identity is read from the iam:passrole statement's
	// resources, not the compute-create action's resources.
	iamEscalationTargetPassedRole iamEscalationTargetKind = "passed_role"
)

// iamEscalationPrimitive is one curated privilege-escalation primitive: the
// closed-vocabulary token written as an edge primitives[] member, the lowercase
// IAM action set that must ALL be present (Allow, not Deny, unconditioned) for
// the primitive to arm, and the IAM node family it escalates to. The action
// tokens are lowercase because aws_iam_permission normalizes actions[] to
// lowercase; IAM actions are case-insensitive so this is loss-free.
type iamEscalationPrimitive struct {
	// Token is the stable primitive identifier carried on the edge.
	Token string
	// Actions is the set of lowercase IAM actions that must all be present for
	// the primitive to arm (multi-action primitives are an AND).
	Actions []string
	// TargetKind selects which IAM node family the primitive escalates to and how
	// the extractor reads the target identity from the contributing statement.
	TargetKind iamEscalationTargetKind
	// PassRoleAction names the action whose statement carries the passed-role
	// target identity for the PassRole family; empty for non-PassRole primitives.
	PassRoleAction string
}

// iamEscalationStsAssumeRoleAction is the sts:AssumeRole action the catalog
// recognizes but never turns into a CAN_ESCALATE_TO edge: role assumption is
// modeled by the separate CAN_ASSUME trust edge (#1134 PR2). The extractor counts
// it under the deferred tally so the deferral is observable, not silent.
const iamEscalationStsAssumeRoleAction = "sts:assumerole"

// iamEscalationPassRoleAction is the inert-alone action every PassRole-family
// primitive requires alongside its compute-create action(s).
const iamEscalationPassRoleAction = "iam:passrole"

// iamEscalationCatalog is the curated, documented set of IAM privilege-escalation
// primitives this slice promotes to CAN_ESCALATE_TO edges. Every entry, its
// required actions, its target, and its citation live in
// docs/internal/design/1134-iam-privilege-escalation-catalog.md §3. The catalog
// is intentionally conservative: only well-known, high-confidence primitives with
// an unambiguous single-node target are included. The general CAN_PERFORM edge
// (any action on any resource) is a deferred follow-up.
//
// It is a package-level value built once; callers MUST NOT mutate it.
var iamEscalationCatalog = []iamEscalationPrimitive{
	// 3.1 single-action policy-mutation primitives.
	{Token: "iam_create_policy_version", Actions: []string{"iam:createpolicyversion"}, TargetKind: iamEscalationTargetPolicy},   // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_set_default_policy_version", Actions: []string{"iam:setdefaultpolicyversion"}, TargetKind: iamEscalationTargetPolicy}, // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_attach_user_policy", Actions: []string{"iam:attachuserpolicy"}, TargetKind: iamEscalationTargetUser},            // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_attach_role_policy", Actions: []string{"iam:attachrolepolicy"}, TargetKind: iamEscalationTargetRole},            // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_attach_group_policy", Actions: []string{"iam:attachgrouppolicy"}, TargetKind: iamEscalationTargetGroup},         // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_put_user_policy", Actions: []string{"iam:putuserpolicy"}, TargetKind: iamEscalationTargetUser},                  // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_put_role_policy", Actions: []string{"iam:putrolepolicy"}, TargetKind: iamEscalationTargetRole},                  // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_put_group_policy", Actions: []string{"iam:putgrouppolicy"}, TargetKind: iamEscalationTargetGroup},               // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_update_assume_role_policy", Actions: []string{"iam:updateassumerolepolicy"}, TargetKind: iamEscalationTargetRole}, // #nosec G101 -- IAM action name string, not a credential
	// 3.2 credential / login primitives (escalate to the target user).
	{Token: "iam_create_access_key", Actions: []string{"iam:createaccesskey"}, TargetKind: iamEscalationTargetUser},              // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_create_login_profile", Actions: []string{"iam:createloginprofile"}, TargetKind: iamEscalationTargetUser},        // #nosec G101 -- IAM action name string, not a credential
	{Token: "iam_update_login_profile", Actions: []string{"iam:updateloginprofile"}, TargetKind: iamEscalationTargetUser},        // #nosec G101 -- IAM action name string, not a credential
	// 3.3 group-membership primitive.
	{Token: "iam_add_user_to_group", Actions: []string{"iam:addusertogroup"}, TargetKind: iamEscalationTargetGroup}, // #nosec G101 -- IAM action name string, not a credential
	// 3.4 PassRole + compute-create primitives (escalate to the passed role).
	{Token: "passrole_lambda", Actions: []string{iamEscalationPassRoleAction, "lambda:createfunction", "lambda:invokefunction"}, TargetKind: iamEscalationTargetPassedRole, PassRoleAction: iamEscalationPassRoleAction},
	{Token: "passrole_ec2", Actions: []string{iamEscalationPassRoleAction, "ec2:runinstances"}, TargetKind: iamEscalationTargetPassedRole, PassRoleAction: iamEscalationPassRoleAction},                                    // #nosec G101 -- IAM action name string, not a credential
	{Token: "passrole_glue_dev_endpoint", Actions: []string{iamEscalationPassRoleAction, "glue:createdevendpoint"}, TargetKind: iamEscalationTargetPassedRole, PassRoleAction: iamEscalationPassRoleAction},               // #nosec G101 -- IAM action name string, not a credential
	{Token: "passrole_cloudformation", Actions: []string{iamEscalationPassRoleAction, "cloudformation:createstack"}, TargetKind: iamEscalationTargetPassedRole, PassRoleAction: iamEscalationPassRoleAction},
	{Token: "passrole_sagemaker_notebook", Actions: []string{iamEscalationPassRoleAction, "sagemaker:createnotebookinstance"}, TargetKind: iamEscalationTargetPassedRole, PassRoleAction: iamEscalationPassRoleAction},
	{Token: "passrole_datapipeline", Actions: []string{iamEscalationPassRoleAction, "datapipeline:createpipeline", "datapipeline:putpipelinedefinition", "datapipeline:activatepipeline"}, TargetKind: iamEscalationTargetPassedRole, PassRoleAction: iamEscalationPassRoleAction},
}

// iamEscalationCatalogActions returns the union of every action any catalog
// primitive requires, plus sts:assumerole, as a set. The extractor uses it to
// decide which Deny statements are "primitive-touching" without scanning the
// whole catalog per statement.
func iamEscalationCatalogActions() map[string]struct{} {
	actions := make(map[string]struct{})
	for _, primitive := range iamEscalationCatalog {
		for _, action := range primitive.Actions {
			actions[action] = struct{}{}
		}
	}
	actions[iamEscalationStsAssumeRoleAction] = struct{}{}
	return actions
}

// sortedPrimitiveTokens returns the tokens deduplicated and sorted so the edge's
// primitives[] property is byte-stable across retries and reprojections, keeping
// the idempotent SET deterministic.
func sortedPrimitiveTokens(tokens map[string]struct{}) []string {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]string, 0, len(tokens))
	for token := range tokens {
		out = append(out, token)
	}
	sort.Strings(out)
	return out
}
