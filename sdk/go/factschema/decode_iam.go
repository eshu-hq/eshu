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
	return encodeToPayload(permission)
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
	return encodeToPayload(permission)
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
	return encodeToPayload(principal)
}
