// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// This file registers this family's six capabilities directly with
// querycontract, rather than through root package query's local
// capabilityMatrix compatibility map (contract_capability_matrix.go,
// contract_package_registry.go before #6060): root's package_registry_alias.go
// already imports packagereg for the PackageRegistryHandler compatibility
// alias, so the reverse import needed to reach root's map would cycle.
// querycontract.RegisterCapabilities is the API querycontract's own doc
// comment says new family packages should use in place of the legacy
// SetCapabilitySupport root packages still call.
//
// Registering here rather than in root is also required for correctness, not
// only to avoid the cycle: go test ./internal/query/packagereg never runs
// root package query's init() functions (packagereg cannot import root), so a
// registration left in root would leave every capability gate in this
// package's own tests reporting unsupported_capability -- exactly the sweep
// of 501s that motivated this file. Go still runs packagereg's init() before
// root's own, because Go always finishes an imported package's init() before
// the importing package's, so registration order (and
// contract_capability_matrix_terraform.go's SetCapabilityOrder, which lists
// all six of these capabilities) is unaffected by which package performs the
// registration.
func init() {
	exact := querycontract.TruthLevelExact
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{
			Capability: "package_registry.packages.list",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &exact,
				LocalFullStackMax:     &exact,
				ProductionMax:         &exact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "package_registry.versions.list",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &exact,
				LocalFullStackMax:     &exact,
				ProductionMax:         &exact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "package_registry.dependencies.list",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &exact,
				LocalFullStackMax:     &exact,
				ProductionMax:         &exact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "package_registry.correlations.list",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &exact,
				LocalFullStackMax:     &exact,
				ProductionMax:         &exact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: "package_registry.dependency_chains.list",
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &exact,
				LocalFullStackMax:     &exact,
				ProductionMax:         &exact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
		querycontract.CapabilityRegistration{
			Capability: packageRegistryAggregateCapability,
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   nil,
				LocalAuthoritativeMax: &exact,
				LocalFullStackMax:     &exact,
				ProductionMax:         &exact,
				RequiredProfile:       querycontract.ProfileLocalAuthoritative,
			},
		},
	)
}
