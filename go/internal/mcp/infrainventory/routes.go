// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrainventorytools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for an infrastructure-inventory
// tool without executing it. It reports handled only for the four tools
// this package owns: count_infra_resources, get_infra_resource_inventory,
// investigate_resource, and analyze_infra_relationships. Family membership
// is an explicit name switch, never a prefix match, so a future tool
// spelled similarly cannot be silently absorbed. None of the four tools
// validates its arguments before building a request, so unlike
// servicecontext this package reports only (Request, bool), never an
// error -- a missing selector reaches the query handler as an empty field
// or filter, matching the pre-extraction root switch arms this replaces.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "count_infra_resources":
		return countInfraResourcesRequest(args), true
	case "get_infra_resource_inventory":
		return infraResourceInventoryRequest(args), true
	case "investigate_resource":
		return investigateResourceRequest(args), true
	case "analyze_infra_relationships":
		return analyzeInfraRelationshipsRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// countInfraResourcesRequest resolves count_infra_resources to
// GET /api/v0/infra/resources/count, forwarding the caller's optional
// category, kind, resource_type, provider, environment, resource_service,
// and resource_category filters verbatim as query parameters.
func countInfraResourcesRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/infra/resources/count", Query: map[string]string{
		"category":          args.String("category"),
		"kind":              args.String("kind"),
		"resource_type":     args.String("resource_type"),
		"provider":          args.String("provider"),
		"environment":       args.String("environment"),
		"resource_service":  args.String("resource_service"),
		"resource_category": args.String("resource_category"),
	}}
}

// infraResourceInventoryRequest resolves get_infra_resource_inventory to
// GET /api/v0/infra/resources/inventory, defaulting group_by to "provider"
// and limit/offset to 100/0 when the caller omits them.
func infraResourceInventoryRequest(args routecontract.Arguments) routecontract.Request {
	groupBy := args.String("group_by")
	if groupBy == "" {
		groupBy = "provider"
	}
	return routecontract.Request{Method: "GET", Path: "/api/v0/infra/resources/inventory", Query: map[string]string{
		"group_by":          groupBy,
		"category":          args.String("category"),
		"kind":              args.String("kind"),
		"resource_type":     args.String("resource_type"),
		"provider":          args.String("provider"),
		"environment":       args.String("environment"),
		"resource_service":  args.String("resource_service"),
		"resource_category": args.String("resource_category"),
		"limit":             strconv.Itoa(args.IntOr("limit", 100)),
		"offset":            strconv.Itoa(args.IntOr("offset", 0)),
	}}
}

// investigateResourceRequest resolves investigate_resource to
// POST /api/v0/impact/resource-investigation, defaulting max_depth to 4 and
// limit to 25 when the caller omits them.
func investigateResourceRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/impact/resource-investigation", Body: map[string]any{
		"query":         args.String("query"),
		"resource_id":   args.String("resource_id"),
		"resource_type": args.String("resource_type"),
		"environment":   args.String("environment"),
		"max_depth":     args.IntOr("max_depth", 4),
		"limit":         args.IntOr("limit", 25),
	}}
}

// analyzeInfraRelationshipsRequest resolves analyze_infra_relationships to
// POST /api/v0/infra/relationships, forwarding the caller's target as
// entity_id and query_type as relationship_type.
func analyzeInfraRelationshipsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: "/api/v0/infra/relationships", Body: map[string]any{
		"entity_id":         args.String("target"),
		"relationship_type": args.String("query_type"),
	}}
}
