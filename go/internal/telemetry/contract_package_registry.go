// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

const (
	// SpanQueryPackageRegistryCorrelations wraps package publication and
	// consumption correlation reads from reducer facts.
	SpanQueryPackageRegistryCorrelations = "query.package_registry_correlations"
	// SpanQueryPackageRegistryDependencyChains wraps the read-side join that
	// resolves consumer-repo -> package -> publisher-repo dependency chains from
	// reducer consumption and provenance-only publication/ownership correlations.
	SpanQueryPackageRegistryDependencyChains = "query.package_registry_dependency_chains"
	// SpanQueryPackageRegistryAggregate wraps cheap-summary count and
	// inventory aggregates over the (:Package) corpus. Replaces the
	// page-and-iterate caller pattern for ecosystem-level questions like
	// "how many packages per ecosystem?".
	SpanQueryPackageRegistryAggregate = "query.package_registry_aggregate"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryPackageRegistryDependencies {
			spanNames = slices.Insert(
				spanNames, idx+1,
				SpanQueryPackageRegistryCorrelations,
				SpanQueryPackageRegistryDependencyChains,
				SpanQueryPackageRegistryAggregate,
			)
			return
		}
	}
	spanNames = append(
		spanNames,
		SpanQueryPackageRegistryCorrelations,
		SpanQueryPackageRegistryDependencyChains,
		SpanQueryPackageRegistryAggregate,
	)
}
