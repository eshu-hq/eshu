// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 slice 2 disposable PostgreSQL 18 proof that the winners-payload list read (migration 331 stored payload, no fact_records payload join) returns byte-identical FindingRow pages to the legacy fact read across the grouped-inventory seed: unfiltered, precise profile, priority sorts, cursor paging, suppressed inclusion with live and expired operator suppressions, and scoped-token repository and scope grants; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	pgmigrations "github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestFindingsWinnersPayloadParityLive proves the #10880 slice-2
// reader switch: for every filter below the winners read returns
// exactly the rows the legacy fact read returns, including the
// operator-suppression overlay (live and expired) and scoped-token
// grants, even though it never joins fact_records for payloads.
func TestFindingsWinnersPayloadParityLive(t *testing.T) {
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

	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}
	winners := PostgresFindingStore{DB: database, ReadFromWinners: true}
	// The store requires a selector; the seed's status serves as the
	// broad one so each case varies only its named dimension.
	filters := map[string]FindingFilter{
		"unfiltered":          {Limit: 50, Status: "affected_exact"},
		"precise":             {Limit: 50, Status: "affected_exact", DetectionProfile: ProfilePrecise},
		"priority_desc":       {Limit: 50, Status: "affected_exact", Sort: "priority_score_desc"},
		"priority_asc":        {Limit: 50, Status: "affected_exact", Sort: "priority_score_asc"},
		"cursor":              {Limit: 2, Status: "affected_exact", Sort: "priority_score_desc"},
		"include_suppressed":  {Limit: 50, Status: "affected_exact", IncludeSuppressed: true},
		"suppression_state":   {Limit: 50, Status: "affected_exact", SuppressionState: "expired", IncludeSuppressed: true},
		"scoped_repositories": {Limit: 50, Status: "affected_exact", AllowedRepositoryIDs: []string{"repository:r1", "repository:r5"}},
		"scoped_scopes":       {Limit: 50, Status: "affected_exact", AllowedScopeIDs: []string{"git-repository-scope:repository:r2"}},
		"min_priority":        {Limit: 50, MinPriorityScore: 60},
	}
	for name, filter := range filters {
		t.Run(name, func(t *testing.T) {
			want, err := legacy.ListSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("legacy list: %v", err)
			}
			got, err := winners.ListSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("winners list: %v", err)
			}
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if string(wantJSON) != string(gotJSON) {
				t.Fatalf("winners page differs from legacy page\nlegacy:  %s\nwinners: %s", wantJSON, gotJSON)
			}
		})
	}
}

// TestFindingsWinnersPayloadBackfillServesWithoutResweepLive is the
// #10880 P1-1 regression: migration 331 alone leaves pre-existing
// winner rows with payload '{}' and source_confidence 'unknown', and
// the slice-2 list read serves win.payload directly, so between
// deploy and the first post-deploy resweep the impact list would
// serve empty payloads. This proof recreates that post-331 state
// (typed winners built by a resweep, then the two new columns reset
// to their 331 defaults), applies ONLY the 332 backfill migration,
// and requires the winners read to match the legacy fact read
// without any resweep in between.
func TestFindingsWinnersPayloadBackfillServesWithoutResweepLive(t *testing.T) {
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

	// Recreate the exact state migration 331 leaves on an upgraded
	// deployment: every pre-existing winner row at the column defaults.
	if _, err := database.ExecContext(ctx, `
UPDATE supply_chain_impact_canonical_winners
SET payload = '{}'::jsonb, source_confidence = 'unknown'`); err != nil {
		t.Fatalf("reset winners payload columns: %v", err)
	}
	var empty int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE payload = '{}'::jsonb`).Scan(&empty); err != nil {
		t.Fatalf("count empty winner payloads: %v", err)
	}
	if empty == 0 {
		t.Fatal("seed produced no winners to backfill")
	}

	backfill := ""
	for _, def := range pgmigrations.BootstrapDefinitions() {
		if def.Name == "supply_chain_impact_winners_payload_backfill" {
			backfill = def.SQL
		}
	}
	if backfill == "" {
		t.Fatal("migration 332 supply_chain_impact_winners_payload_backfill not found in BootstrapDefinitions " +
			"(winners read would serve '{}' payloads until the first post-deploy resweep)")
	}
	if _, err := database.ExecContext(ctx, backfill); err != nil {
		t.Fatalf("apply backfill migration: %v", err)
	}
	// Idempotency: a replayed apply (untracked database, retried
	// bootstrap) must not disturb the filled rows.
	if _, err := database.ExecContext(ctx, backfill); err != nil {
		t.Fatalf("replay backfill migration: %v", err)
	}

	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE payload = '{}'::jsonb`).Scan(&empty); err != nil {
		t.Fatalf("recount empty winner payloads: %v", err)
	}
	if empty != 0 {
		t.Fatalf("%d winner rows still carry the '{}' default payload after the backfill", empty)
	}
	var wrongConfidence int
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM supply_chain_impact_canonical_winners AS w
JOIN fact_records AS fact ON fact.fact_id = w.winner_fact_id
WHERE w.source_confidence IS DISTINCT FROM fact.source_confidence`).Scan(&wrongConfidence); err != nil {
		t.Fatalf("count winner source_confidence mismatches: %v", err)
	}
	if wrongConfidence != 0 {
		t.Fatalf("%d winner rows carry a source_confidence other than their winner fact's", wrongConfidence)
	}

	// No resweep runs between the backfill and these reads: the page
	// payloads come from the backfilled winners rows alone.
	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}
	winners := PostgresFindingStore{DB: database, ReadFromWinners: true}
	for name, filter := range map[string]FindingFilter{
		"unfiltered":         {Limit: 50, Status: "affected_exact"},
		"include_suppressed": {Limit: 50, Status: "affected_exact", IncludeSuppressed: true},
		"suppression_state":  {Limit: 50, Status: "affected_exact", SuppressionState: "expired", IncludeSuppressed: true},
	} {
		t.Run(name, func(t *testing.T) {
			want, err := legacy.ListSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("legacy list: %v", err)
			}
			got, err := winners.ListSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("winners list: %v", err)
			}
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if string(wantJSON) != string(gotJSON) {
				t.Fatalf("backfilled winners page differs from legacy page\nlegacy:  %s\nwinners: %s", wantJSON, gotJSON)
			}
		})
	}
}
