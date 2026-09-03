// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	replatformingtools "github.com/eshu-hq/eshu/go/internal/mcp/replatforming"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// iacManagementStatusBody, terraformImportPlanBody,
// terraformConfigStateDriftFindingsBody, and replatformingOwnershipBody
// moved to internal/mcp/iacmanagement (managementStatusBody and the inline
// bodies in Route) with the tools that used them. compose_replatforming_plan
// and get_replatforming_rollups moved to internal/mcp/replatforming,
// reached through the replatformingRoute adapter below — this file already
// owned their body builders, so the adapter reuses it rather than creating a
// new one, keeping the root non-test file count at its dirgate pin.
// awsRuntimeDriftFindingsBody stays here: list_aws_runtime_drift_findings is
// not part of the replatforming family (see the Route doc comment in
// internal/mcp/replatforming/routes.go) or the IaC-management family (see
// the iacManagementRoute doc comment in dispatch.go).

func awsRuntimeDriftFindingsBody(args map[string]any) map[string]any {
	return map[string]any{
		"scope_id":      str(args, "scope_id"),
		"account_id":    str(args, "account_id"),
		"region":        str(args, "region"),
		"arn":           str(args, "arn"),
		"finding_kinds": stringSlice(args, "finding_kinds"),
		"limit":         intOr(args, "limit", 100),
		"offset":        intOr(args, "offset", 0),
	}
}

// replatformingRoute adapts the child package's replatforming-planning
// request selection into the root dispatcher's transport route. It lives in
// this file rather than dispatch.go itself because dispatch.go already
// owned the two switch arms this delegation replaces, matching the
// infraInventoryRoute precedent of reusing an existing file to keep the
// root non-test file count unchanged.
func replatformingRoute(toolName string, args map[string]any) (*route, bool) {
	return adaptChildRoute(replatformingtools.Route(toolName, routecontract.Arguments(args)))
}
