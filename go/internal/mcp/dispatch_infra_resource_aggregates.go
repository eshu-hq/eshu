// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	infrainventorytools "github.com/eshu-hq/eshu/go/internal/mcp/infrainventory"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// infraInventoryRoute adapts the child package's infrastructure-inventory
// request selection into the root dispatcher's transport route, exactly as
// deadCodeRoute, codeQualityRoute, entityResolutionRoute, codeIntelRoute,
// and iacManagementRoute in dispatch.go adapt theirs: same former-switch
// arms, same delegation position, same dirgate reason for living in an
// existing file rather than a new one. count_infra_resources and
// get_infra_resource_inventory moved here from this file's own
// infraResourceAggregateCountRoute and infraResourceAggregateInventoryRoute
// body helpers; investigate_resource and analyze_infra_relationships moved
// here from inline dispatch.go switch arms. find_infra_resources stays with
// the sibling infrasearch family reached through infraResourceSearchRoute
// in dispatch.go -- searching resources and counting/investigating them are
// different families that happen to share the infra/resources namespace.
func infraInventoryRoute(toolName string, args map[string]any) (*route, bool) {
	return adaptChildRoute(infrainventorytools.Route(toolName, routecontract.Arguments(args)))
}
