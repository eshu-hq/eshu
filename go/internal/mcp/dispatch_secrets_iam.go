// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

// secretsIAMIdentityTrustChainsRoute maps the list_secrets_iam_identity_trust_chains
// tool call to the bounded read-only HTTP route. The limit defaults to 50 and
// the handler enforces the 1-200 bound and the required scope anchor.
func secretsIAMIdentityTrustChainsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/secrets-iam/identity-trust-chains", query: map[string]string{
		"after_chain_id":           str(args, "after_chain_id"),
		"chain_id":                 str(args, "chain_id"),
		"iam_role_fingerprint":     str(args, "iam_role_fingerprint"),
		"limit":                    strconv.Itoa(intOr(args, "limit", 50)),
		"scope_id":                 str(args, "scope_id"),
		"service_account_join_key": str(args, "service_account_join_key"),
		"state":                    str(args, "state"),
		"workload_object_id":       str(args, "workload_object_id"),
	}}
}

// secretsIAMPrivilegePostureObservationsRoute maps the
// list_secrets_iam_privilege_posture_observations tool to its bounded route.
func secretsIAMPrivilegePostureObservationsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/secrets-iam/privilege-posture-observations", query: map[string]string{
		"after_observation_id": str(args, "after_observation_id"),
		"limit":                strconv.Itoa(intOr(args, "limit", 50)),
		"observation_id":       str(args, "observation_id"),
		"risk_type":            str(args, "risk_type"),
		"scope_id":             str(args, "scope_id"),
		"severity":             str(args, "severity"),
		"state":                str(args, "state"),
	}}
}

// secretsIAMSecretAccessPathsRoute maps the list_secrets_iam_secret_access_paths
// tool to its bounded route.
func secretsIAMSecretAccessPathsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/secrets-iam/secret-access-paths", query: map[string]string{
		"after_path_id":        str(args, "after_path_id"),
		"chain_id":             str(args, "chain_id"),
		"limit":                strconv.Itoa(intOr(args, "limit", 50)),
		"path_id":              str(args, "path_id"),
		"scope_id":             str(args, "scope_id"),
		"state":                str(args, "state"),
		"vault_mount_join_key": str(args, "vault_mount_join_key"),
	}}
}

// secretsIAMPostureSummaryRoute maps the count_secrets_iam_posture tool to the
// bounded scope-anchored summary route.
func secretsIAMPostureSummaryRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/secrets-iam/posture-summary", query: map[string]string{
		"scope_id": str(args, "scope_id"),
	}}
}

// secretsIAMPostureGapsRoute maps the list_secrets_iam_posture_gaps tool to its
// bounded route.
func secretsIAMPostureGapsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/secrets-iam/posture-gaps", query: map[string]string{
		"after_gap_id":             str(args, "after_gap_id"),
		"gap_id":                   str(args, "gap_id"),
		"gap_type":                 str(args, "gap_type"),
		"limit":                    strconv.Itoa(intOr(args, "limit", 50)),
		"scope_id":                 str(args, "scope_id"),
		"service_account_join_key": str(args, "service_account_join_key"),
		"state":                    str(args, "state"),
	}}
}
