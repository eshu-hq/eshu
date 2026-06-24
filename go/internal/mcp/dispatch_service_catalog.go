// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func serviceCatalogCorrelationsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/service-catalog/correlations", query: map[string]string{
		"after_correlation_id": str(args, "after_correlation_id"),
		"drift_status":         str(args, "drift_status"),
		"entity_ref":           str(args, "entity_ref"),
		"limit":                strconv.Itoa(intOr(args, "limit", 50)),
		"outcome":              str(args, "outcome"),
		"owner_ref":            str(args, "owner_ref"),
		"provider":             str(args, "provider"),
		"repository_id":        str(args, "repository_id"),
		"scope_id":             str(args, "scope_id"),
		"service_id":           str(args, "service_id"),
		"workload_id":          str(args, "workload_id"),
	}}
}
