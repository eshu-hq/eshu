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
