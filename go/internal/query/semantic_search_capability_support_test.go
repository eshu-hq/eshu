// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/semanticsearch"
)

// TestSemanticSearchCapabilitySupportMatchesIntendedProfileCeiling pins the
// semantic-search capability's registered support row to the values production
// is meant to enforce.
//
// It states those values as literals rather than comparing the registry against
// semanticsearch.Support. Comparing the two would be tautological: root's
// baseCapabilityMatrix registers exactly that var, so the assertion would hold
// no matter what the var said. Writing the intent out independently is what
// makes editing the family's declaration fail here.
//
// The gap this closes: the family's own TestMain has to register this
// capability itself, because go test on that package cannot link root (#6060).
// While the two sides each held their own copy of the five fields, flipping
// LocalLightweightMax from nil to a truth level and RequiredProfile from
// local_authoritative down to local_lightweight left
// `go test ./internal/query/semanticsearch/` at exit 0 — its profile-gate tests
// passing against a capability profile production did not serve. There is now
// one declaration and this test guards its contents.
//
// LocalLightweightMax must stay nil. Nil means unsupported, and that profile
// has no curated search-document index at all, so a non-nil ceiling would have
// the route answer from a corpus that does not exist instead of refusing.
func TestSemanticSearchCapabilitySupportMatchesIntendedProfileCeiling(t *testing.T) {
	t.Parallel()

	support, ok := querycontract.CapabilitySupportFor(semanticsearch.Capability)
	if !ok {
		t.Fatalf("CapabilitySupportFor(%q) ok = false, want true; the capability is not registered", semanticsearch.Capability)
	}

	if support.LocalLightweightMax != nil {
		t.Errorf(
			"LocalLightweightMax = %v, want nil (local_lightweight has no curated search-document index, so the route must be unsupported there, not degraded)",
			*support.LocalLightweightMax,
		)
	}

	derivedCeilings := map[string]*querycontract.TruthLevel{
		"LocalAuthoritativeMax": support.LocalAuthoritativeMax,
		"LocalFullStackMax":     support.LocalFullStackMax,
		"ProductionMax":         support.ProductionMax,
	}
	for name, ceiling := range derivedCeilings {
		if ceiling == nil {
			t.Errorf("%s = nil, want %q", name, querycontract.TruthLevelDerived)
			continue
		}
		if *ceiling != querycontract.TruthLevelDerived {
			t.Errorf("%s = %q, want %q", name, *ceiling, querycontract.TruthLevelDerived)
		}
	}

	if got, want := support.RequiredProfile, querycontract.ProfileLocalAuthoritative; got != want {
		t.Errorf("RequiredProfile = %q, want %q", got, want)
	}
}
