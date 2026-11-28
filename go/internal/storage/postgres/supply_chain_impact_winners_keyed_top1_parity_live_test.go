// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
// has_payload_finding_id tiebreak, JSON-null vs missing vs empty-string key
// components, a non-object payload in a filtered scope, and a mixed-case
// ecosystem spelling must agree byte for byte. A class the capture misses
// fails at its own step, and the explicit winner assertions below prove each
// decisive class beyond the parity equality.
func TestSupplyChainImpactWinnersKeyedTop1MatchesRecomputeLive(t *testing.T) {
	h := newKeyedHarness(t, "#11120 lane-A TOP-1 parity proof")
	const (
		scopeA  = "vuln-intel://top1/a"
		scopeB  = "vuln-intel://top1/b"
		scopeC  = "vuln-intel://top1/c"
		derived = "image://derived/top1"
	)
	h.seedScope(scopeA, scopeA+"/g1")
	h.seedScope(scopeB, scopeB+"/g0")
	h.seedScope(scopeC, scopeC+"/g1")
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
	// The tie and json-shape copies below are planted as raw facts after the
	// first sync (the writer emits no finding_id-less or JSON-shaped
	// payloads on its own); the ecosystem key rides the writer with a
	// mixed-case spelling.
	h.write(scopeA, scopeA+"/g1",
		keyedFinding("CVE-2026-9102", "pkg:npm/top1-assoc", 950),
		keyedFinding("CVE-2026-9103", "pkg:npm/top1-class", 940),
		keyedAdvisoryFinding("GHSA-top1-9105", "pkg:npm/top1-adv", 30),
		keyedFinding("CVE-2026-9106", "pkg:npm/top1-multi", 60),
		top1ObservedVersion("CVE-2026-9106", "pkg:npm/top1-multi", 70, "2.0.0"),
		keyedFinding("CVE-2026-9107", "pkg:npm/top1-derived", 30),
		keyedFinding("CVE-2026-9111", "pkg:npm/top1-tie", 60),
		keyedFinding("CVE-2026-9112", "pkg:npm/top1-json", 61),
		top1MixedCaseEcosystem("CVE-2026-9110", "pkg:npm/top1-eco", 60, "NPM"),
	)
	h.write(scopeB, scopeB+"/g1",
		keyedFinding("CVE-2026-9101", "pkg:npm/top1-dead", 60),
		keyedFinding("CVE-2026-9102", "pkg:npm/top1-assoc", 50),
		keyedFinding("CVE-2026-9103", "pkg:npm/top1-class", 40),
		keyedAdvisoryFinding("GHSA-top1-9105", "pkg:npm/top1-adv", 31),
		keyedFinding("CVE-2026-9106", "pkg:npm/top1-multi", 61),
		top1ObservedVersion("CVE-2026-9106", "pkg:npm/top1-multi", 71, "2.0.0"),
		keyedFinding("CVE-2026-9111", "pkg:npm/top1-tie", 60),
		keyedFinding("CVE-2026-9112", "pkg:npm/top1-json", 62),
	)
	h.write(scopeC, scopeC+"/g1",
		keyedFinding("CVE-2026-9112", "pkg:npm/top1-json", 63),
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

	// Tiebreak inversion: a second scopeA copy drops finding_id while the
	// writer's equal-priority copies carry their own, so it must lose on
	// has_payload_finding_id even though its fact id sorts first (the raw id
	// is chosen to collate before the writer's reducer-prefixed ids, so a
	// flag-blind rank would elect it). The copy is planted as a raw fact;
	// the trigger holiday plus auto-sync derives its support through the
	// writer's own upsert, keeping the plain flag coupled to the payload.
	top1InsertRawCopy(t, h, scopeA, scopeA+"/g1",
		top1FactID(t, h, scopeA, "CVE-2026-9111", 60), "aaa-top1-tie-raw",
		`(s.payload - 'finding_id')`)
	// JSON-null vs missing vs empty-string key components: three raw copies
	// of one key spell observed_version three ways. Every canonical-key
	// argument is COALESCE-wrapped, so all three derive the same key (with
	// an empty observed version); the auto-synced plain columns must match
	// the payload derivation byte for byte. observed_version rides no
	// identifier, so the writer-queued five-tuples already cover the new
	// copies and no extra capture is staged. All three raws live outside
	// scopeB, which the rewrite phase below replaces.
	top1InsertRawCopy(t, h, scopeA, scopeA+"/g1",
		top1FactID(t, h, scopeA, "CVE-2026-9112", 61), "top1-json-raw-a",
		`jsonb_set(jsonb_set(s.payload, '{observed_version}', 'null'), '{priority_score}', '10')`)
	top1InsertRawCopy(t, h, scopeC, scopeC+"/g1",
		top1FactID(t, h, scopeC, "CVE-2026-9112", 63), "top1-json-raw-c1",
		`jsonb_set((s.payload - 'observed_version'), '{priority_score}', '11')`)
	top1InsertRawCopy(t, h, scopeC, scopeC+"/g1",
		top1FactID(t, h, scopeC, "CVE-2026-9112", 63), "top1-json-raw-c2",
		`jsonb_set(jsonb_set(s.payload, '{observed_version}', '""'), '{priority_score}', '12')`)
	top1PatchScalarDerivedTop(t, h, derived)

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
			keyedFinding("CVE-2026-9111", "pkg:npm/top1-tie", 60),
			keyedFinding("CVE-2026-9112", "pkg:npm/top1-json", 62),
		)
		h.syncSupports()
		// The generation write replaces the whole scopeB set, but the raw
		// tie and json copies live in scopeA and scopeC, so they persist;
		// only the scalar derived top is a supports-only row the sync
		// restores from the intact fact, so it is re-patched.
		top1PatchScalarDerivedTop(t, h, derived)
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

// top1InsertRawCopy plants a crafted finding fact beside a writer-written
// seed fact: the new row copies the seed support's payload through payloadSQL
// (an expression over alias s), with a fresh fact id in the same scope and
// generation. h.exec takes the trigger holiday for the raw finding write and
// auto-syncs supports from facts afterwards, so the new support row's plain
// columns stay coupled to its payload through the writer's own upsert. The
// new copy must share the seed's five-tuple identifier (payloadSQL must not
// touch an identifier field): the writer-queued dirty mark for that tuple
// already covers the copy, so no extra capture is staged. The sync check
// fails the seed loudly instead of running the tick on a missing row.
func top1InsertRawCopy(t *testing.T, h *keyedHarness, scope, generation, seedFactID, newFactID, payloadSQL string) {
	t.Helper()
	h.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $1, $2, $3, '`+facts.ReducerSupplyChainImpactFindingFactKind+`', $1, '1.0.0',
'vulnerability_intelligence', $1, now(), now(), false, `+payloadSQL+`
FROM supply_chain_impact_finding_supports AS s WHERE s.fact_id = $4`,
		newFactID, scope, generation, seedFactID)
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_finding_supports WHERE fact_id = $1`, newFactID); n != 1 {
		t.Fatalf("raw copy %s synced %d support rows, want exactly 1", newFactID, n)
	}
}

