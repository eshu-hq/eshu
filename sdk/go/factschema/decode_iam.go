// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// DecodeAWSIAMPermission decodes env.Payload into the latest iamv1.Permission
// struct for the "aws_iam_permission" fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. Callers (reducer
// handlers) receive either the decoded struct or a classified *DecodeError; they
// must never substitute a zero-value struct on error.
func DecodeAWSIAMPermission(env Envelope) (iamv1.Permission, error) {
	return decodeLatestMajor[iamv1.Permission](FactKindAWSIAMPermission, env)
}

// EncodeAWSIAMPermission marshals an iamv1.Permission into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeAWSIAMPermission
// for schema-version-1 payloads, used by collectors emitting this fact kind and
// by this module's round-trip tests.
func EncodeAWSIAMPermission(permission iamv1.Permission) (map[string]any, error) {
	payload := map[string]any{
		"account_id":    permission.AccountID,
		"region":        permission.Region,
		"principal_arn": permission.PrincipalARN,
		"effect":        permission.Effect,
		"policy_source": permission.PolicySource,
	}
	addStringPtr(payload, "service_kind", permission.ServiceKind)
	addStringPtr(payload, "collector_instance_id", permission.CollectorInstanceID)
	addStringPtr(payload, "principal_type", permission.PrincipalType)
	addStringPtr(payload, "policy_arn", permission.PolicyARN)
	addStringPtr(payload, "policy_name", permission.PolicyName)
	addStringPtr(payload, "statement_sid", permission.StatementSID)
	addStringSlice(payload, "actions", permission.Actions)
	addStringSlice(payload, "not_actions", permission.NotActions)
	addStringSlice(payload, "resources", permission.Resources)
	addStringSlice(payload, "not_resources", permission.NotResources)
	addStringSlice(payload, "assume_principals", permission.AssumePrincipals)
	addBoolPtr(payload, "has_conditions", permission.HasConditions)
	addStringSlice(payload, "condition_keys", permission.ConditionKeys)
	addStringSlice(payload, "condition_operators", permission.ConditionOperators)
	addIntPtr(payload, "condition_operator_count", permission.ConditionOperatorCount)
	addBoolPtr(payload, "is_wildcard_action", permission.IsWildcardAction)
	addBoolPtr(payload, "is_wildcard_resource", permission.IsWildcardResource)
	return payload, nil
}

// DecodeAWSResourcePolicyPermission decodes env.Payload into the latest
// iamv1.ResourcePolicyPermission struct for the "aws_resource_policy_permission"
// fact kind. See DecodeAWSIAMPermission for the dispatch and error contract.
func DecodeAWSResourcePolicyPermission(env Envelope) (iamv1.ResourcePolicyPermission, error) {
	return decodeLatestMajor[iamv1.ResourcePolicyPermission](FactKindAWSResourcePolicyPermission, env)
}

// EncodeAWSResourcePolicyPermission marshals an iamv1.ResourcePolicyPermission
// into the map[string]any payload shape an Envelope carries. It is the inverse
// of DecodeAWSResourcePolicyPermission for schema-version-1 payloads.
func EncodeAWSResourcePolicyPermission(permission iamv1.ResourcePolicyPermission) (map[string]any, error) {
	payload := map[string]any{
		"account_id":    permission.AccountID,
		"region":        permission.Region,
		"resource_arn":  permission.ResourceARN,
		"resource_type": permission.ResourceType,
		"effect":        permission.Effect,
	}
	addStringPtr(payload, "service_kind", permission.ServiceKind)
	addStringPtr(payload, "collector_instance_id", permission.CollectorInstanceID)
	addStringPtr(payload, "policy_source", permission.PolicySource)
	addStringSlice(payload, "actions", permission.Actions)
	addStringSlice(payload, "not_actions", permission.NotActions)
	addStringSlice(payload, "resources", permission.Resources)
	addStringSlice(payload, "not_resources", permission.NotResources)
	addStringSlice(payload, "principal_arns", permission.PrincipalARNs)
	addBoolPtr(payload, "is_public", permission.IsPublic)
	addBoolPtr(payload, "has_conditions", permission.HasConditions)
	addStringSlice(payload, "condition_keys", permission.ConditionKeys)
	addStringSlice(payload, "condition_operators", permission.ConditionOperators)
	addIntPtr(payload, "condition_operator_count", permission.ConditionOperatorCount)
	addStringSlice(payload, "principal_account_ids", permission.PrincipalAccountIDs)
	addStringSlice(payload, "principal_types", permission.PrincipalTypes)
	addBoolPtr(payload, "is_wildcard_action", permission.IsWildcardAction)
	addBoolPtr(payload, "is_wildcard_resource", permission.IsWildcardResource)
	addBoolPtr(payload, "is_cross_account", permission.IsCrossAccount)
	return payload, nil
}

// DecodeAWSIAMPrincipal decodes env.Payload into the latest iamv1.Principal
// struct for the "aws_iam_principal" fact kind. See DecodeAWSIAMPermission for
// the dispatch and error contract.
func DecodeAWSIAMPrincipal(env Envelope) (iamv1.Principal, error) {
	return decodeLatestMajor[iamv1.Principal](FactKindAWSIAMPrincipal, env)
}

// EncodeAWSIAMPrincipal marshals an iamv1.Principal into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeAWSIAMPrincipal
// for schema-version-1 payloads.
func EncodeAWSIAMPrincipal(principal iamv1.Principal) (map[string]any, error) {
	payload := map[string]any{
		"account_id":     principal.AccountID,
		"region":         principal.Region,
		"principal_arn":  principal.PrincipalARN,
		"principal_type": principal.PrincipalType,
	}
	addStringPtr(payload, "principal_id", principal.PrincipalID)
	addStringPtr(payload, "provider", principal.Provider)
	addStringPtr(payload, "collector_instance_id", principal.CollectorInstanceID)
	addStringPtr(payload, "redaction_policy_version", principal.RedactionPolicyVersion)
	addStringPtr(payload, "name", principal.Name)
	addStringPtr(payload, "path", principal.Path)
	addStringPtr(payload, "url_fingerprint", principal.URLFingerprint)
	addBoolPtr(payload, "url_present", principal.URLPresent)
	addIntPtr(payload, "client_id_count", principal.ClientIDCount)
	addIntPtr(payload, "thumbprint_count", principal.ThumbprintCount)
	addStringSlice(payload, "correlation_hints", principal.CorrelationHints)
	return payload, nil
}
