package telemetry

import "slices"

const (
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
			spanNames = slices.Insert(spanNames, idx+1,
				SpanQueryCICDRunCorrelations,
				SpanQueryCICDRunCorrelationAggregate,
			)
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryDependencies {
			spanNames = slices.Insert(spanNames, idx+1,
				SpanQueryCICDRunCorrelations,
				SpanQueryCICDRunCorrelationAggregate,
			)
			return
		}
	}
	spanNames = append(spanNames,
		SpanQueryCICDRunCorrelations,
		SpanQueryCICDRunCorrelationAggregate,
	)
}
