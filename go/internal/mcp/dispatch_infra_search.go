// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func infraResourceSearchRoute(args map[string]any) *route {
	return &route{method: "POST", path: "/api/v0/infra/resources/search", body: map[string]any{
		"query":             str(args, "query"),
		"category":          str(args, "category"),
		"kind":              str(args, "kind"),
		"provider":          str(args, "provider"),
		"environment":       str(args, "environment"),
		"resource_service":  str(args, "resource_service"),
		"resource_category": str(args, "resource_category"),
		"limit":             intOr(args, "limit", 50),
	}}
}
