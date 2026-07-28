// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByLowercaseCVEIDAndPaddedPURLLive
// is the #5466 round-3 review F-6 fix proof: scopeAnchorMatches
// (go/internal/reducer/supply_chain_suppression_scope_match.go) compares
// scope.CVEID and scope.PURL with strings.TrimSpace + strings.EqualFold, so
// a payload authored with different case or padding than the reducer's
// derived filter decodes and matches in Go, but the ORIGINAL exact-match
// ->'scope'->>'cve_id'/'purl' = ANY($3/$2) predicates could never SELECT
// it -- the identical defect class P1-1/F-4 fixed for
// environment/workload_id/service_id, now closed for the five sibling
// suppression-scope anchors (package_id, purl, cve_id, subject_digest,
// repository_id) via new placeholders $15-$19. This test proves two of the
// five on the real load path: a lowercase CVE ID and a whitespace-padded
// PURL.
func TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByLowercaseCVEIDAndPaddedPURLLive(t *testing.T) {
	db := newSupplyChainImpactScopeLiveTestDB(t, "eshu_5466_suppression_scope_normalize_live")

	// SUP-LOWERCASE-CVE: payload names the CVE ID in lowercase, never the
	// uppercase form vulnerability.cve facts conventionally carry (e.g.
	// "CVE-2024-11001" in the golden corpus fixtures).
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:lowercase-cve", "lowercase-cve",
		`{"suppression_id":"SUP-LOWERCASE-CVE","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"cve_id":"cve-2026-1234"}}`)
	// SUP-PADDED-PURL: payload names the PURL padded with leading/trailing
	// ASCII spaces.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:padded-purl", "padded-purl",
		`{"suppression_id":"SUP-PADDED-PURL","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"purl":" pkg:npm/left-pad@1.3.0 "}}`)
	// SUP-NOISE: a different CVE entirely, must never match the filter
	// below regardless of normalization.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:noise", "noise",
		`{"suppression_id":"SUP-NOISE","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"cve_id":"CVE-2026-9999"}}`)

	store := NewFactStore(SQLDB{DB: db})

	// The reducer derives CVEIDs/PURLs from already-loaded
	// vulnerability.cve/vulnerability.affected_package evidence, which
	// conventionally carries uppercase CVE IDs and unpadded PURLs -- the
	// exact filter shape used here.
	loaded, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
		CVEIDs: []string{"CVE-2026-1234"},
		PURLs:  []string{"pkg:npm/left-pad@1.3.0"},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	if got, want := len(loaded), 2; got != want {
		t.Fatalf("len = %d, want %d (the lowercase-CVE and padded-PURL suppressions, and only those): %#v", got, want, loaded)
	}
	gotIDs := map[string]bool{}
	for _, envelope := range loaded {
		gotIDs[envelope.FactID] = true
	}
	for _, wantID := range []string{"vuln-suppression:lowercase-cve", "vuln-suppression:padded-purl"} {
		if !gotIDs[wantID] {
			t.Fatalf("FactID %q missing from loaded set: %#v", wantID, loaded)
		}
	}
}
