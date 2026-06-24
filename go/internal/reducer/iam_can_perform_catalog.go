// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "sort"

// CAN_PERFORM target resource_type tokens the catalog maps actions to. They
// mirror the awscloud collector's ResourceType* constants (the reducer must not
// import the collector package, so the tokens are duplicated here on purpose, the
// same way iam_escalation_target.go duplicates the IAM resource types). The
// resolver requires a matched scanned node's ARN to classify as the catalog
// entry's expected type so, e.g., an s3:GetObject grant never resolves to a KMS
// key node that happens to share a glob.
const (
	iamCanPerformResourceTypeS3Bucket    = "aws_s3_bucket"
	iamCanPerformResourceTypeKMSKey      = "aws_kms_key"
	iamCanPerformResourceTypeSecret      = "aws_secretsmanager_secret"
	iamCanPerformResourceTypeSSMParam    = "aws_ssm_parameter"
	iamCanPerformResourceTypeDynamoDB    = "aws_dynamodb_table"
	iamCanPerformResourceTypeEC2Instance = "aws_ec2_instance"
	iamCanPerformResourceTypeRDSInstance = "aws_rds_db_instance"
	iamCanPerformResourceTypeLambdaFunc  = "aws_lambda_function"
)

const (
	// iamCanPerformGrantSourceIdentityPolicy marks actions granted by principal
	// identity-policy facts (`aws_iam_permission`).
	iamCanPerformGrantSourceIdentityPolicy = "identity_policy"
	// iamCanPerformGrantSourceResourcePolicy marks actions granted by
	// resource-policy facts (`aws_resource_policy_permission`).
	iamCanPerformGrantSourceResourcePolicy = "resource_policy"
)

const (
	// iamCanPerformEvaluationScopeIdentityPolicyOnly documents that the edge was
	// derived from identity-policy statements only: resource-based policies,
	// permission boundaries, SCPs, condition values, and session policies were not
	// evaluated.
	iamCanPerformEvaluationScopeIdentityPolicyOnly = "identity_policy_only"
	// iamCanPerformEvaluationScopeResourcePolicyOnly documents that the edge was
	// derived from resource-policy statements only: identity policies, permission
	// boundaries, SCPs, condition values, and session policies were not evaluated.
	iamCanPerformEvaluationScopeResourcePolicyOnly = "resource_policy_only"
	// iamCanPerformEvaluationScopeIdentityAndResourcePolicy documents that both
	// identity-policy and resource-policy statements grant at least one action on
	// the same principal/resource edge.
	iamCanPerformEvaluationScopeIdentityAndResourcePolicy = "identity_and_resource_policy"
	// iamCanPerformEvaluationScopeIdentityPolicyWithPermissionBoundary documents
	// that identity-policy grants were intersected with a permission-boundary
	// policy before the edge was emitted.
	iamCanPerformEvaluationScopeIdentityPolicyWithPermissionBoundary = "identity_policy_with_permission_boundary"
	// iamCanPerformEvaluationScopeIdentityAndResourcePolicyWithPermissionBoundary
	// documents that identity-policy grants were boundary-evaluated and a
	// resource-policy grant also contributes at least one action on the edge.
	iamCanPerformEvaluationScopeIdentityAndResourcePolicyWithPermissionBoundary = "identity_and_resource_policy_with_permission_boundary"
)

const (
	iamCanPerformPolicySourceInline             = "inline"
	iamCanPerformPolicySourceAttachedManaged    = "attached_managed"
	iamCanPerformPolicySourcePermissionBoundary = "permission_boundary"
)

// iamCanPerformEvaluationScope is the legacy MVP honesty label kept for tests and
// identity-only callers. New rows derive the scope from grant_sources.
const iamCanPerformEvaluationScope = iamCanPerformEvaluationScopeIdentityPolicyOnly

// iamCanPerformAction is one curated, reviewed, high-value sensitive IAM action
// the CAN_PERFORM MVP promotes to an edge. The action token is lowercase because
// aws_iam_permission normalizes actions[] to lowercase; IAM actions are
// case-insensitive so this is loss-free. ExpectedResourceType is the resource_type
// the resolver requires the matched scanned node to be, which bounds cardinality
// and prevents a cross-type glob match from fabricating a wrong edge.
type iamCanPerformAction struct {
	// Action is the lowercase IAM action this catalog entry recognizes.
	Action string
	// ExpectedResourceType is the resource_type a matched scanned node must
	// classify as for the resolver to accept it as this action's target.
	ExpectedResourceType string
}

