// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// packageRegistryAggregateCapability was package_registry_aggregates_handler.go's
// unexported constant before that file moved into packagereg with the rest
// of the family (#6060); it is now unexported inside packagereg and
// unreachable from here, so this file keeps the literal string it names
// ("package_registry.packages.aggregate") instead, matching the two sibling
// entries below that already use a literal capability string.
func init() {
	capabilityMatrix["package_registry.correlations.list"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
	capabilityMatrix["package_registry.dependency_chains.list"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
	capabilityMatrix["package_registry.packages.aggregate"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
