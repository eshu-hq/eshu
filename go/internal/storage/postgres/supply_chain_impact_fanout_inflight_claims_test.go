// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestInFlightClaimQueriesDeriveFromTheShippedInFlightQuery guards the #10972
// D4b derivation. Set B must be the shipped marker-free in-flight read plus the
// claim identity column and nothing else, and set A must be set B minus only
// the active-generation arm. strings.Replace returns its input unchanged when
// an anchor moves, which would silently drop the claim column or keep the arm;
// undoing each edit must give back the source query exactly.
func TestInFlightClaimQueriesDeriveFromTheShippedInFlightQuery(t *testing.T) {
	t.Parallel()

	if !strings.Contains(findInFlightNonMarkerClaimsQuery, "SELECT DISTINCT work.scope_id, work.last_attempt_at\n") {
		t.Fatalf("set B lacks the claim identity column:\n%s", findInFlightNonMarkerClaimsQuery)
	}
	if !strings.Contains(findInFlightNonMarkerClaimsQuery, "ORDER BY work.scope_id, work.last_attempt_at") {
		t.Fatalf("set B lacks its ORDER BY on the claim column:\n%s", findInFlightNonMarkerClaimsQuery)
	}
	if undone := strings.ReplaceAll(findInFlightNonMarkerClaimsQuery, ", work.last_attempt_at", ""); undone != findInFlightNonMarkerScopesQuery {
		t.Fatalf("set B is not the shipped in-flight read plus the claim column:\n%s", findInFlightNonMarkerClaimsQuery)
	}
	if !strings.Contains(findInFlightNonMarkerClaimsQuery, inFlightActiveGenerationPredicate) {
		t.Fatal("set B lost the active-generation arm")
	}
	if strings.Contains(snapshotInFlightNonMarkerClaimsQuery, "active_generation_id") {
		t.Fatalf("set A still limits to the active generation:\n%s", snapshotInFlightNonMarkerClaimsQuery)
	}
	cut := strings.Index(findInFlightNonMarkerClaimsQuery, inFlightActiveGenerationPredicate)
	rebuilt := snapshotInFlightNonMarkerClaimsQuery[:cut] + inFlightActiveGenerationPredicate + snapshotInFlightNonMarkerClaimsQuery[cut:]
	if rebuilt != findInFlightNonMarkerClaimsQuery {
		t.Fatalf("set A is not set B minus only the active-generation arm:\n%s", snapshotInFlightNonMarkerClaimsQuery)
	}
	for _, query := range []string{findInFlightNonMarkerClaimsQuery, snapshotInFlightNonMarkerClaimsQuery} {
		if !strings.Contains(query, "<> 'manifest_dependency'") || !strings.Contains(query, "work.status IN ('claimed', 'running')") {
			t.Fatalf("claim read lost the marker exclusion or the in-flight status filter:\n%s", query)
		}
	}
}
