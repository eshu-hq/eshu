// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iacmanagementtools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for an IaC-management tool
// without executing it. It reports handled only for the seven tools this
// package owns: dead-IaC discovery, unmanaged-cloud-resource discovery, the
// read-only management-status pair, the Terraform import-plan proposer, the
// Terraform config-vs-state drift finder, and the replatforming-ownership
// packet builder. Family membership is an explicit name switch, never a
// prefix match, so a future tool spelled similarly (or a sibling
// replatforming tool such as compose_replatforming_plan,
// list_aws_runtime_drift_findings, or get_replatforming_rollups) cannot be
// silently absorbed.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "find_dead_iac":
		return routecontract.Request{Method: "POST", Path: "/api/v0/iac/dead", Body: map[string]any{
			"repo_id":           args.String("repo_id"),
			"repo_ids":          args.StringSlice("repo_ids"),
			"families":          args.StringSlice("families"),
			"include_ambiguous": args.BoolOr("include_ambiguous", false),
			"limit":             args.IntOr("limit", 100),
			"offset":            args.IntOr("offset", 0),
		}}, true
	case "find_unmanaged_resources":
		return routecontract.Request{Method: "POST", Path: "/api/v0/iac/unmanaged-resources", Body: map[string]any{
			"scope_id":      args.String("scope_id"),
			"account_id":    args.String("account_id"),
			"region":        args.String("region"),
			"finding_kinds": args.StringSlice("finding_kinds"),
			"limit":         args.IntOr("limit", 100),
			"offset":        args.IntOr("offset", 0),
		}}, true
	case "get_iac_management_status":
		return routecontract.Request{Method: "POST", Path: "/api/v0/iac/management-status", Body: managementStatusBody(args)}, true
	case "explain_iac_management_status":
		return routecontract.Request{Method: "POST", Path: "/api/v0/iac/management-status/explain", Body: managementStatusBody(args)}, true
	case "propose_terraform_import_plan":
		return routecontract.Request{Method: "POST", Path: "/api/v0/iac/terraform-import-plan/candidates", Body: map[string]any{
			"scope_id":      args.String("scope_id"),
			"account_id":    args.String("account_id"),
			"region":        args.String("region"),
			"arn":           args.String("arn"),
			"resource_id":   args.String("resource_id"),
			"finding_kinds": args.StringSlice("finding_kinds"),
			"limit":         args.IntOr("limit", 100),
			"offset":        args.IntOr("offset", 0),
		}}, true
	case "list_terraform_config_state_drift_findings":
		return routecontract.Request{Method: "POST", Path: "/api/v0/terraform/config-state-drift/findings", Body: map[string]any{
			"scope_id":    args.String("scope_id"),
			"address":     args.String("address"),
			"outcome":     args.String("outcome"),
			"drift_kinds": args.StringSlice("drift_kinds"),
			"limit":       args.IntOr("limit", 100),
			"offset":      args.IntOr("offset", 0),
		}}, true
	case "find_unmanaged_resource_owners":
		return routecontract.Request{Method: "POST", Path: "/api/v0/replatforming/ownership-packets", Body: map[string]any{
			"scope_id":      args.String("scope_id"),
			"account_id":    args.String("account_id"),
			"region":        args.String("region"),
			"finding_kinds": args.StringSlice("finding_kinds"),
			"limit":         args.IntOr("limit", 100),
			"offset":        args.IntOr("offset", 0),
		}}, true
	default:
		return routecontract.Request{}, false
	}
}

// managementStatusBody builds the shared request body for
// get_iac_management_status and explain_iac_management_status. Both tools
// resolve one AWS stable resource identity to the same bounded, single-item
// page (limit 1, offset 0); they differ only in which handler renders the
// result (a status summary vs. a grouped explanation), so the wire body is
// identical. No tool outside this pair uses this body shape.
func managementStatusBody(args routecontract.Arguments) map[string]any {
	return map[string]any{
		"scope_id":      args.String("scope_id"),
		"account_id":    args.String("account_id"),
		"region":        args.String("region"),
		"arn":           args.String("arn"),
		"resource_id":   args.String("resource_id"),
		"finding_kinds": args.StringSlice("finding_kinds"),
		"limit":         1,
		"offset":        0,
	}
}