// iamCanPerformCatalog is the CLOSED, curated, reviewable set of sensitive IAM
// actions the CAN_PERFORM MVP resolves to edges. It is intentionally small and
// high-value (design §3.1): every entry is a well-known data-access or
// destructive action whose target resource type is unambiguous, so the resolver
// can require the matched node be the right type. Actions NOT in this catalog are
// counted skipped_uncatalogued_action, never silently dropped. The vocabulary is
// expandable in a later phase (design §8 PR4e) under the same security review;
// adding an entry is a security-sensitive change, not a style nit.
//
// It is a package-level value built once; callers MUST NOT mutate it.
var iamCanPerformCatalog = []iamCanPerformAction{
	// S3 object/bucket data-plane and destructive actions.
	{Action: "s3:getobject", ExpectedResourceType: iamCanPerformResourceTypeS3Bucket},
	{Action: "s3:putobject", ExpectedResourceType: iamCanPerformResourceTypeS3Bucket},
	{Action: "s3:deletebucket", ExpectedResourceType: iamCanPerformResourceTypeS3Bucket},
	{Action: "s3:listbucket", ExpectedResourceType: iamCanPerformResourceTypeS3Bucket},
	// KMS data-at-rest exposure and data-key issuance.
	{Action: "kms:decrypt", ExpectedResourceType: iamCanPerformResourceTypeKMSKey},
	{Action: "kms:generatedatakey", ExpectedResourceType: iamCanPerformResourceTypeKMSKey},
	// Secret / parameter exfiltration or value mutation.
	{Action: "secretsmanager:getsecretvalue", ExpectedResourceType: iamCanPerformResourceTypeSecret},
	{Action: "secretsmanager:putsecretvalue", ExpectedResourceType: iamCanPerformResourceTypeSecret},
	{Action: "ssm:getparameter", ExpectedResourceType: iamCanPerformResourceTypeSSMParam},
	{Action: "ssm:getparameters", ExpectedResourceType: iamCanPerformResourceTypeSSMParam},
	// DynamoDB item/table read and mutation.
	{Action: "dynamodb:getitem", ExpectedResourceType: iamCanPerformResourceTypeDynamoDB},
	{Action: "dynamodb:query", ExpectedResourceType: iamCanPerformResourceTypeDynamoDB},
	{Action: "dynamodb:scan", ExpectedResourceType: iamCanPerformResourceTypeDynamoDB},
	{Action: "dynamodb:putitem", ExpectedResourceType: iamCanPerformResourceTypeDynamoDB},
	{Action: "dynamodb:updateitem", ExpectedResourceType: iamCanPerformResourceTypeDynamoDB},
	{Action: "dynamodb:deleteitem", ExpectedResourceType: iamCanPerformResourceTypeDynamoDB},
	// Destructive or workload-executing compute / database actions.
	{Action: "ec2:terminateinstances", ExpectedResourceType: iamCanPerformResourceTypeEC2Instance},
	{Action: "ec2:stopinstances", ExpectedResourceType: iamCanPerformResourceTypeEC2Instance},
	{Action: "rds:deletedbinstance", ExpectedResourceType: iamCanPerformResourceTypeRDSInstance},
	{Action: "rds:stopdbinstance", ExpectedResourceType: iamCanPerformResourceTypeRDSInstance},
	{Action: "lambda:invokefunction", ExpectedResourceType: iamCanPerformResourceTypeLambdaFunc},
}

// iamCanPerformCatalogByAction indexes the catalog by lowercase action so the
// extractor can look an action up in O(1) and decide whether it is in the closed
// vocabulary without scanning the catalog per statement. Built once per call from
// the immutable package-level catalog.
func iamCanPerformCatalogByAction() map[string]iamCanPerformAction {
	out := make(map[string]iamCanPerformAction, len(iamCanPerformCatalog))
	for _, entry := range iamCanPerformCatalog {
		out[entry.Action] = entry
	}
	return out
}

// iamCanPerformCatalogActions returns the set of every catalog action so the
// Deny / conditioned-skip accounting can decide whether an untrusted statement is
// catalog-relevant without scanning the catalog slice per statement.
func iamCanPerformCatalogActions() map[string]struct{} {
	out := make(map[string]struct{}, len(iamCanPerformCatalog))
	for _, entry := range iamCanPerformCatalog {
		out[entry.Action] = struct{}{}
	}
	return out
}

// iamCanPerformCatalogActionsFromCatalog derives the statement-matching action
// set from the caller's catalog map so tests and future narrowed catalogs do not
// accidentally consult the package-global vocabulary.
func iamCanPerformCatalogActionsFromCatalog(catalog map[string]iamCanPerformAction) map[string]struct{} {
	out := make(map[string]struct{}, len(catalog))
	for action, entry := range catalog {
		if entry.Action != "" {
			out[entry.Action] = struct{}{}
			continue
		}
		out[action] = struct{}{}
	}
	return out
}

// sortedCanPerformActions returns the granted action tokens deduplicated and
// sorted so the edge's rel.actions property is byte-stable across retries and
// reprojections, keeping the idempotent SET deterministic.
func sortedCanPerformActions(actions map[string]struct{}) []string {
	return sortedCanPerformStringSet(actions)
}

func sortedCanPerformStringSet(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
