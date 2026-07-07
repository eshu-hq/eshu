// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"

// DecodeAWSIAMTrustPolicy decodes env.Payload into the latest
// secretsiamv1.AWSIAMTrustPolicy struct for the "aws_iam_trust_policy" fact
// kind.
func DecodeAWSIAMTrustPolicy(env Envelope) (secretsiamv1.AWSIAMTrustPolicy, error) {
	return decodeLatestMajor[secretsiamv1.AWSIAMTrustPolicy](FactKindAWSIAMTrustPolicy, env)
}

// EncodeAWSIAMTrustPolicy builds the direct map payload for an
// "aws_iam_trust_policy" fact.
func EncodeAWSIAMTrustPolicy(policy secretsiamv1.AWSIAMTrustPolicy) (map[string]any, error) {
	payload := encodeAWSCommon(
		policy.AccountID,
		policy.Region,
		policy.Provider,
		policy.CollectorInstanceID,
		policy.RedactionPolicyVersion,
	)
	payload["role_arn"] = policy.RoleARN
	payload["policy_source"] = policy.PolicySource
	payload["effect"] = policy.Effect
	addStringPtr(payload, "statement_sid", policy.StatementSID)
	addStringSlice(payload, "actions", policy.Actions)
	addStringSlice(payload, "condition_keys", policy.ConditionKeys)
	addStringSlice(payload, "condition_operators", policy.ConditionOperators)
	addIntPtr(payload, "condition_operator_count", policy.ConditionOperatorCount)
	addStringSlice(payload, "assume_principals", policy.AssumePrincipals)
	addBoolPtr(payload, "has_conditions", policy.HasConditions)
	addStringSlice(payload, "web_identity_subject_fingerprints", policy.WebIdentitySubjectFingerprints)
	addBoolPtr(payload, "web_identity_subject_wildcard", policy.WebIdentitySubjectWildcard)
	return payload, nil
}

// DecodeAWSIAMPermissionPolicy decodes env.Payload into the latest
// secretsiamv1.AWSIAMPermissionPolicy struct for the
// "aws_iam_permission_policy" fact kind.
func DecodeAWSIAMPermissionPolicy(env Envelope) (secretsiamv1.AWSIAMPermissionPolicy, error) {
	return decodeLatestMajor[secretsiamv1.AWSIAMPermissionPolicy](FactKindAWSIAMPermissionPolicy, env)
}

// EncodeAWSIAMPermissionPolicy builds the direct map payload for an
// "aws_iam_permission_policy" fact.
func EncodeAWSIAMPermissionPolicy(policy secretsiamv1.AWSIAMPermissionPolicy) (map[string]any, error) {
	payload := encodeAWSCommon(
		policy.AccountID,
		policy.Region,
		policy.Provider,
		policy.CollectorInstanceID,
		policy.RedactionPolicyVersion,
	)
	payload["principal_arn"] = policy.PrincipalARN
	payload["policy_source"] = policy.PolicySource
	payload["effect"] = policy.Effect
	addStringPtr(payload, "principal_type", policy.PrincipalType)
	addStringPtr(payload, "policy_arn", policy.PolicyARN)
	addStringPtr(payload, "policy_name", policy.PolicyName)
	addStringPtr(payload, "statement_sid", policy.StatementSID)
	addStringSlice(payload, "actions", policy.Actions)
	addStringSlice(payload, "not_actions", policy.NotActions)
	addStringSlice(payload, "resources", policy.Resources)
	addStringSlice(payload, "not_resources", policy.NotResources)
	addStringSlice(payload, "condition_keys", policy.ConditionKeys)
	addStringSlice(payload, "condition_operators", policy.ConditionOperators)
	addIntPtr(payload, "condition_operator_count", policy.ConditionOperatorCount)
	addBoolPtr(payload, "has_conditions", policy.HasConditions)
	addBoolPtr(payload, "is_wildcard_action", policy.IsWildcardAction)
	addBoolPtr(payload, "is_wildcard_resource", policy.IsWildcardResource)
	return payload, nil
}

// DecodeAWSIAMPolicyAttachment decodes an "aws_iam_policy_attachment" payload.
func DecodeAWSIAMPolicyAttachment(env Envelope) (secretsiamv1.AWSIAMPolicyAttachment, error) {
	return decodeLatestMajor[secretsiamv1.AWSIAMPolicyAttachment](FactKindAWSIAMPolicyAttachment, env)
}

