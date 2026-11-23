// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2a disposable PostgreSQL 18 proof of the unscoped explain on the multi-support corpus (arbiter ruling V): an ungranted selector lookup reads the winners table's winning supports, an ungranted image selector reads the image partial index, and an identity fallback (canonical key, winner or non-winner fact id) resolves through the winners table; every outcome equals the legacy explain, the case list of differences is recorded, a list-then-explain round trip on one branch A row and one supplement row returns equal findings, and a stale winners table refuses; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// TestExplainUnscopedEqualsLegacyLive holds every ungranted lookup the unscoped
// explain serves from the winners table, the image index and the identity
// fallback to the legacy explain: selector pairs of every seeded finding, image
// selectors, and the identity forms (canonical key, finding id, and the fact id of
// both the winner and a lower copy). Each outcome (payload, not found, or
// ambiguous) must match. The count of compared lookups and of non-trivial
// outcomes guards the matrix against going vacuous.
func TestExplainUnscopedEqualsLegacyLive(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	gated := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}

	all, err := legacy.ListSupplyChainImpactFindings(ctx, FindingFilter{Status: "affected_exact", Limit: 200})
	if err != nil || len(all) == 0 {
		t.Fatalf("seed list = %d rows, %v", len(all), err)
	}
	var requests []packet.ExplanationFilter
	for _, row := range all {
		requests = append(requests,
			packet.ExplanationFilter{CVEID: row.CVEID, PackageID: row.PackageID},
			packet.ExplanationFilter{CVEID: row.CVEID, RepositoryID: row.RepositoryID},
			packet.ExplanationFilter{AdvisoryID: row.CVEID, RepositoryID: row.RepositoryID},
			packet.ExplanationFilter{CVEID: row.CVEID, PackageID: row.PackageID, ImageRef: "registry.example/a:1"},
			packet.ExplanationFilter{CVEID: row.CVEID, PackageID: row.PackageID, ImageRef: "registry.example/b:2"},
			packet.ExplanationFilter{FindingID: row.FindingID},
		)
	}
	// Selectors that name no finding, and a CVE and package pair shared by two
	// keys (amb1, amb2), which is ambiguous on both reads.
	requests = append(requests,
		packet.ExplanationFilter{CVEID: "CVE-2026-m1", RepositoryID: "repository:nowhere"},
		packet.ExplanationFilter{CVEID: "CVE-2026-none", PackageID: "pkg:npm/none"},
		// The collapsed key: a selector that only a lower copy carries.
		packet.ExplanationFilter{CVEID: "ADV-C1", PackageID: "pkg:npm/example-c@1.0.0"},
		packet.ExplanationFilter{AdvisoryID: "ADV-C1", PackageID: "pkg:npm/example-c@1.0.0"},
		packet.ExplanationFilter{CVEID: "CVE-2026-amb", PackageID: "pkg:npm/example-amb@1.0.0"},
		packet.ExplanationFilter{CVEID: "CVE-2026-amb", PackageID: "pkg:npm/example-amb@1.0.0", ImageRef: "registry.example/a:1"},
	)
	// Identity forms resolved through the fact id and canonical key.
	keyRows, err := database.QueryContext(ctx, `
SELECT s.fact_id, s.canonical_key, s.finding_id, w.winner_fact_id = s.fact_id
FROM supply_chain_impact_finding_supports AS s
JOIN supply_chain_impact_canonical_winners AS w ON w.canonical_key = s.canonical_key
WHERE s.scope_id <> 'operator:vulnerability_suppressions'`)
	if err != nil {
		t.Fatalf("read identities: %v", err)
	}
	defer func() { _ = keyRows.Close() }()
	for keyRows.Next() {
		var factID, key, findingID string
		var winner bool
		if err := keyRows.Scan(&factID, &key, &findingID, &winner); err != nil {
			t.Fatalf("scan identity: %v", err)
		}
		requests = append(requests,
			packet.ExplanationFilter{FindingID: factID},
			packet.ExplanationFilter{FindingID: key},
			packet.ExplanationFilter{FindingID: findingID, CVEID: "CVE-2026-m1"},
		)
	}
	if err := keyRows.Err(); err != nil {
		t.Fatalf("identities: %v", err)
	}

	compared, payloads, ambiguous, notFound := 0, 0, 0, 0
	differences := map[string]string{}
	for _, request := range requests {
		got, gotErr := gated.ExplainSupplyChainImpact(ctx, request)
		want, wantErr := legacy.ExplainSupplyChainImpact(ctx, request)
		gotOutcome, wantOutcome := explainOutcome(got, gotErr), explainOutcome(want, wantErr)
		compared++
		switch {
		case errors.Is(wantErr, ErrExplanationAmbiguous):
			ambiguous++
		case errors.Is(wantErr, ErrExplanationNotFound):
			notFound++
		case wantErr == nil:
			payloads++
		}
		if gotOutcome != wantOutcome {
			encoded, _ := json.Marshal(request)
			differences[string(encoded)] = "legacy: " + wantOutcome + " | gate-on: " + gotOutcome
		}
	}
	// The one known difference (listed for B3, see TestCollapsedKeyCVEFilterLive):
	// the key collapses the CVE and advisory ids, so a CVE selector that only a
	// lower copy carries misses on the winners row.
	const collapsedCVESelector = `{"cve_id":"ADV-C1","package_id":"pkg:npm/example-c@1.0.0"}`
	for request, diff := range differences {
		if request == collapsedCVESelector {
			t.Logf("B3 difference: explain %s: %.300s", request, diff)
			continue
		}
		t.Errorf("explain %s differs\n%.600s", request, diff)
	}
	if _, ok := differences[collapsedCVESelector]; !ok {
		t.Errorf("the collapsed-key CVE selector no longer differs from legacy: the fix landed, so remove the known difference")
	}
	t.Logf("compared %d lookups: %d payloads, %d ambiguous, %d not found", compared, payloads, ambiguous, notFound)
	if payloads == 0 || ambiguous == 0 || notFound == 0 {
		t.Fatalf("the matrix lost its signal: payloads=%d ambiguous=%d notFound=%d", payloads, ambiguous, notFound)
	}
}

