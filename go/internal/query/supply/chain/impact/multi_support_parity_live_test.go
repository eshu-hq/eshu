// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B0 disposable PostgreSQL 18 characterization of the multi-support divergence between the legacy fact read and the winners read (one canonical finding with supports in two scopes, plus a derived-class row): scope-only grants and rank filters on non-key fields return different findings and counts, repository grants and the union grant agree, and the shipped gate-on stores serve granted calls from the supports table and return the legacy answers; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openMultiSupportSeededDatabase opens a disposable database and seeds the
// multi-support corpus without running the supports backfill or winners build.
// Callers that seed extra findings before backfill use this helper directly so
// the backfill cursor is not already marked complete.
func openMultiSupportSeededDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(
		t,
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DSN"),
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE"),
		5*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
		},
	)
	seedMultiSupportCorpus(t, ctx, database)
	return ctx, database
}

// openMultiSupportDatabase seeds the multi-support corpus on a disposable
// database, fills the supports table, and rebuilds the winners from it.
func openMultiSupportDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, database := openMultiSupportSeededDatabase(t)
	// The winners build reads the supports table, so fill it with the real
	// backfill before the rebuild.
	runSupportsBackfillToCompletion(t, ctx, database)
	if err := storagepostgres.NewSupplyChainImpactWinnersStore(storagepostgres.SQLDB{DB: database}).
		RebuildAllWinners(ctx); err != nil {
		t.Fatalf("RebuildAllWinners: %v", err)
	}
	return ctx, database
}

