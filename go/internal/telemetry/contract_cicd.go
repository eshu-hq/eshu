package telemetry

import "slices"

const (
	// SpanQueryCICDRunCorrelations wraps reducer-owned CI/CD run correlation
	// reads from durable facts.
	SpanQueryCICDRunCorrelations = "query.ci_cd_run_correlations"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryCICDRunCorrelations)
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryDependencies {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryCICDRunCorrelations)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryCICDRunCorrelations)
}
