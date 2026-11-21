// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestAggregatesWinnersReadGate pins the #10880 slice-2b query routing:
// the gated store serves ungranted calls from the winners read model and
// keeps scoped-token calls and the default store on the legacy CTE,
// mirroring PostgresFindingStore.
func TestAggregatesWinnersReadGate(t *testing.T) {
	t.Parallel()

	countRow := func() db.Row {
		return &cov7StoreRow{values: []any{
			cov7NullInt(3), cov7NullInt(2), cov7NullInt(1),
			cov7NullInt(1), cov7NullInt(0), cov7NullInt(1),
			[]byte(`[{"bucket":"p1","bucket_count":2}]`), []byte(`[{"bucket":"critical","bucket_count":3}]`),
		}}
	}
	emptyRows := func(string) (db.Rows, error) { return &cov7StoreRows{}, nil }

	// routed runs one count call and returns every issued statement so each
	// subtest can assert the read model of the whole call. The count is one
	// combined statement (#10970), winners or legacy.
	routed := func(store PostgresAggregateStore, filter AggregateFilter) []string {
		t.Helper()
		probe := &cov7StoreQueryer{onRow: func(string) db.Row { return countRow() }, onRows: emptyRows}
		store.reader = probe
		if _, err := store.CountSupplyChainImpactFindings(t.Context(), filter); err != nil {
			t.Fatalf("count = %v, want envelope", err)
		}
		if len(probe.queries) != 1 {
			t.Fatalf("count issued %d queries, want exactly one combined statement", len(probe.queries))
		}
		return probe.queries
	}

	t.Run("gated ungranted reads winners", func(t *testing.T) {
		t.Parallel()
		store := NewPostgresAggregateStoreWithReadModel(nil, true)
		if !store.ReadFromWinners {
			t.Fatal("gated constructor left ReadFromWinners false")
		}
		queries := routed(store, AggregateFilter{Status: "affected_exact"})
		for _, query := range queries {
			if !strings.Contains(query, "aggregate_winners AS") ||
				!strings.Contains(query, "supply_chain_impact_canonical_winners AS w") {
				t.Fatalf("gated count query misses the winners read:\n%s", query)
			}
			if strings.Contains(query, "canonical_facts") {
				t.Fatalf("gated count query still reads the legacy CTE:\n%s", query)
			}
		}
	})

	t.Run("gated scoped call stays on legacy", func(t *testing.T) {
		t.Parallel()
		store := NewPostgresAggregateStoreWithReadModel(nil, true)
		queries := routed(store, AggregateFilter{
			Status:               "affected_exact",
			AllowedRepositoryIDs: []string{"repository:r1"},
		})
		for _, query := range queries {
			if !strings.Contains(query, "canonical_facts") {
				t.Fatalf("gated scoped count query left the legacy CTE:\n%s", query)
			}
			if strings.Contains(query, "aggregate_winners AS") {
				t.Fatalf("gated scoped count query reaches the winners read:\n%s", query)
			}
		}
	})

	t.Run("default store stays on legacy", func(t *testing.T) {
		t.Parallel()
		queries := routed(PostgresAggregateStore{}, AggregateFilter{Status: "affected_exact"})
		for _, query := range queries {
			if !strings.Contains(query, "canonical_facts") {
				t.Fatalf("default count query left the legacy CTE:\n%s", query)
			}
		}
	})

	t.Run("gated ungranted inventory reads winners", func(t *testing.T) {
		t.Parallel()
		probe := &cov7StoreQueryer{onRows: emptyRows}
		store := NewPostgresAggregateStoreWithReadStore(probe, true)
		if _, err := store.SupplyChainImpactInventory(
			t.Context(), AggregateFilter{Status: "affected_exact"}, InventoryByStatus, 10, 0,
		); err != nil {
			t.Fatalf("inventory = %v, want page", err)
		}
		if len(probe.queries) != 1 {
			t.Fatalf("inventory issued %d queries, want one", len(probe.queries))
		}
		if !strings.Contains(probe.queries[0], "aggregate_winners AS") {
			t.Fatalf("gated inventory query misses the winners read:\n%s", probe.queries[0])
		}
	})

	t.Run("legacy inventory keeps group expression", func(t *testing.T) {
		t.Parallel()
		rendered := AggregateWinnersInventoryQuery("GROUP_EXPR")
		if strings.Count(rendered, "GROUP_EXPR") != 1 {
			t.Fatalf("winners inventory query did not substitute the group expression once")
		}
	})
}