// top1PatchScalarDerivedTop replaces the derived-scope top copy's payload
// with a scalar, leaving its plain ranking columns intact. Production cannot
// plant this through the writer (the payload stays verbatim, so a scalar
// always recomputes to empty plain columns); the decoupled row proves a
// non-object payload that reaches the narrow and active temps never aborts
// the build and never leaks into the winners, in both arms, because the
// derived-image filter drops it before the final payload merge. A later
// syncSupports restores the verbatim payload from the intact fact, so a
// rewrite phase re-applies it. The payload check fails the seed loudly if
// the patch matched nothing.
func top1PatchScalarDerivedTop(t *testing.T, h *keyedHarness, derived string) {
	t.Helper()
	id := top1FactID(t, h, derived, "CVE-2026-9107", 930)
	h.exec(`UPDATE supply_chain_impact_finding_supports SET payload = '"scalar-payload"'::jsonb
WHERE fact_id = $1`, id)
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_finding_supports
WHERE fact_id = $1 AND payload = '"scalar-payload"'::jsonb`, id); n != 1 {
		t.Fatalf("scalar derived top %s carries a non-scalar payload: the seed matched nothing", id)
	}
}

// top1AssertDecisiveWinners proves each decisive seed class beyond parity:
// the dead, associated, classified, and derived top copies must all lose,
// the operator overlay must attach to the assoc key, the tiebreak must elect
// the finding_id copy at equal priority, the three json-shape copies must
// collapse to one winner with an empty observed version, the scalar derived
// top must stay invisible, the mixed-case ecosystem must keep the fact's own
// spelling, and the multi-version identifier must emit both keys.
func top1AssertDecisiveWinners(t *testing.T, h *keyedHarness) {
	t.Helper()
	winner := func(cve, pkg, observed string) (factID, scopeID, generationID, suppression, ecosystem, findingID string) {
		t.Helper()
		key := top1CanonicalKey(t, h, cve, pkg, observed)
		if err := h.db.QueryRowContext(h.ctx, `SELECT w.winner_fact_id, w.winner_scope_id,
(SELECT s.generation_id FROM supply_chain_impact_finding_supports s WHERE s.fact_id = w.winner_fact_id),
w.suppression_state, w.ecosystem, w.finding_id FROM supply_chain_impact_canonical_winners w WHERE w.canonical_key = $1`, key).
			Scan(&factID, &scopeID, &generationID, &suppression, &ecosystem, &findingID); err != nil {
			t.Fatalf("read winner for %s %s: %v", cve, pkg, err)
		}
		return factID, scopeID, generationID, suppression, ecosystem, findingID
	}
	if _, scope, gen, _, _, _ := winner("CVE-2026-9101", "pkg:npm/top1-dead", "1.0.0"); scope != "vuln-intel://top1/b" || gen != "vuln-intel://top1/b/g1" {
		t.Errorf("dead-top key won by %s %s, want the live g1 copy", scope, gen)
	}
	if _, scope, _, suppression, _, _ := winner("CVE-2026-9102", "pkg:npm/top1-assoc", "1.0.0"); scope != "vuln-intel://top1/b" {
		t.Errorf("assoc-top key won by %s, want the scopeB live copy", scope)
	} else if suppression != "accepted_risk" {
		t.Errorf("assoc key suppression = %q, want the operator overlay accepted_risk", suppression)
	}
	if _, scope, _, _, _, _ := winner("CVE-2026-9103", "pkg:npm/top1-class", "1.0.0"); scope != "vuln-intel://top1/b" {
		t.Errorf("class-top key won by %s, want the scopeB live copy", scope)
	}
	if _, scope, _, _, _, _ := winner("CVE-2026-9107", "pkg:npm/top1-derived", "1.0.0"); scope != "vuln-intel://top1/a" {
		t.Errorf("derived-top key won by %s, want the scopeA live copy", scope)
	}
	// Equal-priority tiebreak: the findless raw copy sorts first by fact
	// id, so only has_payload_finding_id keeps it from winning. The winner
	// must carry a payload finding_id (never the canonical-key fallback).
	tieKey := top1CanonicalKey(t, h, "CVE-2026-9111", "pkg:npm/top1-tie", "1.0.0")
	if factID, _, _, _, _, finding := winner("CVE-2026-9111", "pkg:npm/top1-tie", "1.0.0"); factID == "aaa-top1-tie-raw" {
		t.Errorf("tiebreak elected the findless raw copy: has_payload_finding_id did not outrank fact_id")
	} else if finding == tieKey {
		t.Errorf("tiebreak winner finding_id is the canonical-key fallback: a payload-carried id must win")
	}
	// JSON-null vs missing vs empty-string observed_version: the three raw
	// copies derive the same key, so exactly one winner with an empty
	// observed version, elected by priority (the empty-string copy at 12).
	jsonKey := top1CanonicalKey(t, h, "CVE-2026-9112", "pkg:npm/top1-json", "")
	if _, scope, _, _, _, _ := winner("CVE-2026-9112", "pkg:npm/top1-json", ""); scope != "vuln-intel://top1/c" {
		t.Errorf("json-shape key won by %s, want the scopeC priority-12 copy", scope)
	} else {
		var n int
		if err := h.db.QueryRowContext(h.ctx, `SELECT count(*) FROM supply_chain_impact_canonical_winners
WHERE canonical_key = $1`, jsonKey).Scan(&n); err != nil {
			t.Fatalf("count json-shape winners: %v", err)
		}
		if n != 1 {
			t.Errorf("json-shape key produced %d winners, want 1 (null/missing/'' collapse to one key)", n)
		}
	}
	// The writer's 1.0.0 copies form the sibling key and still elect the
	// priority-63 scopeC copy.
	if _, scope, _, _, _, _ := winner("CVE-2026-9112", "pkg:npm/top1-json", "1.0.0"); scope != "vuln-intel://top1/c" {
		t.Errorf("json sibling key won by %s, want the scopeC priority-63 copy", scope)
	}
	// Mixed-case ecosystem: the winner keeps the fact's own spelling, not
	// the lowercased filter column.
	if _, _, _, _, ecosystem, _ := winner("CVE-2026-9110", "pkg:npm/top1-eco", "1.0.0"); ecosystem != "NPM" {
		t.Errorf("ecosystem winner = %q, want the fact's own spelling NPM", ecosystem)
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
// writer-filled support row, qualified by observed version: the json-shape
// seed fans one identifier out to two keys that share every other column.
func top1CanonicalKey(t *testing.T, h *keyedHarness, cve, pkg, observed string) string {
	t.Helper()
	var key string
	if err := h.db.QueryRowContext(h.ctx, `SELECT canonical_key FROM supply_chain_impact_finding_supports
WHERE cve_id = $1 AND package_id = $2 AND observed_version = $3 LIMIT 1`, cve, pkg, observed).Scan(&key); err != nil {
		t.Fatalf("resolve canonical key for %s %s observed %q: %v", cve, pkg, observed, err)
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

// top1MixedCaseEcosystem returns a keyed-style finding whose payload spells
// the ecosystem in the caller's case. The support fill lowercases the filter
// column, but the winner must carry the fact's own spelling (F1).
func top1MixedCaseEcosystem(cve, pkg string, priority int, ecosystem string) supplychaincore.SupplyChainImpactFinding {
	f := keyedFinding(cve, pkg, priority)
	f.Ecosystem = ecosystem
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
