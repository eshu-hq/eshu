// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func cicdRunCorrelationAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/ci-cd/run-correlations/count", query: map[string]string{
		"scope_id":        str(args, "scope_id"),
		"repository_id":   str(args, "repository_id"),
		"commit_sha":      str(args, "commit_sha"),
		"provider":        str(args, "provider"),
		"artifact_digest": str(args, "artifact_digest"),
		"image_ref":       str(args, "image_ref"),
		"environment":     str(args, "environment"),
		"outcome":         str(args, "outcome"),
	}}
}

func cicdRunCorrelationAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "outcome"
	}
	return &route{method: "GET", path: "/api/v0/ci-cd/run-correlations/inventory", query: map[string]string{
		"group_by":        groupBy,
		"scope_id":        str(args, "scope_id"),
		"repository_id":   str(args, "repository_id"),
		"commit_sha":      str(args, "commit_sha"),
		"provider":        str(args, "provider"),
		"artifact_digest": str(args, "artifact_digest"),
		"image_ref":       str(args, "image_ref"),
		"environment":     str(args, "environment"),
		"outcome":         str(args, "outcome"),
		"limit":           strconv.Itoa(intOr(args, "limit", 100)),
		"offset":          strconv.Itoa(intOr(args, "offset", 0)),
	}}
}
