// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// completeKeyPresenceFillMarkerForTop1 completes the key-presence fill marker
// on the disposable database, so the keyed tick takes the marker-gated TOP-1
// path. The fill's own live test covers the fill sweep and its
// disagreement-0 verification; the tick only reads the marker row, and the
// writer fills every support's bits at write time, so setting the stamp here
// exercises the tick's marker-gated text, not the fill.
func completeKeyPresenceFillMarkerForTop1(t *testing.T, h *keyedHarness) {
	t.Helper()
	h.exec(`INSERT INTO supply_chain_impact_finding_supports_backfill AS b
(marker_name, contract_version, completed_at) VALUES ($1, $2, now())
ON CONFLICT (marker_name) DO UPDATE SET completed_at = EXCLUDED.completed_at,
contract_version = EXCLUDED.contract_version`,
		postgres.SupplyChainImpactKeyPresenceFillMarker, postgres.SupplyChainImpactKeyPresenceFillContractVersion)
}

// TestSupplyChainImpactWinnersKeyedTop1MatchesRecomputeLive is the lane-A
// accuracy proof (ruling proof 2, seed classes 2a): every change class goes
// through its production capture path with the key-presence marker complete,
// one TOP-1 keyed tick runs, and the winners table must equal a fresh full
// recompute: same keys, same 23 data columns.
//
// The writer collapses same-key findings of one generation to one fact, so
// multi-copy keys are seeded across scopes (the canonical key carries no
// scope). The decisive classes are the ones a probe-stage prune would get
// wrong: an ineligible top-ranked copy (association present, dynamic class,
// derived-image scope) and a dead-generation top-ranked copy must both lose
// to a live copy; the operator overlay, advisory-only keys, the
// has_payload_finding_id tiebreak, and JSON-null vs missing key components
// must agree byte for byte. A class the capture misses fails at its own
// step, and the explicit winner assertions below prove each decisive class
// beyond the parity equality.
func TestSupplyChainImpactWinnersKeyedTop1MatchesRecomputeLive(t *testing.T) {
	h := newKeyedHarness(t, "#11120 lane-A TOP-1 parity proof")
	const (
		scopeA  = "vuln-intel://top1/a"
		scopeB  = "vuln-intel://top1/b"
		derived = "image://derived/top1"
	)
	h.seedScope(scopeA, scopeA+"/g1")
	h.seedScope(scopeB, scopeB+"/g0")
	h.seedScope(derived, derived+"/g1")
	h.seedOperator("top1-op-g1", "", map[string]map[string]any{
		"top1-op-noise": {"cve_id": "CVE-2026-0999", "suppression_state": "accepted_risk", "repository_id": "repo-9805"},
	})

	// Dead-generation top rank: the priority-999 copy lives in the
	// superseded g0, so the live g1 copy must win.
	h.write(scopeB, scopeB+"/g0", keyedFinding("CVE-2026-9101", "pkg:npm/top1-dead", 999))
	h.addGeneration(scopeB, scopeB+"/g1", "pending")
	h.write(scopeB, scopeB+"/g1", keyedFinding("CVE-2026-9101", "pkg:npm/top1-dead", 60))
	h.activate(scopeB, scopeB+"/g0", scopeB+"/g1")

	// Multi-copy keys across scopes (same canonical key, distinct facts):
	// the assoc/class tops live in scopeA, their live rank-2 copies in
	// scopeB; the multi-version key splits one identifier into two keys.
	h.write(scopeA, scopeA+"/g1",
		keyedFinding("CVE-2026-9102", "pkg:npm/top1-assoc", 950),
		keyedFinding("CVE-2026-9103", "pkg:npm/top1-class", 940),
		keyedAdvisoryFinding("GHSA-top1-9105", "pkg:npm/top1-adv", 30),
		keyedFinding("CVE-2026-9106", "pkg:npm/top1-multi", 60),
		top1ObservedVersion("CVE-2026-9106", "pkg:npm/top1-multi", 70, "2.0.0"),
		keyedFinding("CVE-2026-9107", "pkg:npm/top1-derived", 30),
	)
	h.write(scopeB, scopeB+"/g1",
		keyedFinding("CVE-2026-9101", "pkg:npm/top1-dead", 60),
		keyedFinding("CVE-2026-9102", "pkg:npm/top1-assoc", 50),
		keyedFinding("CVE-2026-9103", "pkg:npm/top1-class", 40),
		keyedAdvisoryFinding("GHSA-top1-9105", "pkg:npm/top1-adv", 31),
		keyedFinding("CVE-2026-9106", "pkg:npm/top1-multi", 61),
		top1ObservedVersion("CVE-2026-9106", "pkg:npm/top1-multi", 71, "2.0.0"),
	)
	// Derived-image top rank: the 930 copy is excluded.
	h.write(derived, derived+"/g1", keyedFinding("CVE-2026-9107", "pkg:npm/top1-derived", 930))

	// Operator overlay on an existing key, with a live expiry.
	h.seedOperator("top1-op-g2", "top1-op-g1", map[string]map[string]any{
		"top1-op-real": {
			"cve_id": "CVE-2026-9102", "package_id": "pkg:npm/top1-assoc", "impact_status": "affected_exact",
			"observed_version": "1.0.0", "repository_id": "repo-9805", "suppression_state": "accepted_risk",
			"suppression": map[string]any{"expires_at": "2099-01-01T00:00:00Z"},
		},
		"top1-op-noise": {"cve_id": "CVE-2026-0999", "suppression_state": "accepted_risk", "repository_id": "repo-9805"},
	})

	// The harness writer leaves supports to the test: bring them to what the
	// production writer would have written, including the dead generation's
	// copies (the live_gens join, not the supports table, excludes them).
	h.syncSupports()

	// Ineligible top ranks: the association and the dynamic class land after
	// the findings they filter, the way the scanner flow writes them.
	h.dropForeignKeysOf("scanner_image_result_fact_associations")
	h.exec(`INSERT INTO scanner_image_result_fact_associations
(finding_fact_id, publication_id, scan_scope_id, scan_generation_id, tenant_id, workspace_id)
VALUES ($1, 'pub-top1', 'scan-top1', 'scan-gen', 'tenant', 'workspace')
ON CONFLICT DO NOTHING`,
		top1FactID(t, h, scopeA, "CVE-2026-9102", 950))
	h.exec(`INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle)
VALUES ($1, 'derived', 'active')
ON CONFLICT DO NOTHING`,
		top1FactID(t, h, scopeA, "CVE-2026-9103", 940))

	completeKeyPresenceFillMarkerForTop1(t, h)
	got := h.keyed()
	if got.ProbeVariant != supplychaincore.WinnersProbeVariantTop1 {
		t.Fatalf("keyed tick probe variant = %q, want %q (marker complete)", got.ProbeVariant, supplychaincore.WinnersProbeVariantTop1)
	}
	h.assertParity("TOP-1 tick over the decisive seed classes")
	top1AssertDecisiveWinners(t, h)

	t.Run("priority rewrite flips a live winner", func(t *testing.T) {
		h.write(scopeB, scopeB+"/g1",
			keyedFinding("CVE-2026-9101", "pkg:npm/top1-dead", 60),
			keyedFinding("CVE-2026-9102", "pkg:npm/top1-assoc", 51),
			keyedFinding("CVE-2026-9103", "pkg:npm/top1-class", 40),
			keyedAdvisoryFinding("GHSA-top1-9105", "pkg:npm/top1-adv", 31),
			keyedFinding("CVE-2026-9106", "pkg:npm/top1-multi", 61),
			top1ObservedVersion("CVE-2026-9106", "pkg:npm/top1-multi", 71, "2.0.0"),
		)
		h.syncSupports()
		if got := h.keyed(); got.Updated == 0 {
			t.Fatalf("keyed tick = %+v, want the flipped winner updated", got)
		}
		h.assertParity("TOP-1 priority rewrite")
		top1AssertDecisiveWinners(t, h)
	})

	t.Run("operator overlay removal", func(t *testing.T) {
		h.seedOperator("top1-op-g3", "top1-op-g2", map[string]map[string]any{
			"top1-op-noise": {"cve_id": "CVE-2026-0999", "suppression_state": "accepted_risk", "repository_id": "repo-9805"},
		})
		h.syncSupports()
		h.keyed()
		h.assertParity("TOP-1 operator removal")
	})
}

