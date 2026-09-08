// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// TestRepositoryCapabilitySupportMatchesIntendedProfileCeiling pins the
// repository family's registered support rows to the values production is
// meant to enforce.
//
// It states those values as literals rather than comparing the registry
// against repository.ContextOverviewSupport and repository.CatalogSupport.
// Comparing the two would be tautological: root's baseCapabilityMatrix
// registers exactly those declarations, so the assertion would hold no matter
// what they said. Writing the intent out independently is what makes editing
// the family's declaration fail here.
//
// The gap this closes: the family's own TestMain has to register these
// capabilities itself, because go test on that package cannot link root
// (#6060). See semantic_search_capability_support_test.go, the template this
// test copies.
//
// platform_impact.context_overview must stay refused on local_lightweight
// (nil ceiling with a local_authoritative floor): repository context routes
// require authoritative platform context truth, so a non-nil ceiling would
// have the routes answer from a corpus that does not exist instead of
// refusing. platform_impact.catalog instead serves local_lightweight callers
// a derived ceiling with no required profile: the catalog is a content index,
// not authoritative graph truth.
func TestRepositoryCapabilitySupportMatchesIntendedProfileCeiling(t *testing.T) {
	t.Parallel()

	overview, ok := querycontract.CapabilitySupportFor(repository.ContextOverviewCapability)
	if !ok {
		t.Fatalf("CapabilitySupportFor(%q) ok = false, want true; the capability is not registered", repository.ContextOverviewCapability)
	}
	if overview.LocalLightweightMax != nil {
		t.Errorf(
			"LocalLightweightMax = %v, want nil (repository context routes require authoritative truth, so the routes must be unsupported on local_lightweight, not degraded)",
			*overview.LocalLightweightMax,
		)
	}
	for name, ceiling := range map[string]*querycontract.TruthLevel{
		"LocalAuthoritativeMax": overview.LocalAuthoritativeMax,
		"LocalFullStackMax":     overview.LocalFullStackMax,
		"ProductionMax":         overview.ProductionMax,
	} {
		if ceiling == nil {
			t.Errorf("%s = nil, want %q", name, querycontract.TruthLevelExact)
			continue
		}
		if *ceiling != querycontract.TruthLevelExact {
			t.Errorf("%s = %q, want %q", name, *ceiling, querycontract.TruthLevelExact)
		}
	}
	if overview.RequiredProfile != querycontract.ProfileLocalAuthoritative {
		t.Errorf("RequiredProfile = %q, want %q", overview.RequiredProfile, querycontract.ProfileLocalAuthoritative)
	}

	catalog, ok := querycontract.CapabilitySupportFor(repository.CatalogCapability)
	if !ok {
		t.Fatalf("CapabilitySupportFor(%q) ok = false, want true; the capability is not registered", repository.CatalogCapability)
	}
	if catalog.LocalLightweightMax == nil {
		t.Errorf("LocalLightweightMax = nil, want %q (the catalog serves a derived ceiling to local_lightweight, it does not refuse)", querycontract.TruthLevelDerived)
	} else if *catalog.LocalLightweightMax != querycontract.TruthLevelDerived {
		t.Errorf("LocalLightweightMax = %q, want %q", *catalog.LocalLightweightMax, querycontract.TruthLevelDerived)
	}
	for name, ceiling := range map[string]*querycontract.TruthLevel{
		"LocalAuthoritativeMax": catalog.LocalAuthoritativeMax,
		"LocalFullStackMax":     catalog.LocalFullStackMax,
		"ProductionMax":         catalog.ProductionMax,
	} {
		if ceiling == nil {
			t.Errorf("%s = nil, want %q", name, querycontract.TruthLevelExact)
			continue
		}
		if *ceiling != querycontract.TruthLevelExact {
			t.Errorf("%s = %q, want %q", name, *ceiling, querycontract.TruthLevelExact)
		}
	}
	if catalog.RequiredProfile != "" {
		t.Errorf("RequiredProfile = %q, want empty (the catalog names no required profile)", catalog.RequiredProfile)
	}
}
