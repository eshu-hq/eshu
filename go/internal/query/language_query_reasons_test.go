// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestSourceBackendForTruthBasisDefaultReturnsUnavailableSentinel is the
// #5761 P3-1 review-fix regression. sourceBackendForTruthBasis
// (language_query_reasons.go) only has explicit cases for the three
// TruthBasis values language_queries.go's dispatch branches actually pass in
// today (TruthBasisAuthoritativeGraph, TruthBasisHybrid,
// TruthBasisContentIndex); its default arm used to return "", which is not a
// member of the OpenAPI LanguageQueryResponse.source_backend enum
// (openapi_components.go) and so would silently violate the documented
// contract if a future basis (e.g. TruthBasisSemanticFacts,
// TruthBasisRuntimeState, or an unrecognized value) ever reached this
// function. It now returns the "unavailable" sentinel instead, matching the
// fallback code_relationship_story.go:301 already uses.
func TestSourceBackendForTruthBasisDefaultReturnsUnavailableSentinel(t *testing.T) {
	t.Parallel()

	for _, basis := range []TruthBasis{TruthBasisSemanticFacts, TruthBasisRuntimeState, TruthBasis("some_future_basis")} {
		if got, want := sourceBackendForTruthBasis(basis), "unavailable"; got != want {
			t.Fatalf("sourceBackendForTruthBasis(%q) = %q, want %q", basis, got, want)
		}
	}
}
