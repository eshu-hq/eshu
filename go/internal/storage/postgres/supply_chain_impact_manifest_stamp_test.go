// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestFindInFlightNonMarkerScopesQueryDerivesFromTheInFlightQuery guards the
// derivation: the marker-free query is the shipped in-flight query up to its
// ORDER BY, plus exactly the marker exclusion. If the base query loses its
// ORDER BY anchor, strings.Replace would silently return it unchanged and the
// exclusion would vanish; this test fails instead.
func TestFindInFlightNonMarkerScopesQueryDerivesFromTheInFlightQuery(t *testing.T) {
	t.Parallel()

	anchor := "ORDER BY work.scope_id"
	cut := strings.Index(findInFlightSupplyChainImpactScopesQuery, anchor)
	if cut < 0 {
		t.Fatalf("base in-flight query lost its %q anchor", anchor)
	}
	if !strings.HasPrefix(findInFlightNonMarkerScopesQuery, findInFlightSupplyChainImpactScopesQuery[:cut]) {
		t.Fatal("marker-free query does not start with the shipped in-flight query")
	}
	if !strings.Contains(findInFlightNonMarkerScopesQuery, "COALESCE(work.payload->>'trigger', '') <> 'manifest_dependency'") {
		t.Fatalf("marker-free query lacks the marker exclusion:\n%s", findInFlightNonMarkerScopesQuery)
	}
	if findInFlightNonMarkerScopesQuery == findInFlightSupplyChainImpactScopesQuery {
		t.Fatal("marker-free query equals the unfiltered query")
	}
}
