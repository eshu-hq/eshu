// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagereg

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's six capabilities with querycontract
// before any test runs, then runs the suite.
//
// In production these capabilities are registered by root package query's
// init() functions -- three in contract_package_registry.go
// (package_registry.correlations.list, package_registry.dependency_chains.list,
// package_registry.packages.aggregate) and three in
// contract_capability_matrix.go's baseCapabilityMatrix
// (package_registry.packages.list, package_registry.versions.list,
// package_registry.dependencies.list). Root always links into the production
// binary (it owns the router), so those init() functions always run there and
// production is unaffected by this file.
//
// `go test ./internal/query/packagereg` never links root package query:
// packagereg cannot import it without an import cycle (root's
// package_registry_alias.go already imports packagereg for the
// PackageRegistryHandler compatibility alias, #6060), so root's init()
// functions never run in this test binary. Without this TestMain, every
// handler test in this package fails with the capability gate's
// unsupported_capability 501 -- not because the handler is broken, but
// because no capability was ever registered for it to check against. The
// values below are copied faithfully from the two root files named above;
// they must be kept in sync if either changes.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
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
	os.Exit(m.Run())
}
