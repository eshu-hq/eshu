package telemetry

import "slices"

const (
	// SpanQueryPackageRegistryCorrelations wraps package publication and
	// consumption correlation reads from reducer facts.
	SpanQueryPackageRegistryCorrelations = "query.package_registry_correlations"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryDependencies {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryPackageRegistryCorrelations)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryPackageRegistryCorrelations)
}
