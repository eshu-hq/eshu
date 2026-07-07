// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// GCPCommon carries the redaction-safe common payload fields shared by GCP IAM
// source facts in the secrets_iam family.
type GCPCommon struct {
	Provider               string  `json:"provider"`
	ProjectID              *string `json:"project_id,omitempty"`
	LocationBucket         *string `json:"location_bucket,omitempty"`
	CollectorInstanceID    string  `json:"collector_instance_id"`
	RedactionPolicyVersion string  `json:"redaction_policy_version"`
}

// GCPIAMPrincipal is the schema-version-1 payload for a "gcp_iam_principal"
// source fact.
type GCPIAMPrincipal struct {
	Provider               string  `json:"provider"`
	CollectorInstanceID    string  `json:"collector_instance_id"`
	RedactionPolicyVersion string  `json:"redaction_policy_version"`
	PrincipalFingerprint   string  `json:"principal_fingerprint"`
	PrincipalType          string  `json:"principal_type"`
	ProjectID              *string `json:"project_id,omitempty"`
	LocationBucket         *string `json:"location_bucket,omitempty"`
	MemberClass            *string `json:"member_class,omitempty"`
}

// GCPIAMTrustPolicy is the schema-version-1 payload for a
// "gcp_iam_trust_policy" source fact.
type GCPIAMTrustPolicy struct {
	Provider                              string  `json:"provider"`
	CollectorInstanceID                   string  `json:"collector_instance_id"`
	RedactionPolicyVersion                string  `json:"redaction_policy_version"`
	TargetPrincipalFingerprint            string  `json:"target_principal_fingerprint"`
	TargetServiceAccountEmailDigest       string  `json:"target_service_account_email_digest"`
	Role                                  string  `json:"role"`
	ImpersonationMode                     string  `json:"impersonation_mode"`
	ProjectID                             *string `json:"project_id,omitempty"`
	LocationBucket                        *string `json:"location_bucket,omitempty"`
	TargetServiceAccountCloudResourceUID  *string `json:"target_service_account_cloud_resource_uid,omitempty"`
	TrustedMemberFingerprint              *string `json:"trusted_member_fingerprint,omitempty"`
	TrustedMemberClass                    *string `json:"trusted_member_class,omitempty"`
	GCPWorkloadIdentitySubjectFingerprint *string `json:"gcp_workload_identity_subject_fingerprint,omitempty"`
	GCPWorkloadIdentityMemberClass        *string `json:"gcp_workload_identity_member_class,omitempty"`
	ConditionPresent                      *bool   `json:"condition_present,omitempty"`
	ConditionFingerprint                  *string `json:"condition_fingerprint,omitempty"`
}

// GCPIAMPermissionPolicy is the schema-version-1 payload for a
// "gcp_iam_permission_policy" source fact.
type GCPIAMPermissionPolicy struct {
	Provider               string  `json:"provider"`
	CollectorInstanceID    string  `json:"collector_instance_id"`
	RedactionPolicyVersion string  `json:"redaction_policy_version"`
	PrincipalFingerprint   string  `json:"principal_fingerprint"`
	PrincipalType          string  `json:"principal_type"`
	Role                   string  `json:"role"`
	ResourceFullName       string  `json:"resource_full_resource_name"`
	ProjectID              *string `json:"project_id,omitempty"`
	LocationBucket         *string `json:"location_bucket,omitempty"`
	ResourceAssetType      *string `json:"resource_asset_type,omitempty"`
	ResourceIsSecret       *bool   `json:"resource_is_secret,omitempty"`
	BroadRole              *bool   `json:"broad_role,omitempty"`
	ConditionPresent       *bool   `json:"condition_present,omitempty"`
	ConditionFingerprint   *string `json:"condition_fingerprint,omitempty"`
}