// EncodeAWSIAMPolicyAttachment builds the direct map payload for an
// "aws_iam_policy_attachment" fact.
func EncodeAWSIAMPolicyAttachment(attachment secretsiamv1.AWSIAMPolicyAttachment) (map[string]any, error) {
	payload := encodeAWSCommon(
		attachment.AccountID,
		attachment.Region,
		attachment.Provider,
		attachment.CollectorInstanceID,
		attachment.RedactionPolicyVersion,
	)
	payload["principal_arn"] = attachment.PrincipalARN
	payload["policy_arn"] = attachment.PolicyARN
	addStringPtr(payload, "principal_type", attachment.PrincipalType)
	addStringPtr(payload, "policy_name", attachment.PolicyName)
	addStringPtr(payload, "policy_source", attachment.PolicySource)
	return payload, nil
}

// DecodeAWSIAMPermissionBoundary decodes an
// "aws_iam_permission_boundary" payload.
func DecodeAWSIAMPermissionBoundary(env Envelope) (secretsiamv1.AWSIAMPermissionBoundary, error) {
	return decodeLatestMajor[secretsiamv1.AWSIAMPermissionBoundary](FactKindAWSIAMPermissionBoundary, env)
}

// EncodeAWSIAMPermissionBoundary builds the direct map payload for an
// "aws_iam_permission_boundary" fact.
func EncodeAWSIAMPermissionBoundary(boundary secretsiamv1.AWSIAMPermissionBoundary) (map[string]any, error) {
	payload := encodeAWSCommon(
		boundary.AccountID,
		boundary.Region,
		boundary.Provider,
		boundary.CollectorInstanceID,
		boundary.RedactionPolicyVersion,
	)
	payload["principal_arn"] = boundary.PrincipalARN
	payload["boundary_policy_arn"] = boundary.BoundaryPolicyARN
	addStringPtr(payload, "principal_type", boundary.PrincipalType)
	addStringPtr(payload, "boundary_type", boundary.BoundaryType)
	return payload, nil
}

// DecodeAWSIAMInstanceProfile decodes an "aws_iam_instance_profile" payload.
func DecodeAWSIAMInstanceProfile(env Envelope) (secretsiamv1.AWSIAMInstanceProfile, error) {
	return decodeLatestMajor[secretsiamv1.AWSIAMInstanceProfile](FactKindAWSIAMInstanceProfile, env)
}

// EncodeAWSIAMInstanceProfile builds the direct map payload for an
// "aws_iam_instance_profile" fact.
func EncodeAWSIAMInstanceProfile(profile secretsiamv1.AWSIAMInstanceProfile) (map[string]any, error) {
	payload := encodeAWSCommon(
		profile.AccountID,
		profile.Region,
		profile.Provider,
		profile.CollectorInstanceID,
		profile.RedactionPolicyVersion,
	)
	payload["profile_arn"] = profile.ProfileARN
	addStringPtr(payload, "name", profile.Name)
	addStringPtr(payload, "path", profile.Path)
	addStringSlice(payload, "role_arns", profile.RoleARNs)
	addIntPtr(payload, "role_count", profile.RoleCount)
	return payload, nil
}

// DecodeAWSIAMAccessAnalyzerFinding decodes an
// "aws_iam_access_analyzer_finding" payload.
func DecodeAWSIAMAccessAnalyzerFinding(env Envelope) (secretsiamv1.AWSIAMAccessAnalyzerFinding, error) {
	return decodeLatestMajor[secretsiamv1.AWSIAMAccessAnalyzerFinding](FactKindAWSIAMAccessAnalyzerFinding, env)
}

// EncodeAWSIAMAccessAnalyzerFinding builds the direct map payload for an
// "aws_iam_access_analyzer_finding" fact.
func EncodeAWSIAMAccessAnalyzerFinding(finding secretsiamv1.AWSIAMAccessAnalyzerFinding) (map[string]any, error) {
	payload := encodeAWSCommon(
		finding.AccountID,
		finding.Region,
		finding.Provider,
		finding.CollectorInstanceID,
		finding.RedactionPolicyVersion,
	)
	addStringPtr(payload, "finding_id", finding.FindingID)
	addStringPtr(payload, "analyzer_arn", finding.AnalyzerARN)
	addStringPtr(payload, "resource_arn", finding.ResourceARN)
	addStringPtr(payload, "resource_type", finding.ResourceType)
	addStringPtr(payload, "status", finding.Status)
	addStringPtr(payload, "finding_type", finding.FindingType)
	addStringSlice(payload, "condition_keys", finding.ConditionKeys)
	return payload, nil
}

// DecodeGCPIAMPrincipal decodes a "gcp_iam_principal" payload.
func DecodeGCPIAMPrincipal(env Envelope) (secretsiamv1.GCPIAMPrincipal, error) {
	return decodeLatestMajor[secretsiamv1.GCPIAMPrincipal](FactKindGCPIAMPrincipal, env)
}

