package mcp

import "strconv"

func supplyChainImpactAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/findings/count", query: map[string]string{
		"cve_id":         str(args, "cve_id"),
		"package_id":     str(args, "package_id"),
		"repository_id":  str(args, "repository_id"),
		"subject_digest": str(args, "subject_digest"),
		"impact_status":  str(args, "impact_status"),
	}}
}

func supplyChainImpactAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "impact_status"
	}
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/inventory", query: map[string]string{
		"group_by":       groupBy,
		"cve_id":         str(args, "cve_id"),
		"package_id":     str(args, "package_id"),
		"repository_id":  str(args, "repository_id"),
		"subject_digest": str(args, "subject_digest"),
		"impact_status":  str(args, "impact_status"),
		"limit":          strconv.Itoa(intOr(args, "limit", 100)),
		"offset":         strconv.Itoa(intOr(args, "offset", 0)),
	}}
}
