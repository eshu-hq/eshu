// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Route parity (#10880 PR-B2a, arbiter ruling V, required proof 7): one route
// function decides the read model, the read of every store method takes the
// statement from it, and the freshness method takes its answer from it. These
// tests enumerate the filters and assert the statement read equals the route, the
// route equals what the freshness method reports, and that no gate-on call reaches
// the legacy fact read.

// routeQueryer answers the supports readiness probe as ready, the winners
// watermark as fresh, a count row, and every other statement with no rows.
func routeQueryer() *routeFake {
	return &routeFake{cov7StoreQueryer: &cov7StoreQueryer{
		onRow: func(query string) db.Row {
			if strings.Contains(query, "set_config('jit'") {
				return &cov7StoreRow{values: []any{"off"}}
			}
			return &cov7StoreRow{values: []any{
				cov7NullInt(0), cov7NullInt(0), cov7NullInt(0), cov7NullInt(0), cov7NullInt(0), cov7NullInt(0),
				[]byte(`[]`), []byte(`[]`),
			}}
		},
		onRows: func(query string) (db.Rows, error) {
			switch {
			case strings.Contains(query, "supply_chain_impact_finding_supports_backfill"):
				return &cov7StoreRows{rows: [][]any{{true}}}, nil
			case query == SelectWinnersWatermarkQuery:
				return &cov7StoreRows{rows: [][]any{{time.Now().UTC()}}}, nil
			}
			return &cov7StoreRows{}, nil
		},
	}}
}

// routeOf names the read model a statement reads.
func routeOf(statement string) ReadRoute {
	switch {
	case strings.Contains(statement, "image_supports AS MATERIALIZED"):
		return RouteSupportsImage
	case strings.Contains(statement, "branch_b AS ("):
		return RouteWinnersSupplement
	case strings.Contains(statement, "FROM (SELECT * FROM supply_chain_impact_finding_supports WHERE scope_id = ANY("):
		return RouteSupportsGrant
	case strings.Contains(statement, "supply_chain_impact_canonical_winners AS w"):
		return RouteWinners
	case strings.Contains(statement, "FROM fact_records AS fact"):
		return RouteLegacy
	}
	return ""
}

// existenceFilters is the filter matrix of the route tests: each is a shape the
// route function distinguishes.
var existenceFilters = []struct {
	name    string
	granted bool
	image   string
	profile string
	want    ReadRoute
}{
	{"no existence filter", false, "", "", RouteWinners},
	{"precise profile", false, "", ProfilePrecise, RouteWinnersSupplement},
	{"image ref", false, "registry.example/app:1", "", RouteSupportsImage},
	{"image ref and precise", false, "registry.example/app:1", ProfilePrecise, RouteSupportsImage},
	{"grant", true, "", "", RouteSupportsGrant},
	{"grant and precise", true, "", ProfilePrecise, RouteSupportsGrant},
	{"grant and image ref", true, "registry.example/app:1", "", RouteSupportsGrant},
	{"grant, image ref and precise", true, "registry.example/app:1", ProfilePrecise, RouteSupportsGrant},
}

func TestRouteForEnumeratesEveryExistenceFilter(t *testing.T) {
	t.Parallel()
	for _, tc := range existenceFilters {
		if got := routeFor(true, tc.granted, tc.image, tc.profile); got != tc.want {
			t.Errorf("%s: gate-on route = %q, want %q", tc.name, got, tc.want)
		}
		if got := routeFor(false, tc.granted, tc.image, tc.profile); got != RouteLegacy {
			t.Errorf("%s: gate-off route = %q, want legacy", tc.name, got)
		}
		if got := routeFor(true, tc.granted, tc.image, tc.profile); got == RouteLegacy {
			t.Errorf("%s: a gate-on call reaches the legacy read", tc.name)
		}
	}
}

func TestListReadFollowsTheRoute(t *testing.T) {
	t.Parallel()
	for _, tc := range existenceFilters {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filter := FindingFilter{Limit: 10, Status: "affected_exact", ImageRef: tc.image, DetectionProfile: tc.profile}
			if tc.granted {
				filter.AllowedScopeIDs = []string{"scope:a"}
			}
			probe := routeQueryer()
			store := NewPostgresFindingStoreWithReadStore(probe, true)
			if _, err := store.ListSupplyChainImpactFindings(t.Context(), filter); err != nil {
				t.Fatalf("list = %v", err)
			}
			read := probe.queries[len(probe.queries)-1]
			if got := routeOf(read); got != tc.want {
				t.Fatalf("list read the %q model, want %q", got, tc.want)
			}
			probed := strings.Contains(probe.queries[0], "supply_chain_impact_finding_supports_backfill")
			if probed != tc.want.readsSupports() {
				t.Fatalf("supports readiness probed = %v, want %v for route %q", probed, tc.want.readsSupports(), tc.want)
			}
			// The freshness method takes its answer from the same route.
			fresh, err := store.SupplyChainImpactFreshness(t.Context(), filter)
			if err != nil {
				t.Fatalf("freshness = %v", err)
			}
			if tc.want.servesWinners() != fresh.Present {
				t.Fatalf("freshness reports the winners watermark = %v, want %v for route %q", fresh.Present, tc.want.servesWinners(), tc.want)
			}
		})
	}
}

