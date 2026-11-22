// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// A profile filter is an existence filter too (the precise profile is the API
// default): supports of one canonical key can differ in precise eligibility
// (TestDetectionProfileIsAPerSupportOutputOfOneCanonicalKey in the reducer's
// supply-chain core package), so the picked row cannot answer it. The guards
// below cover both existence filters.
//
// An image_ref filter is an existence filter: copies of one canonical key can
// carry different image refs, and the arbiter ruling for #10880 PR-B answers
// it from any support. The winners row holds only the picked support's image
// ref, so no gate-on route may answer an image_ref call from winners. These
// tests pin the guard at each gate site: list and aggregates fall back to the
// legacy fact read, grouped inventory (which has no legacy read) answers
// ErrGroupedInventoryReadModelUnavailable.

func TestFindingListImageRefStaysOffWinners(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, filter FindingFilter) string {
		t.Helper()
		probe := &cov7StoreQueryer{onRows: func(string) (db.Rows, error) { return &cov7StoreRows{}, nil }}
		store := NewPostgresFindingStoreWithReadStore(probe, true)
		if _, err := store.ListSupplyChainImpactFindings(t.Context(), filter); err != nil {
			t.Fatalf("list = %v, want page", err)
		}
		if len(probe.queries) != 1 {
			t.Fatalf("list issued %d queries, want one", len(probe.queries))
		}
		return probe.queries[0]
	}
	if query := run(t, FindingFilter{Limit: 10, ImageRef: "registry.example/app:1"}); strings.Contains(query, "supply_chain_impact_canonical_winners AS w") {
		t.Fatalf("gate-on list with image_ref reads the winners table:\n%s", query)
	}
	if query := run(t, FindingFilter{Limit: 10, Status: "affected_exact", DetectionProfile: ProfilePrecise}); strings.Contains(query, "supply_chain_impact_canonical_winners AS w") {
		t.Fatalf("gate-on list with the precise profile reads the winners table:\n%s", query)
	}
	if query := run(t, FindingFilter{Limit: 10, Status: "affected_exact"}); !strings.Contains(query, "supply_chain_impact_canonical_winners AS w") {
		t.Fatalf("gate-on list without image_ref left the winners read:\n%s", query)
	}
}

func TestAggregateImageRefStaysOffWinners(t *testing.T) {
	t.Parallel()
	countRow := func(string) db.Row {
		return &cov7StoreRow{values: []any{
			cov7NullInt(0), cov7NullInt(0), cov7NullInt(0), cov7NullInt(0), cov7NullInt(0), cov7NullInt(0),
			[]byte(`[]`), []byte(`[]`),
		}}
	}
	emptyRows := func(string) (db.Rows, error) { return &cov7StoreRows{}, nil }
	for name, filter := range map[string]AggregateFilter{
		"image_ref": {ImageRef: "registry.example/app:1"},
		"profile":   {Status: "affected_exact", DetectionProfile: ProfilePrecise},
		"plain":     {Status: "affected_exact"},
	} {
		wantWinners := name == "plain"
		t.Run("count/"+name, func(t *testing.T) {
			t.Parallel()
			probe := &cov7StoreQueryer{onRow: countRow, onRows: emptyRows}
			store := NewPostgresAggregateStoreWithReadStore(probe, true)
			if _, err := store.CountSupplyChainImpactFindings(t.Context(), filter); err != nil {
				t.Fatalf("count = %v", err)
			}
			if got := strings.Contains(probe.queries[0], "aggregate_winners AS"); got != wantWinners {
				t.Fatalf("count reads winners = %v, want %v", got, wantWinners)
			}
		})
		t.Run("inventory/"+name, func(t *testing.T) {
			t.Parallel()
			probe := &cov7StoreQueryer{onRow: countRow, onRows: emptyRows}
			store := NewPostgresAggregateStoreWithReadStore(probe, true)
			if _, err := store.SupplyChainImpactInventory(t.Context(), filter, InventoryByStatus, 10, 0); err != nil {
				t.Fatalf("inventory = %v", err)
			}
			if got := strings.Contains(probe.queries[0], "aggregate_winners AS"); got != wantWinners {
				t.Fatalf("inventory reads winners = %v, want %v", got, wantWinners)
			}
		})
	}
}

func TestGroupedInventoryImageRefAnswersUnavailable(t *testing.T) {
	t.Parallel()
	probe := &cov7StoreQueryer{onRows: func(string) (db.Rows, error) { return &cov7StoreRows{}, nil }}
	store := NewPostgresGroupedInventoryStoreWithReadStore(probe, true, nil)
	_, err := store.SupplyChainImpactGroupedInventory(
		t.Context(), AggregateFilter{ImageRef: "registry.example/app:1"}, GroupedInventoryByService, 10, 0,
	)
	if !errors.Is(err, ErrGroupedInventoryReadModelUnavailable) {
		t.Fatalf("grouped inventory with image_ref = %v, want ErrGroupedInventoryReadModelUnavailable", err)
	}
	if len(probe.queries) != 0 {
		t.Fatalf("grouped inventory with image_ref issued %d queries, want none (winners cannot answer it)", len(probe.queries))
	}
}