// TestMultiSupportWinnersRecordsEverySupport pins the premise: the six
// multi-support keys carry source_count 2, the winners sit where the corpus
// comment says, and the derived-class fact never wins m2.
func TestMultiSupportWinnersRecordsEverySupport(t *testing.T) {
	ctx, database := openMultiSupportDatabase(t)
	rows, err := database.QueryContext(ctx, `
SELECT winner_fact_id, winner_scope_id, source_count
FROM supply_chain_impact_canonical_winners
ORDER BY winner_fact_id`)
	if err != nil {
		t.Fatalf("read winners: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]string{}
	for rows.Next() {
		var fact, scope string
		var count int
		if err := rows.Scan(&fact, &scope, &count); err != nil {
			t.Fatal(err)
		}
		got[fact] = scope + "|" + string(rune('0'+count))
	}
	want := map[string]string{
		"fact:m1:b": multiSupportScopeB + "|2",
		"fact:m2:a": multiSupportScopeA + "|2",
		"fact:m3:a": multiSupportScopeA + "|2",
		"fact:m4:b": multiSupportScopeB + "|2",
		"fact:s1:a": multiSupportScopeA + "|1",
		"fact:s2:b": multiSupportScopeB + "|1",
		"fact:p1:a": multiSupportScopeA + "|2",
		"fact:p2:b": multiSupportScopeB + "|2",
	}
	if len(got) != len(want) {
		t.Fatalf("winners = %v, want %v (a derived-class row must not be a winner)", got, want)
	}
	for fact, wantValue := range want {
		if got[fact] != wantValue {
			t.Errorf("winner %s = %q, want %q", fact, got[fact], wantValue)
		}
	}
}

// TestMultiSupportScopedGrantDivergenceLive pins today's answers for every
// grant shape. Legacy applies the grant to the supports, then picks; the
// winners query applies it to the already-picked row. Legacy is the contract
// for granted calls (the arbiter ruling for #10880 PR-B: a granted call is
// never served from winners as-is). Since #10880 PR-B2a the gate-on stores serve
// a granted call from the supports table, which applies the grant to the support
// row like legacy, so the gate-on list and count equal legacy here while the
// winners read keeps the divergence this test pins. The derived row (m2 at p=99)
// appears in no answer.
func TestMultiSupportScopedGrantDivergenceLive(t *testing.T) {
	ctx, database := openMultiSupportDatabase(t)
	runSupportsBackfillToCompletion(t, ctx, database)
	at := time.Now().UTC()
	legacyList := PostgresFindingStore{DB: database, ReadFromWinners: false}
	gatedList := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacyAggregate := NewPostgresAggregateStore(database)
	legacyAggregate.Now = func() time.Time { return at }
	gatedAggregate := NewPostgresAggregateStoreWithReadModel(database, true)
	gatedAggregate.Now = func() time.Time { return at }

	cases := []struct {
		name                      string
		repositories, scopes      []string
		legacy, winners           string
		legacyTotal, winnersTotal int
		dimensionsEqual           bool
	}{
		{
			name: "scope SA only", scopes: []string{multiSupportScopeA},
			legacy:      "m1(p=40) m2(p=90) m3(p=60) m4(p=60) p1(p=50) p2(p=30) s1(p=70)",
			winners:     "m2(p=90) m3(p=60) p1(p=50) s1(p=70)",
			legacyTotal: 7, winnersTotal: 4, dimensionsEqual: false,
		},
		{
			name: "scope SB only", scopes: []string{multiSupportScopeB},
			legacy:      "m1(p=80) m2(p=30) m3(p=60) m4(p=60) p1(p=40) p2(p=45) s2(p=65)",
			winners:     "m1(p=80) m4(p=60) p2(p=45) s2(p=65)",
			legacyTotal: 7, winnersTotal: 4, dimensionsEqual: false,
		},
		{
			name: "scope SA and SB", scopes: []string{multiSupportScopeA, multiSupportScopeB},
			legacy:      "m1(p=80) m2(p=90) m3(p=60) m4(p=60) p1(p=50) p2(p=45) s1(p=70) s2(p=65)",
			winners:     "m1(p=80) m2(p=90) m3(p=60) m4(p=60) p1(p=50) p2(p=45) s1(p=70) s2(p=65)",
			legacyTotal: 8, winnersTotal: 8, dimensionsEqual: true,
		},
		{
			name: "repositories m1 and m2", repositories: []string{"repository:m1", "repository:m2"},
			legacy: "m1(p=80) m2(p=90)", winners: "m1(p=80) m2(p=90)",
			legacyTotal: 2, winnersTotal: 2, dimensionsEqual: true,
		},
		{
			name: "scope SA plus repository s2", repositories: []string{"repository:s2"}, scopes: []string{multiSupportScopeA},
			legacy:      "m1(p=40) m2(p=90) m3(p=60) m4(p=60) p1(p=50) p2(p=30) s1(p=70) s2(p=65)",
			winners:     "m2(p=90) m3(p=60) p1(p=50) s1(p=70) s2(p=65)",
			legacyTotal: 8, winnersTotal: 5, dimensionsEqual: false,
		},
	}
	dimensions := []InventoryDimension{
		InventoryByStatus, InventoryByPriorityBucket, InventoryBySeverity, InventoryByRepository, InventoryByEcosystem,
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			listFilter := FindingFilter{
				Limit: 50, Status: "affected_exact",
				AllowedRepositoryIDs: tc.repositories, AllowedScopeIDs: tc.scopes,
			}
			legacy, err := legacyList.ListSupplyChainImpactFindings(ctx, listFilter)
			if err != nil {
				t.Fatalf("legacy list: %v", err)
			}
			if got := multiSupportDescribe(legacy); got != tc.legacy {
				t.Errorf("legacy list = %q, want %q", got, tc.legacy)
			}
			// The shipped gate-on store serves a granted call from the supports table,
			// which must return the legacy page.
			gated, err := gatedList.ListSupplyChainImpactFindings(ctx, listFilter)
			if err != nil {
				t.Fatalf("gate-on list: %v", err)
			}
			legacyJSON, _ := json.Marshal(legacy)
			gatedJSON, _ := json.Marshal(gated)
			if string(legacyJSON) != string(gatedJSON) {
				t.Errorf("gate-on store served a granted call differently from legacy\nlegacy: %s\ngated:  %s", legacyJSON, gatedJSON)
			}
			winners, err := multiSupportWinnersList(ctx, database, listFilter)
			if err != nil {
				t.Fatalf("winners list: %v", err)
			}
			if got := multiSupportDescribe(winners); got != tc.winners {
				t.Errorf("winners list = %q, want %q", got, tc.winners)
			}

			aggregateFilter := AggregateFilter{
				Status: "affected_exact", AllowedRepositoryIDs: tc.repositories, AllowedScopeIDs: tc.scopes,
			}
			legacyCount, err := legacyAggregate.CountSupplyChainImpactFindings(ctx, aggregateFilter)
			if err != nil {
				t.Fatalf("legacy count: %v", err)
			}
			gatedCount, err := gatedAggregate.CountSupplyChainImpactFindings(ctx, aggregateFilter)
			if err != nil {
				t.Fatalf("gate-on count: %v", err)
			}
			winnersCount, err := multiSupportWinnersCount(ctx, database, aggregateFilter, at)
			if err != nil {
				t.Fatalf("winners count: %v", err)
			}
			if legacyCount.TotalFindings != tc.legacyTotal || winnersCount.TotalFindings != tc.winnersTotal {
				t.Errorf("count legacy/winners = %d/%d, want %d/%d",
					legacyCount.TotalFindings, winnersCount.TotalFindings, tc.legacyTotal, tc.winnersTotal)
			}
			legacyCountJSON, _ := json.Marshal(legacyCount)
			gatedCountJSON, _ := json.Marshal(gatedCount)
			if string(legacyCountJSON) != string(gatedCountJSON) {
				t.Errorf("gate-on store served a granted count differently from legacy\nlegacy: %s\ngated:  %s",
					legacyCountJSON, gatedCountJSON)
			}

			for _, dimension := range dimensions {
				legacyPage, err := legacyAggregate.SupplyChainImpactInventory(ctx, aggregateFilter, dimension, 50, 0)
				if err != nil {
					t.Fatalf("legacy inventory %s: %v", dimension, err)
				}
				legacyRows := legacyPage.Rows
				winnersRows, err := multiSupportWinnersInventory(ctx, database, aggregateFilter, dimension, at)
				if err != nil {
					t.Fatalf("winners inventory %s: %v", dimension, err)
				}
				legacyRowsJSON, _ := json.Marshal(legacyRows)
				winnersRowsJSON, _ := json.Marshal(winnersRows)
				if equal := string(legacyRowsJSON) == string(winnersRowsJSON); equal != tc.dimensionsEqual {
					t.Errorf("inventory %s equal = %v, want %v\nlegacy:  %s\nwinners: %s",
						dimension, equal, tc.dimensionsEqual, legacyRowsJSON, winnersRowsJSON)
				}
			}
		})
	}
}

