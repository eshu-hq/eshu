package mcp

import "strconv"

func supplyChainImpactAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/findings/count", query: map[string]string{
		"advisory_id":    str(args, "advisory_id"),
		"cve_id":         str(args, "cve_id"),
		"ecosystem":      str(args, "ecosystem"),
		"environment":    str(args, "environment"),
		"ghsa_id":        str(args, "ghsa_id"),
		"osv_id":         str(args, "osv_id"),
		"package_id":     str(args, "package_id"),
		"repository_id":  str(args, "repository_id"),
		"service_id":     str(args, "service_id"),
		"severity":       str(args, "severity"),
		"subject_digest": str(args, "subject_digest"),
		"impact_status":  str(args, "impact_status"),
		"workload_id":    str(args, "workload_id"),
	}}
}

func supplyChainImpactAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "impact_status"
	}
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/inventory", query: map[string]string{
		"advisory_id":    str(args, "advisory_id"),
		"group_by":       groupBy,
		"cve_id":         str(args, "cve_id"),
		"ecosystem":      str(args, "ecosystem"),
		"environment":    str(args, "environment"),
		"ghsa_id":        str(args, "ghsa_id"),
		"osv_id":         str(args, "osv_id"),
		"package_id":     str(args, "package_id"),
		"repository_id":  str(args, "repository_id"),
		"service_id":     str(args, "service_id"),
		"severity":       str(args, "severity"),
		"subject_digest": str(args, "subject_digest"),
		"impact_status":  str(args, "impact_status"),
		"limit":          strconv.Itoa(intOr(args, "limit", 100)),
		"offset":         strconv.Itoa(intOr(args, "offset", 0)),
		"workload_id":    str(args, "workload_id"),
	}}
}
