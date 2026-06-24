// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func observabilityCoverageCorrelationsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/observability/coverage/correlations", query: map[string]string{
		"after_correlation_id":     str(args, "after_correlation_id"),
		"coverage_signal":          str(args, "coverage_signal"),
		"coverage_status":          str(args, "coverage_status"),
		"limit":                    strconv.Itoa(intOr(args, "limit", 50)),
		"observability_object_ref": str(args, "observability_object_ref"),
		"outcome":                  str(args, "outcome"),
		"provider":                 str(args, "provider"),
		"resource_class":           str(args, "resource_class"),
		"scope_id":                 str(args, "scope_id"),
		"source_class":             str(args, "source_class"),
		"target_service_ref":       str(args, "target_service_ref"),
		"target_uid":               str(args, "target_uid"),
	}}
}
