// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiamtools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a secrets/IAM posture tool
// without executing it. It reports handled only for tools owned by this package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_secrets_iam_identity_trust_chains":
		return identityTrustChainsRequest(args), true
	case "list_secrets_iam_privilege_posture_observations":
		return privilegePostureObservationsRequest(args), true
	case "list_secrets_iam_secret_access_paths":
		return secretAccessPathsRequest(args), true
	case "list_secrets_iam_posture_gaps":
		return postureGapsRequest(args), true
	case "count_secrets_iam_posture":
		return postureSummaryRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// identityTrustChainsRequest maps the list_secrets_iam_identity_trust_chains
// tool call to the bounded read-only HTTP route. The limit defaults to 50 and
// the handler enforces the 1-200 bound and the required scope anchor.
func identityTrustChainsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/identity-trust-chains", Query: map[string]string{
		"after_chain_id":           args.String("after_chain_id"),
		"chain_id":                 args.String("chain_id"),
		"iam_role_fingerprint":     args.String("iam_role_fingerprint"),
		"limit":                    strconv.Itoa(args.IntOr("limit", 50)),
		"scope_id":                 args.String("scope_id"),
		"service_account_join_key": args.String("service_account_join_key"),
		"state":                    args.String("state"),
		"workload_object_id":       args.String("workload_object_id"),
	}}
}

// privilegePostureObservationsRequest maps the
// list_secrets_iam_privilege_posture_observations tool to its bounded route.
func privilegePostureObservationsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/privilege-posture-observations", Query: map[string]string{
		"after_observation_id": args.String("after_observation_id"),
		"limit":                strconv.Itoa(args.IntOr("limit", 50)),
		"observation_id":       args.String("observation_id"),
		"risk_type":            args.String("risk_type"),
		"scope_id":             args.String("scope_id"),
		"severity":             args.String("severity"),
		"state":                args.String("state"),
	}}
}

// secretAccessPathsRequest maps the list_secrets_iam_secret_access_paths tool
// to its bounded route.
func secretAccessPathsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/secret-access-paths", Query: map[string]string{
		"after_path_id":        args.String("after_path_id"),
		"chain_id":             args.String("chain_id"),
		"limit":                strconv.Itoa(args.IntOr("limit", 50)),
		"path_id":              args.String("path_id"),
		"scope_id":             args.String("scope_id"),
		"state":                args.String("state"),
		"vault_mount_join_key": args.String("vault_mount_join_key"),
	}}
}

// postureGapsRequest maps the list_secrets_iam_posture_gaps tool to its
// bounded route.
func postureGapsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/posture-gaps", Query: map[string]string{
		"after_gap_id":             args.String("after_gap_id"),
		"gap_id":                   args.String("gap_id"),
		"gap_type":                 args.String("gap_type"),
		"limit":                    strconv.Itoa(args.IntOr("limit", 50)),
		"scope_id":                 args.String("scope_id"),
		"service_account_join_key": args.String("service_account_join_key"),
		"state":                    args.String("state"),
	}}
}

// postureSummaryRequest maps the count_secrets_iam_posture tool to the bounded
// scope-anchored summary route.
//
// It is the one route in this family that takes no limit and no cursor. The
// summary is an aggregate over the whole scope, so there is no page to size
// and nothing to seek past. Forwarding a limit here would not cap anything
// either -- the handler never reads one -- so the key would be inert and would
// advertise a bound the endpoint does not honor. scope_id is the only key, and it is always
// sent, so the handler sees an explicitly empty anchor rather than none.
func postureSummaryRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/posture-summary", Query: map[string]string{
		"scope_id": args.String("scope_id"),
	}}
}