// top1AssertDecisiveWinners proves each decisive seed class beyond parity:
// the dead, associated, classified, and derived top copies must all lose,
// the operator overlay must attach to the assoc key, and the multi-version
// identifier must emit both keys.
func top1AssertDecisiveWinners(t *testing.T, h *keyedHarness) {
	t.Helper()
	winner := func(cve, pkg string) (factID, scopeID, generationID, suppression string) {
		t.Helper()
		key := top1CanonicalKey(t, h, cve, pkg)
		if err := h.db.QueryRowContext(h.ctx, `SELECT w.winner_fact_id, w.winner_scope_id,
(SELECT s.generation_id FROM supply_chain_impact_finding_supports s WHERE s.fact_id = w.winner_fact_id),
w.suppression_state FROM supply_chain_impact_canonical_winners w WHERE w.canonical_key = $1`, key).
			Scan(&factID, &scopeID, &generationID, &suppression); err != nil {
			t.Fatalf("read winner for %s %s: %v", cve, pkg, err)
		}
		return factID, scopeID, generationID, suppression
	}
	if _, scope, gen, _ := winner("CVE-2026-9101", "pkg:npm/top1-dead"); scope != "vuln-intel://top1/b" || gen != "vuln-intel://top1/b/g1" {
		t.Errorf("dead-top key won by %s %s, want the live g1 copy", scope, gen)
	}
	if _, scope, _, suppression := winner("CVE-2026-9102", "pkg:npm/top1-assoc"); scope != "vuln-intel://top1/b" {
		t.Errorf("assoc-top key won by %s, want the scopeB live copy", scope)
	} else if suppression != "accepted_risk" {
		t.Errorf("assoc key suppression = %q, want the operator overlay accepted_risk", suppression)
	}
	if _, scope, _, _ := winner("CVE-2026-9103", "pkg:npm/top1-class"); scope != "vuln-intel://top1/b" {
		t.Errorf("class-top key won by %s, want the scopeB live copy", scope)
	}
	if _, scope, _, _ := winner("CVE-2026-9107", "pkg:npm/top1-derived"); scope != "vuln-intel://top1/a" {
		t.Errorf("derived-top key won by %s, want the scopeA live copy", scope)
	}
	multi := 0
	for _, observed := range []string{"1.0.0", "2.0.0"} {
		var n int
		if err := h.db.QueryRowContext(h.ctx, `SELECT count(*) FROM supply_chain_impact_canonical_winners
WHERE package_id = 'pkg:npm/top1-multi' AND observed_version = $1`, observed).Scan(&n); err != nil {
			t.Fatalf("count multi winners at %s: %v", observed, err)
		}
		multi += n
	}
	if multi != 2 {
		t.Errorf("multi-version identifier produced %d winners, want 2 (one per observed version)", multi)
	}
}

