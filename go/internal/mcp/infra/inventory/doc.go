// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package infrainventorytools defines pure route selection for the MCP
// infrastructure-inventory family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order (count_infra_resources
// and get_infra_resource_inventory stay at the root in
// tools_infra_resource_aggregates.go; investigate_resource and
// analyze_infra_relationships stay in ecosystem/tools.go), global route
// fanout, the private infraInventoryRoute adapter, HTTP dispatch,
// authorization, timeouts, response budgets, envelopes, and telemetry. The
// query package owns the bounded reads behind each
// /api/v0/infra/resources/..., /api/v0/impact/resource-investigation, and
// /api/v0/infra/relationships path. This package runs no query and must keep
// every tool name, request method, path, and body/query key stable.
//
// The four tools — count_infra_resources, get_infra_resource_inventory,
// investigate_resource, and analyze_infra_relationships — each build a
// distinct request shape; no body or query builder is shared across tools.
// The sibling find_infra_resources tool stays with the infrasearch family:
// it shares the /api/v0/infra/resources namespace but a different request
// shape and a different root adapter (infraResourceSearchRoute).
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "100" becomes the default rather
// than an error. None of the four tools validates its arguments before
// building a request, so Route reports only (Request, bool), never an error.
package infrainventorytools
