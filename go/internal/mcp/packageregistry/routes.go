// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageregistrytools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a package-registry tool without
// executing it. It reports handled only for tools owned by this package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_package_registry_packages":
		return packagesRequest(args), true
	case "count_package_registry_packages":
		return aggregateCountRequest(args), true
	case "get_package_registry_package_inventory":
		return aggregateInventoryRequest(args), true
	case "list_package_registry_versions":
		return versionsRequest(args), true
	case "list_package_registry_dependencies":
		return dependenciesRequest(args), true
	case "list_package_registry_correlations":
		return correlationsRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

func packagesRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/packages", Query: map[string]string{
		"ecosystem":  args.String("ecosystem"),
		"limit":      strconv.Itoa(args.IntOr("limit", 50)),
		"name":       args.String("name"),
		"package_id": args.String("package_id"),
	}}
}

func versionsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/versions", Query: map[string]string{
		"limit":      strconv.Itoa(args.IntOr("limit", 50)),
		"package_id": args.String("package_id"),
	}}
}

func dependenciesRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/dependencies", Query: map[string]string{
		"after_dependency_id": args.String("after_dependency_id"),
		"after_version_id":    args.String("after_version_id"),
		"limit":               strconv.Itoa(args.IntOr("limit", 50)),
		"package_id":          args.String("package_id"),
		"version_id":          args.String("version_id"),
	}}
}

func correlationsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/correlations", Query: map[string]string{
		"after_correlation_id": args.String("after_correlation_id"),
		"limit":                strconv.Itoa(args.IntOr("limit", 50)),
		"package_id":           args.String("package_id"),
		"relationship_kind":    args.String("relationship_kind"),
		"repository_id":        args.String("repository_id"),
	}}
}

func aggregateCountRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/packages/count", Query: map[string]string{
		"ecosystem":       args.String("ecosystem"),
		"registry":        args.String("registry"),
		"namespace":       args.String("namespace"),
		"package_manager": args.String("package_manager"),
		"visibility":      args.String("visibility"),
	}}
}

func aggregateInventoryRequest(args routecontract.Arguments) routecontract.Request {
	groupBy := args.String("group_by")
	if groupBy == "" {
		groupBy = "ecosystem"
	}
	return routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/packages/inventory", Query: map[string]string{
		"group_by":        groupBy,
		"ecosystem":       args.String("ecosystem"),
		"registry":        args.String("registry"),
		"namespace":       args.String("namespace"),
		"package_manager": args.String("package_manager"),
		"visibility":      args.String("visibility"),
		"limit":           strconv.Itoa(args.IntOr("limit", 100)),
		"offset":          strconv.Itoa(args.IntOr("offset", 0)),
	}}
}
