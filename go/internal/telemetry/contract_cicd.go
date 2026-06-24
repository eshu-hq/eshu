// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

const (
	// SpanCICDRunObserve wraps one claimed hosted CI/CD run provider target.
	SpanCICDRunObserve = "ci_cd_run.observe"
	// SpanCICDRunFetch wraps one bounded CI/CD run provider fetch.
	SpanCICDRunFetch = "ci_cd_run.fetch"
	// SpanQueryCICDRunCorrelations wraps reducer-owned CI/CD run correlation
	// reads from durable facts.
	SpanQueryCICDRunCorrelations = "query.ci_cd_run_correlations"
	// SpanQueryCICDRunCorrelationAggregate wraps cheap-summary count and
	// inventory aggregates over reducer-owned CI/CD run correlations.
	// Replaces the page-and-iterate caller pattern for ecosystem-level
	// questions like "how many runs ended in each outcome per environment?".
	SpanQueryCICDRunCorrelationAggregate = "query.ci_cd_run_correlation_aggregate"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryCorrelations {
			spanNames = slices.Insert(
				spanNames, idx+1,
				SpanQueryCICDRunCorrelations,
				SpanQueryCICDRunCorrelationAggregate,
			)
			insertCICDRunSourceSpans()
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryDependencies {
			spanNames = slices.Insert(
				spanNames, idx+1,
				SpanQueryCICDRunCorrelations,
				SpanQueryCICDRunCorrelationAggregate,
			)
			insertCICDRunSourceSpans()
			return
		}
	}
	spanNames = append(
		spanNames,
		SpanQueryCICDRunCorrelations,
		SpanQueryCICDRunCorrelationAggregate,
	)
	insertCICDRunSourceSpans()
}

func insertCICDRunSourceSpans() {
	if slices.Contains(spanNames, SpanCICDRunObserve) {
		return
	}
	for idx, name := range spanNames {
		if name == SpanAWSCollectorClaimProcess {
			spanNames = slices.Insert(
				spanNames, idx,
				SpanCICDRunObserve,
				SpanCICDRunFetch,
			)
			return
		}
	}
	spanNames = append(spanNames, SpanCICDRunObserve, SpanCICDRunFetch)
}
