// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func kubernetesCorrelationsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/kubernetes/correlations", query: map[string]string{
		"after_correlation_id": str(args, "after_correlation_id"),
		"cluster_id":           str(args, "cluster_id"),
		"drift_kind":           str(args, "drift_kind"),
		"image_ref":            str(args, "image_ref"),
		"limit":                strconv.Itoa(intOr(args, "limit", 50)),
		"namespace":            str(args, "namespace"),
		"outcome":              str(args, "outcome"),
		"scope_id":             str(args, "scope_id"),
		"source_digest":        str(args, "source_digest"),
		"workload_object_id":   str(args, "workload_object_id"),
	}}
}
