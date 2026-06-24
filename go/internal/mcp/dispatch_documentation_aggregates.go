// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func documentationFindingAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/documentation/findings/count", query: map[string]string{
		"scope_id":        str(args, "scope_id"),
		"finding_type":    str(args, "finding_type"),
		"source_id":       str(args, "source_id"),
		"document_id":     str(args, "document_id"),
		"status":          str(args, "status"),
		"truth_level":     str(args, "truth_level"),
		"freshness_state": str(args, "freshness_state"),
	}}
}

func documentationFindingAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "status"
	}
	return &route{method: "GET", path: "/api/v0/documentation/findings/inventory", query: map[string]string{
		"group_by":        groupBy,
		"scope_id":        str(args, "scope_id"),
		"finding_type":    str(args, "finding_type"),
		"source_id":       str(args, "source_id"),
		"document_id":     str(args, "document_id"),
		"status":          str(args, "status"),
		"truth_level":     str(args, "truth_level"),
		"freshness_state": str(args, "freshness_state"),
		"limit":           strconv.Itoa(intOr(args, "limit", 100)),
		"offset":          strconv.Itoa(intOr(args, "offset", 0)),
	}}
}
