// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func supplyChainImpactAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/findings/count", query: supplyChainImpactAggregateFilterQuery(args)}
}

func supplyChainImpactAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "impact_status"
	}
	query := supplyChainImpactAggregateFilterQuery(args)
	query["group_by"] = groupBy
	query["limit"] = strconv.Itoa(intOr(args, "limit", 100))
	query["offset"] = strconv.Itoa(intOr(args, "offset", 0))
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/inventory", query: query}
}

func supplyChainImpactAggregateFilterQuery(args map[string]any) map[string]string {
	query := map[string]string{
		"advisory_id":        str(args, "advisory_id"),
		"cve_id":             str(args, "cve_id"),
		"ecosystem":          str(args, "ecosystem"),
		"environment":        str(args, "environment"),
		"ghsa_id":            str(args, "ghsa_id"),
		"image_ref":          str(args, "image_ref"),
		"osv_id":             str(args, "osv_id"),
		"package_id":         str(args, "package_id"),
		"profile":            str(args, "profile"),
		"priority_bucket":    str(args, "priority_bucket"),
		"repository_id":      str(args, "repository_id"),
		"service_id":         str(args, "service_id"),
		"severity":           str(args, "severity"),
		"subject_digest":     str(args, "subject_digest"),
		"impact_status":      str(args, "impact_status"),
		"workload_id":        str(args, "workload_id"),
		"suppression_state":  str(args, "suppression_state"),
		"min_priority_score": strconv.Itoa(intOr(args, "min_priority_score", 0)),
	}
	if encoded := boolStr(args, "include_suppressed"); encoded != "" {
		query["include_suppressed"] = encoded
	}
	return query
}
