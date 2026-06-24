// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// freshnessRoute maps freshness drilldown tools to bounded internal HTTP
// routes. MCP stays pure transport: it forwards scope, repository, collector,
// source-system, generation, status, and limit selectors to the API and lets
// the handler enforce bounding, ordering, and not-found behavior.
func freshnessRoute(toolName string, args map[string]any) (*route, bool) {
	switch toolName {
	case "get_generation_lifecycle":
		return &route{method: "GET", path: "/api/v0/freshness/generations", query: map[string]string{
			"scope_id":       str(args, "scope_id"),
			"repository":     str(args, "repository"),
			"collector_kind": str(args, "collector_kind"),
			"source_system":  str(args, "source_system"),
			"generation_id":  str(args, "generation_id"),
			"status":         str(args, "status"),
			"limit":          intString(args, "limit", 50),
		}}, true
	case "get_changed_since":
		return &route{method: "GET", path: "/api/v0/freshness/changed-since", query: map[string]string{
			"scope_id":            str(args, "scope_id"),
			"repository":          str(args, "repository"),
			"since_generation_id": str(args, "since_generation_id"),
			"since_observed_at":   str(args, "since_observed_at"),
			"sample_limit":        intString(args, "sample_limit", 25),
		}}, true
	case "get_service_changed_since":
		return &route{method: "GET", path: "/api/v0/freshness/services/changed-since", query: map[string]string{
			"service_id":          str(args, "service_id"),
			"since_generation_id": str(args, "since_generation_id"),
			"sample_limit":        intString(args, "sample_limit", 25),
		}}, true
	default:
		return nil, false
	}
}
