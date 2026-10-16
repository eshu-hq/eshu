// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
)

const (
	packageManifestConsumptionBackfillIndexName      = "fact_records_package_manifest_consumption_backfill_idx"
	packageManifestConsumptionBackfillIndexMigration = "migrations/181_fact_records_package_manifest_consumption_backfill_idx.sql"
	packageManifestConsumptionPagePredicateHead      = "WHERE scope_id = $1\n  AND fact_id > $2\n  AND "
	packageManifestConsumptionPagePredicateTail      = "\nORDER BY fact_id\n"
)

// derivePackageManifestConsumptionPagePredicate cuts the fact predicate out
// of the shipped page query, so the index contract below is checked against
// the production text and never against a hand-frozen copy.
func derivePackageManifestConsumptionPagePredicate(t *testing.T) string {
	t.Helper()
	query := listPackageManifestConsumptionKeyScopeFactsSQL
	for _, marker := range []string{packageManifestConsumptionPagePredicateHead, packageManifestConsumptionPagePredicateTail} {
		if count := strings.Count(query, marker); count != 1 {
			t.Fatalf("page query holds %d copies of boundary marker %q, want exactly 1", count, marker)
		}
	}
	start := strings.Index(query, packageManifestConsumptionPagePredicateHead) + len(packageManifestConsumptionPagePredicateHead)
	end := strings.Index(query, packageManifestConsumptionPagePredicateTail)
	if end <= start {
		t.Fatalf("page query boundary markers are out of order")
	}
	predicate := query[start:end]
	// Hermetic prefix guard: the derived predicate, put back between its
	// markers, must be a byte-for-byte slice of the production query.
	if !strings.Contains(query, packageManifestConsumptionPagePredicateHead+predicate+packageManifestConsumptionPagePredicateTail) {
		t.Fatalf("derived page predicate does not reassemble into the production query")
	}
	if !strings.HasPrefix(predicate, "(") || !strings.HasSuffix(predicate, ")") {
		t.Fatalf("derived page predicate %q is not one parenthesized group", predicate)
	}
	return predicate
}

// TestPackageManifestConsumptionBackfillIndexPredicateMatchesPageSQL locks
// migration 181's partial-index predicate to the backfill's page query. If the
// two drift, the page silently falls back to the per-page heap walk and sort
// that held a 63k-fact scope's transaction open for over an hour (#7663).
func TestPackageManifestConsumptionBackfillIndexPredicateMatchesPageSQL(t *testing.T) {
	t.Parallel()

	predicate := derivePackageManifestConsumptionPagePredicate(t)
	migration, err := os.ReadFile(packageManifestConsumptionBackfillIndexMigration)
	if err != nil {
		t.Fatalf("read migration 181: %v", err)
	}
	want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + packageManifestConsumptionBackfillIndexName +
		"\n    ON fact_records (scope_id, fact_id)\n    WHERE " + predicate + ";\n"
	if !strings.HasSuffix(string(migration), want) {
		t.Fatalf("migration 181 must end with the index built on the page query's exact predicate:\n%s", want)
	}
	if count := strings.Count(string(migration), ";"); count != 1 {
		t.Fatalf("migration 181 holds %d statements, want exactly one CREATE INDEX CONCURRENTLY", count)
	}
}

// TestPackageManifestConsumptionScopeListSharesPagePredicate keeps the
// initial pass's DISTINCT scope walk on the same predicate, so it reads the
// partial index (an index-only scan) instead of every fact above the cursor.
func TestPackageManifestConsumptionScopeListSharesPagePredicate(t *testing.T) {
	t.Parallel()

	predicate := derivePackageManifestConsumptionPagePredicate(t)
	want := "WHERE scope_id > $1\n  AND " + predicate + "\nORDER BY scope_id\n"
	if !strings.Contains(listPackageManifestConsumptionKeyScopesSQL, want) {
		t.Fatalf("scope-list query no longer shares the page predicate; want it to contain:\n%s", want)
	}
}