// TestMultiSupportUnscopedRankFilterDivergenceLive pins today's answers for
// unscoped filters. Rank filters (priority bucket, severity) test every
// support on the legacy read and only the picked row on the winners read;
// filters that are monotone in the pick order (min priority) cannot differ.
// The arbiter ruling makes the winners (canonical, pick-then-filter) answer
// the contract for rank filters, so PR-B3 changes the legacy expectations
// here on purpose; until then the shipped gate-on stores serve winners.
func TestMultiSupportUnscopedRankFilterDivergenceLive(t *testing.T) {
	ctx, database := openMultiSupportDatabase(t)
	at := time.Now().UTC()
	legacyList := PostgresFindingStore{DB: database, ReadFromWinners: false}
	winnersList := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacyAggregate := NewPostgresAggregateStore(database)
	legacyAggregate.Now = func() time.Time { return at }
	winnersAggregate := NewPostgresAggregateStoreWithReadModel(database, true)
	winnersAggregate.Now = func() time.Time { return at }

	cases := []struct {
		name                      string
		filter                    FindingFilter
		legacy, winners           string
		legacyTotal, winnersTotal int
	}{
		{
			name: "status only", filter: FindingFilter{Limit: 50, Status: "affected_exact"},
			legacy:      "m1(p=80) m2(p=90) m3(p=60) m4(p=60) p1(p=50) p2(p=45) s1(p=70) s2(p=65)",
			winners:     "m1(p=80) m2(p=90) m3(p=60) m4(p=60) p1(p=50) p2(p=45) s1(p=70) s2(p=65)",
			legacyTotal: 8, winnersTotal: 8,
		},
		{
			name: "priority bucket critical", filter: FindingFilter{Limit: 50, PriorityBucket: "critical"},
			legacy: "m1(p=80) m2(p=90) s1(p=70)", winners: "m1(p=80) m2(p=90) s1(p=70)",
			legacyTotal: 3, winnersTotal: 3,
		},
		{
			name: "severity critical", filter: FindingFilter{Limit: 50, Severity: "critical"},
			legacy: "m1(p=80) m2(p=90)", winners: "m1(p=80) m2(p=90)",
			legacyTotal: 2, winnersTotal: 2,
		},
		{
			name: "min priority 65", filter: FindingFilter{Limit: 50, MinPriorityScore: 65},
			legacy:      "m1(p=80) m2(p=90) s1(p=70) s2(p=65)",
			winners:     "m1(p=80) m2(p=90) s1(p=70) s2(p=65)",
			legacyTotal: 4, winnersTotal: 4,
		},
		{
			name: "priority bucket low", filter: FindingFilter{Limit: 50, PriorityBucket: "low"},
			legacy:      "m1(p=40) m2(p=30) m3(p=60) m4(p=60) p1(p=50) p2(p=45) s2(p=65)",
			winners:     "m3(p=60) m4(p=60) p1(p=50) p2(p=45) s2(p=65)",
			legacyTotal: 7, winnersTotal: 5,
		},
		{
			name: "severity medium", filter: FindingFilter{Limit: 50, Severity: "medium"},
			legacy:      "m1(p=40) m2(p=30) m3(p=60) m4(p=60) p1(p=50) p2(p=45) s2(p=65)",
			winners:     "m3(p=60) m4(p=60) p1(p=50) p2(p=45) s2(p=65)",
			legacyTotal: 7, winnersTotal: 5,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legacy, err := legacyList.ListSupplyChainImpactFindings(ctx, tc.filter)
			if err != nil {
				t.Fatalf("legacy list: %v", err)
			}
			winners, err := winnersList.ListSupplyChainImpactFindings(ctx, tc.filter)
			if err != nil {
				t.Fatalf("winners list: %v", err)
			}
			if got := multiSupportDescribe(legacy); got != tc.legacy {
				t.Errorf("legacy list = %q, want %q", got, tc.legacy)
			}
			if got := multiSupportDescribe(winners); got != tc.winners {
				t.Errorf("winners list = %q, want %q", got, tc.winners)
			}
			aggregateFilter := AggregateFilter{
				Status: tc.filter.Status, PriorityBucket: tc.filter.PriorityBucket,
				Severity: tc.filter.Severity, MinPriorityScore: tc.filter.MinPriorityScore,
			}
			legacyCount, err := legacyAggregate.CountSupplyChainImpactFindings(ctx, aggregateFilter)
			if err != nil {
				t.Fatalf("legacy count: %v", err)
			}
			winnersCount, err := winnersAggregate.CountSupplyChainImpactFindings(ctx, aggregateFilter)
			if err != nil {
				t.Fatalf("winners count: %v", err)
			}
			if legacyCount.TotalFindings != tc.legacyTotal || winnersCount.TotalFindings != tc.winnersTotal {
				t.Errorf("count legacy/winners = %d/%d, want %d/%d",
					legacyCount.TotalFindings, winnersCount.TotalFindings, tc.legacyTotal, tc.winnersTotal)
			}
		})
	}
}