// TestExplainRoundTripFromAnUnscopedPreciseListLive is ruling V's explain round
// trip (required proof 10): list unscoped precise, then explain by the listed
// finding id, for one branch A row (its winner passes precise) and one branch B
// row (p2, served by the supplement). The explained finding equals the listed
// row field for field.
func TestExplainRoundTripFromAnUnscopedPreciseListLive(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	gated := PostgresFindingStore{DB: database, ReadFromWinners: true}
	rows, err := gated.ListSupplyChainImpactFindings(ctx, FindingFilter{Limit: 200, Status: "affected_exact", DetectionProfile: ProfilePrecise})
	if err != nil {
		t.Fatalf("precise list: %v", err)
	}
	roundTripped := map[string]bool{}
	for _, row := range rows {
		id := row.FindingID
		if id != "finding:m1" && id != "finding:p2" {
			continue
		}
		explained, err := gated.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{FindingID: id})
		if err != nil {
			t.Fatalf("explain %s: %v", id, err)
		}
		listed, _ := json.Marshal(row)
		got, _ := json.Marshal(explained.Finding)
		if id == "finding:p2" {
			// The supplement lists the best precise copy of p2 (priority 30); the
			// explain names the canonical pick (priority 45). Same finding, and the
			// listed copy is the one the precise filter admits.
			if explained.Finding.FindingID != row.FindingID || explained.Finding.CVEID != row.CVEID ||
				explained.Finding.PackageID != row.PackageID || explained.Finding.Status != row.Status {
				t.Fatalf("p2: the explain names a different finding\nlist:    %s\nexplain: %s", listed, got)
			}
			if row.PriorityScore != 30 || explained.Finding.PriorityScore != 45 {
				t.Fatalf("p2 priorities list/explain = %d/%d, want 30/45", row.PriorityScore, explained.Finding.PriorityScore)
			}
		} else if string(listed) != string(got) {
			t.Fatalf("m1: the explain payload differs from the listed branch A row\nlist:    %s\nexplain: %s", listed, got)
		}
		roundTripped[id] = true
	}
	if len(roundTripped) != 2 {
		t.Fatalf("round-tripped %v, want one branch A row (m1) and one supplement row (p2)", roundTripped)
	}
}