func TestAggregateReadsFollowTheRoute(t *testing.T) {
	t.Parallel()
	for _, tc := range existenceFilters {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filter := AggregateFilter{Status: "affected_exact", ImageRef: tc.image, DetectionProfile: tc.profile}
			if tc.granted {
				filter.AllowedRepositoryIDs = []string{"repository:r"}
			}
			for what, call := range map[string]func(PostgresAggregateStore) (AggregateFreshness, error){
				"count": func(s PostgresAggregateStore) (AggregateFreshness, error) {
					count, err := s.CountSupplyChainImpactFindings(t.Context(), filter)
					return count.Freshness, err
				},
				"inventory": func(s PostgresAggregateStore) (AggregateFreshness, error) {
					page, err := s.SupplyChainImpactInventory(t.Context(), filter, InventoryByStatus, 10, 0)
					return page.Freshness, err
				},
			} {
				probe := routeQueryer()
				freshness, err := call(NewPostgresAggregateStoreWithReadStore(probe, true))
				if err != nil {
					t.Fatalf("%s = %v", what, err)
				}
				read := probe.queries[len(probe.queries)-1]
				if got := routeOf(read); got != tc.want {
					t.Fatalf("%s read the %q model, want %q", what, got, tc.want)
				}
				if (freshness.LatestObservedAt != "") != tc.want.servesWinners() {
					t.Fatalf("%s freshness watermark = %q, want one only when the route serves winners (%q)", what, freshness.LatestObservedAt, tc.want)
				}
			}
		})
	}
}

func TestGroupedInventoryReadFollowsTheRoute(t *testing.T) {
	t.Parallel()
	for _, tc := range existenceFilters {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			filter := AggregateFilter{ImageRef: tc.image, DetectionProfile: tc.profile}
			if tc.granted {
				filter.AllowedScopeIDs = []string{"scope:a"}
			}
			probe := routeQueryer()
			store := NewPostgresGroupedInventoryStoreWithReadStore(probe, true, nil)
			store.Now = func() time.Time { return time.Now().UTC() }
			page, err := store.SupplyChainImpactGroupedInventory(t.Context(), filter, GroupedInventoryByService, 10, 0)
			if err != nil {
				t.Fatalf("grouped inventory = %v", err)
			}
			read := probe.queries[len(probe.queries)-1]
			if got := routeOf(read); got != tc.want {
				t.Fatalf("grouped inventory read the %q model, want %q", got, tc.want)
			}
			if (page.Freshness.LatestObservedAt != "") != tc.want.servesWinners() {
				t.Fatalf("grouped freshness watermark = %q, want one only when the route serves winners (%q)", page.Freshness.LatestObservedAt, tc.want)
			}
		})
	}
}

// TestSupplementRouteRefusesWhenSupportsAreNotReady pins the fail-closed rule for
// the V route: the winners watermark being fresh does not make a supplement-served
// answer exact, so a supports table that is not ready refuses (ruling V, will not
// accept: a fallback to bare winners when supports are not ready).
func TestSupplementRouteRefusesWhenSupportsAreNotReady(t *testing.T) {
	t.Parallel()
	notReady := func() *routeFake {
		probe := routeQueryer()
		inner := probe.onRows
		probe.onRows = func(query string) (db.Rows, error) {
			if strings.Contains(query, "supply_chain_impact_finding_supports_backfill") {
				return &cov7StoreRows{rows: [][]any{{false}}}, nil
			}
			return inner(query)
		}
		return probe
	}
	probe := notReady()
	if _, err := NewPostgresFindingStoreWithReadStore(probe, true).ListSupplyChainImpactFindings(t.Context(),
		FindingFilter{Limit: 10, Status: "affected_exact", DetectionProfile: ProfilePrecise}); !isSupportsUnavailable(err) {
		t.Fatalf("list = %v, want ErrFindingSupportsUnavailable", err)
	}
	probe = notReady()
	if _, err := NewPostgresAggregateStoreWithReadStore(probe, true).CountSupplyChainImpactFindings(t.Context(),
		AggregateFilter{Status: "affected_exact", DetectionProfile: ProfilePrecise}); !isSupportsUnavailable(err) {
		t.Fatalf("count = %v, want an error matching ErrFindingSupportsUnavailable", err)
	}
	probe = notReady()
	if _, err := NewPostgresGroupedInventoryStoreWithReadStore(probe, true, nil).SupplyChainImpactGroupedInventory(t.Context(),
		AggregateFilter{DetectionProfile: ProfilePrecise}, GroupedInventoryByService, 10, 0); !isSupportsUnavailable(err) {
		t.Fatalf("grouped = %v, want an error matching ErrFindingSupportsUnavailable", err)
	}
	for _, query := range probe.queries {
		if routeOf(query) != "" {
			t.Fatalf("a refused call issued a read statement (never winners or legacy):\n%.80s", query)
		}
	}
}

func isSupportsUnavailable(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrFindingSupportsUnavailable.Error())
}
