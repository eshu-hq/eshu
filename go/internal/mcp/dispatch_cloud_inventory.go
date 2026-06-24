// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

// cloudInventoryRoute maps the canonical cloud inventory readback tool to its
// internal GET route. It forwards only the bounded, non-sensitive filter
// parameters; the handler validates provider and management_origin against
// closed sets and gates the capability by runtime profile.
func cloudInventoryRoute(toolName string, args map[string]any) (*route, bool) {
	if toolName != "list_cloud_resource_inventory" {
		return nil, false
	}
	return &route{
		method: "GET",
		path:   "/api/v0/cloud/inventory",
		query:  cloudInventoryQuery(args),
	}, true
}

func cloudInventoryQuery(args map[string]any) map[string]string {
	query := map[string]string{}
	for _, key := range []string{
		"provider",
		"scope_id",
		"account_id",
		"project_id",
		"subscription_id",
		"management_origin",
		"cursor",
	} {
		if value := str(args, key); value != "" {
			query[key] = value
		}
	}
	if limit := intOr(args, "limit", 50); limit > 0 {
		query["limit"] = strconv.Itoa(limit)
	}
	return query
}
