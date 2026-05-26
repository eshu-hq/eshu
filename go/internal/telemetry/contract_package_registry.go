package telemetry

import "slices"

const (
	// SpanQueryPackageRegistryCorrelations wraps package publication and
	// consumption correlation reads from reducer facts.
	SpanQueryPackageRegistryCorrelations = "query.package_registry_correlations"
	// SpanQueryPackageRegistryAggregate wraps cheap-summary count and
	// inventory aggregates over the (:Package) corpus. Replaces the
	// page-and-iterate caller pattern for ecosystem-level questions like
	// "how many packages per ecosystem?".
	SpanQueryPackageRegistryAggregate = "query.package_registry_aggregate"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryDependencies {
			spanNames = slices.Insert(spanNames, idx+1,
				SpanQueryPackageRegistryCorrelations,
				SpanQueryPackageRegistryAggregate,
			)
			return
		}
	}
	spanNames = append(spanNames,
		SpanQueryPackageRegistryCorrelations,
		SpanQueryPackageRegistryAggregate,
	)
}
