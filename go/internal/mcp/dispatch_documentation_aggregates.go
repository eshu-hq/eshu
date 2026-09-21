// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

func documentationFindingAggregateCountRoute(args map[string]any) *routecontract.Request {
	return &routecontract.Request{Method: "GET", Path: "/api/v0/documentation/findings/count", Query: map[string]string{
		"scope_id":        str(args, "scope_id"),
		"finding_type":    str(args, "finding_type"),
		"source_id":       str(args, "source_id"),
		"document_id":     str(args, "document_id"),
		"status":          str(args, "status"),
		"truth_level":     str(args, "truth_level"),
		"freshness_state": str(args, "freshness_state"),
	}}
}

func documentationFindingAggregateInventoryRoute(args map[string]any) *routecontract.Request {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "status"
	}
	return &routecontract.Request{Method: "GET", Path: "/api/v0/documentation/findings/inventory", Query: map[string]string{
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
