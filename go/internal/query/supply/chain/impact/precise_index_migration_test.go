// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"os"
	"strings"
	"testing"
)

// TestPreciseIndexMigrationCarriesThePredicate pins that migration 343 indexes the
// supports the precise filter selects, with the read's own predicate text. The
// planner uses a partial index only when the read's WHERE clause implies the index
// predicate, so a read and an index that drift apart read the whole table (a
// sequential scan, or a probe that fetches every support of a key) and fail the
// 1,000 ms bar silently. A change to the precise filter changes
// SupportsPreciseIndexPredicateSQL and needs a NEW index migration.
func TestPreciseIndexMigrationCarriesThePredicate(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../storage/postgres/migrations/343_supply_chain_impact_finding_supports_precise_key_ordered_idx.sql")
	if err != nil {
		t.Fatalf("read migration 343: %v", err)
	}
	ddl := strings.Join(strings.Fields(string(raw)), " ")
	// Comments carry the same words, so compare against the statement alone.
	_, rest, found := strings.Cut(ddl, "CREATE INDEX CONCURRENTLY")
	if !found {
		t.Fatal("the migration has no CREATE INDEX CONCURRENTLY statement")
	}
	statement := "CREATE INDEX CONCURRENTLY" + rest
	want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_precise_key_ordered_idx " +
		"ON supply_chain_impact_finding_supports (canonical_key, priority_score DESC, has_payload_finding_id DESC, fact_id ASC) " +
		"WHERE " + strings.Join(strings.Fields(SupportsPreciseIndexPredicateSQL), " ") + ";"
	if statement != want {
		t.Fatalf("migration 343 differs from the read's precise predicate\nmigration: %s\nwant:      %s", statement, want)
	}
}

// TestImageIndexMigrationShape pins migration 344: partial on a non-empty image
// reference, one concurrent statement.
func TestImageIndexMigrationShape(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../storage/postgres/migrations/344_supply_chain_impact_finding_supports_image_ref_idx.sql")
	if err != nil {
		t.Fatalf("read migration 344: %v", err)
	}
	ddl := strings.Join(strings.Fields(string(raw)), " ")
	_, rest, found := strings.Cut(ddl, "CREATE INDEX CONCURRENTLY")
	if !found {
		t.Fatal("the migration has no CREATE INDEX CONCURRENTLY statement")
	}
	statement := "CREATE INDEX CONCURRENTLY" + rest
	const want = "CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_image_ref_idx " +
		"ON supply_chain_impact_finding_supports (image_ref) WHERE image_ref <> '';"
	if statement != want {
		t.Fatalf("migration 344 = %q, want %q", statement, want)
	}
}

// TestPreciseStatusPruneNamesTheKeyParts pins the status prune the route V supplement
// applies: precise needs one of two statuses and a non-empty observed version.
func TestPreciseStatusPruneNamesTheKeyParts(t *testing.T) {
	t.Parallel()
	got := supplyChainImpactPreciseStatusPruneSQL("w")
	const want = "w.impact_status IN ('affected_exact', 'not_affected_known_fixed') AND w.observed_version <> ''"
	if got != want {
		t.Fatalf("prune = %q, want %q", got, want)
	}
}
