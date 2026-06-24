// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

const (
	// SpanQueryServiceCatalogCorrelations wraps reducer-owned service catalog
	// ownership and drift correlation reads from durable facts.
	SpanQueryServiceCatalogCorrelations = "query.service_catalog_correlations"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryCICDRunCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryServiceCatalogCorrelations)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryServiceCatalogCorrelations)
}
