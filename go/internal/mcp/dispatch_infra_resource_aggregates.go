// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func infraResourceAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/infra/resources/count", query: map[string]string{
		"category":          str(args, "category"),
		"kind":              str(args, "kind"),
		"resource_type":     str(args, "resource_type"),
		"provider":          str(args, "provider"),
		"environment":       str(args, "environment"),
		"resource_service":  str(args, "resource_service"),
		"resource_category": str(args, "resource_category"),
	}}
}

func infraResourceAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "provider"
	}
	return &route{method: "GET", path: "/api/v0/infra/resources/inventory", query: map[string]string{
		"group_by":          groupBy,
		"category":          str(args, "category"),
		"kind":              str(args, "kind"),
		"resource_type":     str(args, "resource_type"),
		"provider":          str(args, "provider"),
		"environment":       str(args, "environment"),
		"resource_service":  str(args, "resource_service"),
		"resource_category": str(args, "resource_category"),
		"limit":             strconv.Itoa(intOr(args, "limit", 100)),
		"offset":            strconv.Itoa(intOr(args, "offset", 0)),
	}}
}
