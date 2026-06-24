// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

const (
	// SpanQueryObservabilityCoverageCorrelations wraps reducer-owned
	// observability coverage correlation reads from durable facts (which
	// monitored resources or services have alarm/dashboard/log/trace coverage
	// versus which are gaps).
	SpanQueryObservabilityCoverageCorrelations = "query.observability_coverage_correlations"
)

// init lands this span after the Kubernetes correlation span when that surface
// is present, otherwise after service catalog. That keeps the frozen read-model
// span order stable as the query surface grows.
func init() {
	for idx, name := range spanNames {
		if name == SpanQueryKubernetesCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryObservabilityCoverageCorrelations)
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryServiceCatalogCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryObservabilityCoverageCorrelations)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryObservabilityCoverageCorrelations)
}
