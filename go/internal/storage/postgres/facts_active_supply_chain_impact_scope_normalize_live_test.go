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
// repository_id) via new placeholders $16-$20. This test proves two of the
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

// TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByAdvisoryIDOnlyLive
// is the #5466 round-4 review F-10 fix proof: advisory_id had NO load-path
// predicate at all before this fix -- not even a stale exact-match one, the
// way package_id/purl/cve_id/subject_digest/repository_id did before F-6.
// vulnerability.cve/affected_package carry a raw top-level advisory_id
// field (indexed by fact_records_vulnerability_active_advisory_lookup_v2_idx
// -- vulnerability.affected_product shares that index's fact_kind list but
// does NOT carry the field itself: a partial index's kind list constrains
// which rows get indexed, not which payloads have the key; see
// AffectedProduct in sdk/go/factschema/vulnerability/v1, #5466 round-5
// review F-12), but supplyChainCVEID prefers cve_id over advisory_id
// (firstNonBlank(cve_id, advisory_id)), so a suppression scoped ONLY by an
// advisory_id distinct from any cve_id (e.g. a GHSA ID) was unreachable by
// this query even though scopeAnchorMatches, suppressionScopeIsEmpty, and
// the reasons string in supply_chain_suppression_reasons.go all accept/
// advertise advisory_id as a sufficient sole anchor. This test covers ONLY
// the raw advisory_id payload field on the vulnerability.suppression
// fact's own scope; it does NOT cover deriving AdvisoryIDs from any other
// fact kind's payload beyond vulnerability.cve/affected_package (see
// supplyChainImpactFilter), and it does NOT change how AdvisoryID is
// derived on a SupplyChainImpactFinding at classification time (that
// remains firstNonBlank(cve.advisoryID, cve.cveID) elsewhere).
func TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByAdvisoryIDOnlyLive(t *testing.T) {
	db := newSupplyChainImpactScopeLiveTestDB(t, "eshu_5466_suppression_scope_advisory_live")

	// SUP-ADVISORY-ONLY: scoped PURELY by advisory_id -- no cve_id,
	// package_id, purl, subject_digest, or repository_id at all. The exact
	// shape scopeAnchorMatches/suppressionScopeIsEmpty already accept but
	// this query's WHERE clause had no predicate that could ever match.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:advisory-only", "advisory-only",
		`{"suppression_id":"SUP-ADVISORY-ONLY","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"advisory_id":"GHSA-demo-1111-2222"}}`)
	// SUP-ADVISORY-NOISE: a different advisory ID entirely, must never
	// match the filter below.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:advisory-noise", "advisory-noise",
		`{"suppression_id":"SUP-ADVISORY-NOISE","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"advisory_id":"GHSA-other-9999-8888"}}`)

	store := NewFactStore(SQLDB{DB: db})

	// The reducer derives AdvisoryIDs from an already-loaded
	// vulnerability.cve fact's raw top-level advisory_id field (see
	// supplyChainImpactFilter), independent of that same fact's cve_id
	// (which is collected separately into CVEIDs).
	loaded, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
		AdvisoryIDs: []string{"GHSA-demo-1111-2222"},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("len = %d, want %d (the advisory-only-scoped suppression, and only it): %#v", got, want, loaded)
	}
	if loaded[0].FactID != "vuln-suppression:advisory-only" {
		t.Fatalf("FactID = %q, want vuln-suppression:advisory-only: %#v", loaded[0].FactID, loaded[0])
	}
}