// TestMultiSupportProfileDivergenceLive pins the profile filter, the other
// existence filter beside image_ref. Supports of one canonical key can differ in
// precise eligibility, and the arbiter ruling serves profile as any-support from
// the supports table (B2). Today the legacy read tests every support; the winners
// query tests only the picked row, so a profile=precise call loses p2, whose
// winner is the comprehensive support while its other support is precise. This
// test characterizes the bare winners read; the gate-on stores serve an unscoped
// profile=precise call through route V, which TestUnscopedExistenceFilterReadsEqualLegacyLive
// holds to the legacy answer.
func TestMultiSupportProfileDivergenceLive(t *testing.T) {
	ctx, database := openMultiSupportDatabase(t)
	legacyList := PostgresFindingStore{DB: database, ReadFromWinners: false}
	for name, tc := range map[string]struct{ profile, legacy, winners string }{
		"precise": {
			profile: ProfilePrecise,
			legacy:  "m1(p=80) m2(p=90) m3(p=60) m4(p=60) p1(p=50) p2(p=30) s1(p=70) s2(p=65)",
			winners: "m1(p=80) m2(p=90) m3(p=60) m4(p=60) p1(p=50) s1(p=70) s2(p=65)",
		},
		"comprehensive": {
			profile: "",
			legacy:  "m1(p=80) m2(p=90) m3(p=60) m4(p=60) p1(p=50) p2(p=45) s1(p=70) s2(p=65)",
			winners: "m1(p=80) m2(p=90) m3(p=60) m4(p=60) p1(p=50) p2(p=45) s1(p=70) s2(p=65)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			filter := FindingFilter{Limit: 50, Status: "affected_exact", DetectionProfile: tc.profile}
			legacy, err := legacyList.ListSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("legacy list: %v", err)
			}
			winners, err := multiSupportWinnersList(ctx, database, filter)
			if err != nil {
				t.Fatalf("winners list: %v", err)
			}
			if got := multiSupportDescribe(legacy); got != tc.legacy {
				t.Errorf("legacy list = %q, want %q", got, tc.legacy)
			}
			if got := multiSupportDescribe(winners); got != tc.winners {
				t.Errorf("winners list = %q, want %q", got, tc.winners)
			}
		})
	}
}
