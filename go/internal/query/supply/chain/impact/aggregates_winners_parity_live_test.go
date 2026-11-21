// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 slice 2b disposable PostgreSQL 18 proof that the winners-sourced aggregate read (count, priority/severity buckets, grouped inventory) returns byte-identical envelopes to the legacy fact read across the grouped-inventory seed: unfiltered, precise profile, priority floor, suppressed inclusion with live and expired operator suppressions, all five inventory dimensions, and scoped-token repository and scope grants (which stay on the legacy CTE, mirroring the list read); reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
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

// TestAggregatesWinnersParityLive proves the #10880 slice-2b reader
// switch: for every filter below the winners aggregate read returns
// exactly the envelopes the legacy fact read returns, even though it never
// scans reducer_supply_chain_impact_finding facts. Scoped-grant filters
// stay on the legacy CTE by construction (mirroring the list read), so
// those cases prove the fallback, not the winners pick.
func TestAggregatesWinnersParityLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(
		t,
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DSN"),
		os.Getenv("ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE"),
		5*time.Minute,
		func(ctx context.Context, db *sql.DB) error {
			return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
		},
	)
	seedGroupedInventoryLive(t, ctx, database)
	if err := storagepostgres.NewSupplyChainImpactWinnersStore(storagepostgres.SQLDB{DB: database}).
		RebuildAllWinners(ctx); err != nil {
		t.Fatalf("RebuildAllWinners: %v", err)
	}

	fixedNow := time.Now().UTC()
	legacy := NewPostgresAggregateStore(database)
	legacy.Now = func() time.Time { return fixedNow }
	winners := NewPostgresAggregateStoreWithReadModel(database, true)
	winners.Now = func() time.Time { return fixedNow }

	filters := map[string]AggregateFilter{
		"unfiltered":          {Status: "affected_exact"},
		"precise":             {Status: "affected_exact", DetectionProfile: ProfilePrecise},
		"include_suppressed":  {Status: "affected_exact", IncludeSuppressed: true},
		"suppression_state":   {Status: "affected_exact", SuppressionState: "expired", IncludeSuppressed: true},
		"min_priority":        {MinPriorityScore: 60},
		"scoped_repositories": {Status: "affected_exact", AllowedRepositoryIDs: []string{"repository:r1", "repository:r5"}},
		"scoped_scopes":       {Status: "affected_exact", AllowedScopeIDs: []string{"git-repository-scope:repository:r2"}},
	}
	dimensions := []InventoryDimension{
		InventoryByStatus,
		InventoryByPriorityBucket,
		InventoryBySeverity,
		InventoryByRepository,
		InventoryByEcosystem,
	}
	for name, filter := range filters {
		t.Run("count/"+name, func(t *testing.T) {
			want, err := legacy.CountSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("legacy count: %v", err)
			}
			got, err := winners.CountSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("winners count: %v", err)
			}
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if string(wantJSON) != string(gotJSON) {
				t.Fatalf("winners count differs from legacy count\nlegacy:  %s\nwinners: %s", wantJSON, gotJSON)
			}
		})
		for _, dimension := range dimensions {
			t.Run("inventory/"+name+"/"+string(dimension), func(t *testing.T) {
				want, err := legacy.SupplyChainImpactInventory(ctx, filter, dimension, 50, 0)
				if err != nil {
					t.Fatalf("legacy inventory: %v", err)
				}
				got, err := winners.SupplyChainImpactInventory(ctx, filter, dimension, 50, 0)
				if err != nil {
					t.Fatalf("winners inventory: %v", err)
				}
				wantJSON, _ := json.Marshal(want)
				gotJSON, _ := json.Marshal(got)
				if string(wantJSON) != string(gotJSON) {
					t.Fatalf("winners inventory differs from legacy inventory\nlegacy:  %s\nwinners: %s", wantJSON, gotJSON)
				}
			})
		}
	}
}
