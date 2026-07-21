// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
)

// TestRelationshipFamilyCandidateIndexMigratesExistingDeployment is the
// existing-deployment idempotency regression for #5483 C2. This repository
// re-executes every migration on every boot with no version ledger, and
// `CREATE INDEX ... IF NOT EXISTS <name>` matches only the index NAME, not its
// definition. So a same-name edit to the relationship-family candidate partial
// index's WHERE (adding the Flux GitRepository arm) would be a SILENT NO-OP on
// any deployment where the original index already exists — the index would keep
// its old predicate forever and never cover Flux rows, defeating the partial
// index for the deferred backfill's Flux path (a scale performance regression,
// correctness still held by the predicate SQL).
//
// The fresh-DB live tests cannot catch this because they create the index new
// (already with the Flux arm). This test simulates the EXISTING-deployment path:
// it first creates the LEGACY-name index with an OLD Flux-less predicate, THEN
// applies the C2 relationship-family index migrations, and asserts the covering
// index now includes the Flux arm and the legacy index is gone.
//
// RED on the pre-fix same-name approach: applying `CREATE ... IF NOT EXISTS
// <legacy-name>` over the pre-existing legacy index is a no-op, so no index
// gains the Flux arm. GREEN after the rename fix: 059 creates the _v2 index WITH
// the Flux arm (a new name, so IF NOT EXISTS actually builds it) and 068 drops
// the legacy index.
func TestRelationshipFamilyCandidateIndexMigratesExistingDeployment(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	schemaName := provisionDeferredPartitionMemoSchema(t, db)

	const legacyIndexName = "fact_records_relationship_family_scope_generation_idx"

	// Simulate an existing deployment that applied the ORIGINAL migration 059:
	// the legacy-name partial index with a Flux-less WHERE. CONCURRENTLY DDL
	// runs in autocommit (never a transaction block).
	if _, err := db.ExecContext(ctx, `
CREATE INDEX CONCURRENTLY IF NOT EXISTS `+legacyIndexName+`
    ON fact_records (scope_id, generation_id, observed_at, fact_id)
    WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
      AND lower(COALESCE(payload->>'artifact_type', '')) = 'argocd'`); err != nil {
		t.Fatalf("seed legacy relationship-family index: %v", err)
	}
	// Precondition: the seeded legacy index must NOT cover Flux, so the
	// post-migration assertion proves the migration performed the update rather
	// than observing a pre-existing state.
	if fluxCoveringIndexPresent(t, ctx, db, schemaName) {
		t.Fatal("precondition failed: the seeded legacy index already covers Flux; it must be Flux-less")
	}

	// Apply the C2 relationship-family index migrations exactly as boot does:
	// 059 creates the renamed _v2 index (with the Flux arm), then the sibling
	// migration drops the legacy index. Each is a single CONCURRENTLY statement.
	applyMigrationByName(t, ctx, db, "relationship_family_candidate_index")
	applyMigrationByNameIfPresent(t, ctx, db, "drop_relationship_family_candidate_index_legacy")

	// The covering index must now include the Flux GitRepository arm. On the
	// pre-fix same-name code this is false (the legacy index was never rebuilt).
	if !fluxCoveringIndexPresent(t, ctx, db, schemaName) {
		t.Fatal("after applying the C2 migrations on an existing deployment, no fact_records index covers " +
			"the Flux GitRepository arm; the same-name CREATE IF NOT EXISTS silently kept the old predicate")
	}
	// The legacy-name index must be dropped so it no longer shadows the v2 index.
	if indexExists(t, ctx, db, schemaName, legacyIndexName) {
		t.Fatalf("legacy index %q still exists after migration; the drop migration did not run", legacyIndexName)
	}
}

// applyMigrationByName executes the named embedded migration's SQL. CONCURRENTLY
// DDL runs in autocommit via a bare ExecContext (single statement, no
// transaction block).
func applyMigrationByName(t *testing.T, ctx context.Context, db *sql.DB, name string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, MigrationSQL(name)); err != nil {
		t.Fatalf("apply migration %q: %v", name, err)
	}
}

// applyMigrationByNameIfPresent applies the named migration only when it exists
// in the embedded set, so this test still runs (and goes RED) when the fix that
// adds the drop migration is reverted.
func applyMigrationByNameIfPresent(t *testing.T, ctx context.Context, db *sql.DB, name string) {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if def.Name == name {
			if _, err := db.ExecContext(ctx, def.SQL); err != nil {
				t.Fatalf("apply migration %q: %v", name, err)
			}
			return
		}
	}
}

// fluxCoveringIndexPresent reports whether any index on fact_records in the
// given schema has a definition mentioning the Flux GitRepository parsed bucket
// — i.e. the partial-index WHERE actually gained the Flux arm.
func fluxCoveringIndexPresent(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) bool {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_indexes
WHERE schemaname = $1
  AND tablename = 'fact_records'
  AND indexdef LIKE '%flux_git_repositories%'`, schemaName).Scan(&count); err != nil {
		t.Fatalf("query flux-covering index presence: %v", err)
	}
	return count > 0
}

// indexExists reports whether a named index exists on fact_records in the schema.
func indexExists(t *testing.T, ctx context.Context, db *sql.DB, schemaName, indexName string) bool {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_indexes
WHERE schemaname = $1
  AND tablename = 'fact_records'
  AND indexname = $2`, schemaName, indexName).Scan(&count); err != nil {
		t.Fatalf("query index existence: %v", err)
	}
	return count > 0
}
