// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func packageRegistryAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/package-registry/packages/count", query: map[string]string{
		"ecosystem":       str(args, "ecosystem"),
		"registry":        str(args, "registry"),
		"namespace":       str(args, "namespace"),
		"package_manager": str(args, "package_manager"),
		"visibility":      str(args, "visibility"),
	}}
}

func packageRegistryAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "ecosystem"
	}
	return &route{method: "GET", path: "/api/v0/package-registry/packages/inventory", query: map[string]string{
		"group_by":        groupBy,
		"ecosystem":       str(args, "ecosystem"),
		"registry":        str(args, "registry"),
		"namespace":       str(args, "namespace"),
		"package_manager": str(args, "package_manager"),
		"visibility":      str(args, "visibility"),
		"limit":           strconv.Itoa(intOr(args, "limit", 100)),
		"offset":          strconv.Itoa(intOr(args, "offset", 0)),
	}}
}
