// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestListActiveFindingCVEIDsLive proves the read returns the distinct
// non-blank ids on active winners only: suppressed rows and blank ids never
// reach the derivation, and one id shared by two winners arrives once.
func TestListActiveFindingCVEIDsLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the finding CVE ids proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	seedFindingCVEWinnersLive(t, ctx, db)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupFindingCVEWinnersLive(t, cleanupCtx, db)
	})

	ids, err := NewFactStore(SQLDB{DB: db}).ListActiveFindingCVEIDs(ctx)
	if err != nil {
		t.Fatalf("ListActiveFindingCVEIDs() error = %v", err)
	}
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	for _, want := range []string{"CVE-2024-0001", "CVE-2024-0002", "GHSA-abcd-1234-efgh"} {
		if !found[want] {
			t.Fatalf("ListActiveFindingCVEIDs() missing %q in %q", want, ids)
		}
	}
	for _, absent := range []string{"", "CVE-2024-9999"} {
		if found[absent] {
			t.Fatalf("ListActiveFindingCVEIDs() must not return %q (suppressed or blank)", absent)
		}
	}
	count := 0
	for _, id := range ids {
		if id == "CVE-2024-0001" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("CVE-2024-0001 appears %d times, want 1 (distinct)", count)
	}
}

func seedFindingCVEWinnersLive(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seed := []struct {
		key   string
		cve   string
		state string
	}{
		{"test-epss-live-1", "CVE-2024-0001", "active"},
		{"test-epss-live-2", "CVE-2024-0001", "active"},
		{"test-epss-live-3", "CVE-2024-0002", "active"},
		{"test-epss-live-4", "GHSA-abcd-1234-efgh", "active"},
		{"test-epss-live-5", "CVE-2024-9999", "suppressed"},
		{"test-epss-live-6", "", "active"},
	}
	for _, row := range seed {
		if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_canonical_winners
    (canonical_key, winner_fact_id, finding_id, cve_id, suppression_state, materialized_at)
    VALUES ($1, $2, $3, $4, $5, NOW())
    ON CONFLICT (canonical_key) DO UPDATE SET
        cve_id = EXCLUDED.cve_id,
        suppression_state = EXCLUDED.suppression_state,
        materialized_at = EXCLUDED.materialized_at`,
			row.key, "fact-"+row.key, "finding-"+row.key, row.cve, row.state); err != nil {
			t.Fatalf("seed finding CVE winner %q: %v", row.key, err)
		}
	}
}

func cleanupFindingCVEWinnersLive(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`DELETE FROM supply_chain_impact_canonical_winners WHERE canonical_key LIKE 'test-epss-live-%'`); err != nil {
		t.Fatalf("cleanup finding CVE winners: %v", err)
	}
}