// EncodeGCPIAMPrincipal builds the direct map payload for a
// "gcp_iam_principal" fact.
func EncodeGCPIAMPrincipal(principal secretsiamv1.GCPIAMPrincipal) (map[string]any, error) {
	payload := encodeGCPCommon(
		principal.Provider,
		principal.CollectorInstanceID,
		principal.RedactionPolicyVersion,
		principal.ProjectID,
		principal.LocationBucket,
	)
	payload["principal_fingerprint"] = principal.PrincipalFingerprint
	payload["principal_type"] = principal.PrincipalType
	addStringPtr(payload, "member_class", principal.MemberClass)
	return payload, nil
}

// DecodeGCPIAMTrustPolicy decodes a "gcp_iam_trust_policy" payload.
func DecodeGCPIAMTrustPolicy(env Envelope) (secretsiamv1.GCPIAMTrustPolicy, error) {
	return decodeLatestMajor[secretsiamv1.GCPIAMTrustPolicy](FactKindGCPIAMTrustPolicy, env)
}

// EncodeGCPIAMTrustPolicy builds the direct map payload for a
// "gcp_iam_trust_policy" fact.
func EncodeGCPIAMTrustPolicy(policy secretsiamv1.GCPIAMTrustPolicy) (map[string]any, error) {
	payload := encodeGCPCommon(
		policy.Provider,
		policy.CollectorInstanceID,
		policy.RedactionPolicyVersion,
		policy.ProjectID,
		policy.LocationBucket,
	)
	payload["target_principal_fingerprint"] = policy.TargetPrincipalFingerprint
	payload["target_service_account_email_digest"] = policy.TargetServiceAccountEmailDigest
	payload["role"] = policy.Role
	payload["impersonation_mode"] = policy.ImpersonationMode
	addStringPtr(payload, "target_service_account_cloud_resource_uid", policy.TargetServiceAccountCloudResourceUID)
	addStringPtr(payload, "trusted_member_fingerprint", policy.TrustedMemberFingerprint)
	addStringPtr(payload, "trusted_member_class", policy.TrustedMemberClass)
	addStringPtr(payload, "gcp_workload_identity_subject_fingerprint", policy.GCPWorkloadIdentitySubjectFingerprint)
	addStringPtr(payload, "gcp_workload_identity_member_class", policy.GCPWorkloadIdentityMemberClass)
	addBoolPtr(payload, "condition_present", policy.ConditionPresent)
	addStringPtr(payload, "condition_fingerprint", policy.ConditionFingerprint)
	return payload, nil
}

// DecodeGCPIAMPermissionPolicy decodes a "gcp_iam_permission_policy" payload.
func DecodeGCPIAMPermissionPolicy(env Envelope) (secretsiamv1.GCPIAMPermissionPolicy, error) {
	return decodeLatestMajor[secretsiamv1.GCPIAMPermissionPolicy](FactKindGCPIAMPermissionPolicy, env)
}

// EncodeGCPIAMPermissionPolicy builds the direct map payload for a
// "gcp_iam_permission_policy" fact.
func EncodeGCPIAMPermissionPolicy(policy secretsiamv1.GCPIAMPermissionPolicy) (map[string]any, error) {
	payload := encodeGCPCommon(
		policy.Provider,
		policy.CollectorInstanceID,
		policy.RedactionPolicyVersion,
		policy.ProjectID,
		policy.LocationBucket,
	)
	payload["principal_fingerprint"] = policy.PrincipalFingerprint
	payload["principal_type"] = policy.PrincipalType
	payload["role"] = policy.Role
	payload["resource_full_resource_name"] = policy.ResourceFullName
	addStringPtr(payload, "resource_asset_type", policy.ResourceAssetType)
	addBoolPtr(payload, "resource_is_secret", policy.ResourceIsSecret)
	addBoolPtr(payload, "broad_role", policy.BroadRole)
	addBoolPtr(payload, "condition_present", policy.ConditionPresent)
	addStringPtr(payload, "condition_fingerprint", policy.ConditionFingerprint)
	return payload, nil
}

func encodeAWSCommon(accountID, region, provider, collectorInstanceID, redactionPolicyVersion string) map[string]any {
	return map[string]any{
		"account_id":               accountID,
		"region":                   region,
		"provider":                 provider,
		"collector_instance_id":    collectorInstanceID,
		"redaction_policy_version": redactionPolicyVersion,
	}
}

func encodeGCPCommon(
	provider string,
	collectorInstanceID string,
	redactionPolicyVersion string,
	projectID *string,
	locationBucket *string,
) map[string]any {
	payload := map[string]any{
		"provider":                 provider,
		"collector_instance_id":    collectorInstanceID,
		"redaction_policy_version": redactionPolicyVersion,
	}
	addStringPtr(payload, "project_id", projectID)
	addStringPtr(payload, "location_bucket", locationBucket)
	return payload
}
