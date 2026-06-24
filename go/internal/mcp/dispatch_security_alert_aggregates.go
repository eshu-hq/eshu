// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func securityAlertReconciliationAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/security-alerts/reconciliations/count", query: map[string]string{
		"repository_id":         str(args, "repository_id"),
		"provider":              str(args, "provider"),
		"package_id":            str(args, "package_id"),
		"cve_id":                str(args, "cve_id"),
		"ghsa_id":               str(args, "ghsa_id"),
		"provider_state":        str(args, "provider_state"),
		"reconciliation_status": str(args, "reconciliation_status"),
	}}
}

func securityAlertReconciliationAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "reconciliation_status"
	}
	return &route{method: "GET", path: "/api/v0/supply-chain/security-alerts/reconciliations/inventory", query: map[string]string{
		"group_by":              groupBy,
		"repository_id":         str(args, "repository_id"),
		"provider":              str(args, "provider"),
		"package_id":            str(args, "package_id"),
		"cve_id":                str(args, "cve_id"),
		"ghsa_id":               str(args, "ghsa_id"),
		"provider_state":        str(args, "provider_state"),
		"reconciliation_status": str(args, "reconciliation_status"),
		"limit":                 strconv.Itoa(intOr(args, "limit", 100)),
		"offset":                strconv.Itoa(intOr(args, "offset", 0)),
	}}
}
