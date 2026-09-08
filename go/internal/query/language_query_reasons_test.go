// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestSourceBackendForTruthBasisNoBackendReadIsItsOwnValue is the #6544
// regression. The empty-grant page's source_backend used to be the
// "unavailable" sentinel, reused outside the meaning the public table in
// docs/public/reference/language-query-dsl.md gives it, because
// sourceBackendForTruthBasis had no case for a page produced without a read and
// "unavailable" was the default arm it fell into. It now has one, and the value
// is written out here rather than compared to noBackendReadSourceBackend: an
// expectation read from the constant under test passes whatever that constant
// becomes, including a silent return to "unavailable".
func TestSourceBackendForTruthBasisNoBackendReadIsItsOwnValue(t *testing.T) {
	t.Parallel()

	if got, want := sourceBackendForTruthBasis(TruthBasisNoBackendRead), "no_backend_read"; got != want {
		t.Fatalf("sourceBackendForTruthBasis(%q) = %q, want %q; a no-read page must not reuse the "+
			"unrecognized-basis sentinel", TruthBasisNoBackendRead, got, want)
	}
}

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
