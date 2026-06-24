// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func containerImageIdentityAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/container-images/identities/count", query: map[string]string{
		"digest":               str(args, "digest"),
		"image_ref":            str(args, "image_ref"),
		"source_repository_id": str(args, "source_repository_id"),
		"repository_id":        str(args, "repository_id"),
		"outcome":              str(args, "outcome"),
	}}
}

func containerImageIdentityAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "outcome"
	}
	return &route{method: "GET", path: "/api/v0/supply-chain/container-images/identities/inventory", query: map[string]string{
		"group_by":             groupBy,
		"digest":               str(args, "digest"),
		"image_ref":            str(args, "image_ref"),
		"source_repository_id": str(args, "source_repository_id"),
		"repository_id":        str(args, "repository_id"),
		"outcome":              str(args, "outcome"),
		"limit":                strconv.Itoa(intOr(args, "limit", 100)),
		"offset":               strconv.Itoa(intOr(args, "offset", 0)),
	}}
}