// top1CanonicalKey resolves one seeded key's canonical key through its
// writer-filled support row.
func top1CanonicalKey(t *testing.T, h *keyedHarness, cve, pkg string) string {
	t.Helper()
	var key string
	if err := h.db.QueryRowContext(h.ctx, `SELECT canonical_key FROM supply_chain_impact_finding_supports
WHERE cve_id = $1 AND package_id = $2 LIMIT 1`, cve, pkg).Scan(&key); err != nil {
		t.Fatalf("resolve canonical key for %s %s: %v", cve, pkg, err)
	}
	return key
}

// top1ObservedVersion returns a keyed-style finding at a non-default
// observed version, for the multi-version seed class: one identifier
// fanning out to two canonical keys.
func top1ObservedVersion(cve, pkg string, priority int, observed string) supplychaincore.SupplyChainImpactFinding {
	f := keyedFinding(cve, pkg, priority)
	f.ObservedVersion = observed
	return f
}

// top1FactID resolves one seeded finding's fact_id by its scope, cve, and
// priority, for the classification and association seeds.
func top1FactID(t *testing.T, h *keyedHarness, scope, cve string, priority int) string {
	t.Helper()
	var id string
	if err := h.db.QueryRowContext(h.ctx, `SELECT fact_id FROM supply_chain_impact_finding_supports
WHERE scope_id = $1 AND cve_id = $2 AND priority_score = $3`, scope, cve, priority).Scan(&id); err != nil {
		t.Fatalf("resolve fact_id for %s %s priority %d: %v", scope, cve, priority, err)
	}
	return id
}