// TestExplainSelectorRefusesWhenWinnersAreStaleLive pins the fail-closed rule on
// a real table: aging the winners watermark past the window turns an ungranted
// selector explain into the typed refusal, while a public-id lookup (supports
// only) and an image selector keep answering.
func TestExplainSelectorRefusesWhenWinnersAreStaleLive(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	gated := PostgresFindingStore{DB: database, ReadFromWinners: true}
	if _, err := database.ExecContext(ctx, `UPDATE supply_chain_impact_winners_materialization SET materialized_at = NOW() - INTERVAL '1 hour'`); err != nil {
		t.Fatalf("age the winners: %v", err)
	}
	_, err := gated.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{CVEID: "CVE-2026-m1", PackageID: "pkg:npm/example-m1@1.0.0"})
	if !errors.Is(err, ErrExplanationWinnersUnavailable) {
		t.Fatalf("selector explain with stale winners = %v, want ErrExplanationWinnersUnavailable", err)
	}
	if _, err := gated.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{FindingID: "finding:m1"}); err != nil {
		t.Fatalf("public-id explain must not need the winners table: %v", err)
	}
	if _, err := gated.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{CVEID: "CVE-2026-i1", ImageRef: "registry.example/a:1"}); err != nil {
		t.Fatalf("image selector explain must not need the winners table: %v", err)
	}
}

// TestCollapsedKeyCVEFilterLive characterizes the one divergence the winners-row
// selector shares with the shipped winners list: the canonical key collapses the
// CVE and advisory ids, so a finding's copies can disagree on cve_id (c1 carries
// the CVE, c3 only the advisory and outranks it). A cve_id filter tests the pick on
// the winners read and every copy on the legacy read, so the plain winners list and
// the precise route miss c while legacy returns it. An advisory_id filter agrees
// (the pick carries the advisory). Listed for B3 with the rank-filter differences;
// the day a winners-row cve_id is made key-determined this test changes on purpose.
func TestCollapsedKeyCVEFilterLive(t *testing.T) {
	ctx, database := openRouteVDatabase(t)
	gated := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}
	for name, tc := range map[string]struct {
		filter     FindingFilter
		wantGated  bool
		wantLegacy bool
	}{
		"cve_id only a lower copy carries": {FindingFilter{Limit: 50, CVEID: "ADV-C1"}, false, true},
		"precise and that cve_id":          {FindingFilter{Limit: 50, CVEID: "ADV-C1", DetectionProfile: ProfilePrecise}, false, true},
		"advisory_id the pick carries":     {FindingFilter{Limit: 50, AdvisoryID: "ADV-C1"}, true, true},
		"precise and the advisory_id":      {FindingFilter{Limit: 50, AdvisoryID: "ADV-C1", DetectionProfile: ProfilePrecise}, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			rows, err := gated.ListSupplyChainImpactFindings(ctx, tc.filter)
			if err != nil {
				t.Fatalf("gate-on list: %v", err)
			}
			legacyRows, err := legacy.ListSupplyChainImpactFindings(ctx, tc.filter)
			if err != nil {
				t.Fatalf("legacy list: %v", err)
			}
			has := func(rows []FindingRow) bool {
				for _, row := range rows {
					if row.FindingID == "finding:c3" || row.FindingID == "finding:c1" {
						return true
					}
				}
				return false
			}
			if has(rows) != tc.wantGated || has(legacyRows) != tc.wantLegacy {
				t.Fatalf("collapsed key listed gate-on=%v legacy=%v, want %v/%v\ngate-on: %s\nlegacy:  %s",
					has(rows), has(legacyRows), tc.wantGated, tc.wantLegacy, multiSupportDescribe(rows), multiSupportDescribe(legacyRows))
			}
		})
	}
}
