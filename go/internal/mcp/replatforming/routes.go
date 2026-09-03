// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replatformingtools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for a replatforming-planning tool
// without executing it. It reports handled only for the two tools this
// package owns: compose_replatforming_plan and get_replatforming_rollups.
// Family membership is an explicit name switch, never a prefix match, so a
// future tool spelled similarly cannot be silently absorbed — in particular
// list_aws_runtime_drift_findings shares the root switch's neighbourhood and
// the "not part of the IaC-management family" grouping, but builds a
// different, narrower body against a different path
// (/api/v0/aws/runtime-drift/findings) and stays in the parent's
// dispatch_iac.go. Neither owned tool validates its arguments before
// building a request, so unlike servicecontext this package reports only
// (Request, bool), never an error — a missing selector reaches the query
// handler as an empty field, matching the pre-extraction root switch arms
// this replaces.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "compose_replatforming_plan":
		return composeReplatformingPlanRequest(args), true
	case "get_replatforming_rollups":
		return replatformingRollupsRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// composeReplatformingPlanRequest resolves compose_replatforming_plan to
// POST /api/v0/replatforming/plans. scope_kind selects the primary anchor
// dimension (account, region, service, workload, repository, environment, or
// resource); the handler behind this path rejects a missing or unsupported
// scope_kind, but that check runs in internal/query, not here.
func composeReplatformingPlanRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/replatforming/plans", Body: map[string]any{
		"scope_kind":    args.String("scope_kind"),
		"scope_id":      args.String("scope_id"),
		"account_id":    args.String("account_id"),
		"region":        args.String("region"),
		"service_name":  args.String("service_name"),
		"workload_id":   args.String("workload_id"),
		"repo_id":       args.String("repo_id"),
		"environment":   args.String("environment"),
		"arn":           args.String("arn"),
		"resource_id":   args.String("resource_id"),
		"finding_kinds": args.StringSlice("finding_kinds"),
		"limit":         args.IntOr("limit", 100),
		"offset":        args.IntOr("offset", 0),
	}}
}

// replatformingRollupsRequest resolves get_replatforming_rollups to
// POST /api/v0/replatforming/rollups. The rollup is account/environment/
// service scoped, not single-resource, so unlike
// composeReplatformingPlanRequest it deliberately never forwards an arn that
// would narrow the summary to one resource.
func replatformingRollupsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/replatforming/rollups", Body: map[string]any{
		"scope_id":      args.String("scope_id"),
		"account_id":    args.String("account_id"),
		"region":        args.String("region"),
		"finding_kinds": args.StringSlice("finding_kinds"),
		"limit":         args.IntOr("limit", 100),
		"offset":        args.IntOr("offset", 0),
	}}
}
