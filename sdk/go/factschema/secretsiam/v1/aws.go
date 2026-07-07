// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// AWSCommon carries the redaction-safe common payload fields shared by AWS IAM
// source facts in the secrets_iam family.
type AWSCommon struct {
	AccountID              string `json:"account_id"`
	Region                 string `json:"region"`
	Provider               string `json:"provider"`
	CollectorInstanceID    string `json:"collector_instance_id"`
	RedactionPolicyVersion string `json:"redaction_policy_version"`
}

// AWSIAMTrustPolicy is the schema-version-1 payload for an
// "aws_iam_trust_policy" source fact.
type AWSIAMTrustPolicy struct {
	AccountID                      string   `json:"account_id"`
	Region                         string   `json:"region"`
	Provider                       string   `json:"provider"`
	CollectorInstanceID            string   `json:"collector_instance_id"`
	RedactionPolicyVersion         string   `json:"redaction_policy_version"`
	RoleARN                        string   `json:"role_arn"`
	PolicySource                   string   `json:"policy_source"`
	Effect                         string   `json:"effect"`
	StatementSID                   *string  `json:"statement_sid,omitempty"`
	Actions                        []string `json:"actions,omitempty"`
	ConditionKeys                  []string `json:"condition_keys,omitempty"`
	ConditionOperators             []string `json:"condition_operators,omitempty"`
	ConditionOperatorCount         *int     `json:"condition_operator_count,omitempty"`
	AssumePrincipals               []string `json:"assume_principals,omitempty"`
	HasConditions                  *bool    `json:"has_conditions,omitempty"`
	WebIdentitySubjectFingerprints []string `json:"web_identity_subject_fingerprints,omitempty"`
	WebIdentitySubjectWildcard     *bool    `json:"web_identity_subject_wildcard,omitempty"`
}

// AWSIAMPermissionPolicy is the schema-version-1 payload for an
// "aws_iam_permission_policy" source fact.
type AWSIAMPermissionPolicy struct {
	AccountID              string   `json:"account_id"`
	Region                 string   `json:"region"`
	Provider               string   `json:"provider"`
	CollectorInstanceID    string   `json:"collector_instance_id"`
	RedactionPolicyVersion string   `json:"redaction_policy_version"`
	PrincipalARN           string   `json:"principal_arn"`
	PolicySource           string   `json:"policy_source"`
	Effect                 string   `json:"effect"`
	PrincipalType          *string  `json:"principal_type,omitempty"`
	PolicyARN              *string  `json:"policy_arn,omitempty"`
	PolicyName             *string  `json:"policy_name,omitempty"`
	StatementSID           *string  `json:"statement_sid,omitempty"`
	Actions                []string `json:"actions,omitempty"`
	NotActions             []string `json:"not_actions,omitempty"`
	Resources              []string `json:"resources,omitempty"`
	NotResources           []string `json:"not_resources,omitempty"`
	ConditionKeys          []string `json:"condition_keys,omitempty"`
	ConditionOperators     []string `json:"condition_operators,omitempty"`
	ConditionOperatorCount *int     `json:"condition_operator_count,omitempty"`
	HasConditions          *bool    `json:"has_conditions,omitempty"`
	IsWildcardAction       *bool    `json:"is_wildcard_action,omitempty"`
	IsWildcardResource     *bool    `json:"is_wildcard_resource,omitempty"`
}

// AWSIAMPolicyAttachment is the schema-version-1 payload for an
// "aws_iam_policy_attachment" source fact.
type AWSIAMPolicyAttachment struct {
	AccountID              string  `json:"account_id"`
	Region                 string  `json:"region"`
	Provider               string  `json:"provider"`
	CollectorInstanceID    string  `json:"collector_instance_id"`
	RedactionPolicyVersion string  `json:"redaction_policy_version"`
	PrincipalARN           string  `json:"principal_arn"`
	PolicyARN              string  `json:"policy_arn"`
	PrincipalType          *string `json:"principal_type,omitempty"`
	PolicyName             *string `json:"policy_name,omitempty"`
	PolicySource           *string `json:"policy_source,omitempty"`
}

// AWSIAMPermissionBoundary is the schema-version-1 payload for an
// "aws_iam_permission_boundary" source fact.
type AWSIAMPermissionBoundary struct {
	AccountID              string  `json:"account_id"`
	Region                 string  `json:"region"`
	Provider               string  `json:"provider"`
	CollectorInstanceID    string  `json:"collector_instance_id"`
	RedactionPolicyVersion string  `json:"redaction_policy_version"`
	PrincipalARN           string  `json:"principal_arn"`
	BoundaryPolicyARN      string  `json:"boundary_policy_arn"`
	PrincipalType          *string `json:"principal_type,omitempty"`
	BoundaryType           *string `json:"boundary_type,omitempty"`
}

// AWSIAMInstanceProfile is the schema-version-1 payload for an
// "aws_iam_instance_profile" source fact.
type AWSIAMInstanceProfile struct {
	AccountID              string   `json:"account_id"`
	Region                 string   `json:"region"`
	Provider               string   `json:"provider"`
	CollectorInstanceID    string   `json:"collector_instance_id"`
	RedactionPolicyVersion string   `json:"redaction_policy_version"`
	ProfileARN             string   `json:"profile_arn"`
	Name                   *string  `json:"name,omitempty"`
	Path                   *string  `json:"path,omitempty"`
	RoleARNs               []string `json:"role_arns,omitempty"`
	RoleCount              *int     `json:"role_count,omitempty"`
}

// AWSIAMAccessAnalyzerFinding is the schema-version-1 payload for an
// "aws_iam_access_analyzer_finding" source fact.
type AWSIAMAccessAnalyzerFinding struct {
	AccountID              string   `json:"account_id"`
	Region                 string   `json:"region"`
	Provider               string   `json:"provider"`
	CollectorInstanceID    string   `json:"collector_instance_id"`
	RedactionPolicyVersion string   `json:"redaction_policy_version"`
	FindingID              *string  `json:"finding_id,omitempty"`
	AnalyzerARN            *string  `json:"analyzer_arn,omitempty"`
	ResourceARN            *string  `json:"resource_arn,omitempty"`
	ResourceType           *string  `json:"resource_type,omitempty"`
	Status                 *string  `json:"status,omitempty"`
	FindingType            *string  `json:"finding_type,omitempty"`
	ConditionKeys          []string `json:"condition_keys,omitempty"`
}
