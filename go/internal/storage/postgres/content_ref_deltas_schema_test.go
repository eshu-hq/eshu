// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestRefDeltaMigrationCreatesOverlayTables pins the #5393 Phase 1 storage
// shape: a separate content_file_ref_deltas table keyed by
// (repo_id, ref, relative_path) with its own trigram index -- never a ref
// column on content_files -- plus the per-ref base_default_sha ledger that
// drives the bounded re-diff trigger.
func TestRefDeltaMigrationCreatesOverlayTables(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL("content_file_ref_deltas")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS content_file_ref_deltas",
		"PRIMARY KEY (repo_id, ref, relative_path)",
		"content_file_ref_deltas_content_trgm_idx",
		"ON content_file_ref_deltas USING gin (content gin_trgm_ops)",
		"CREATE TABLE IF NOT EXISTS content_file_ref_delta_bases",
		"PRIMARY KEY (repo_id, ref)",
		"base_default_sha TEXT NOT NULL",
		"is_deleted BOOLEAN NOT NULL DEFAULT FALSE",
		"(is_deleted AND content IS NULL) OR (NOT is_deleted AND content IS NOT NULL)",
	} {
		if !strings.Contains(migration, want) {
			t.Fatalf("ref-deltas migration missing %q:\n%s", want, migration)
		}
	}
}

// TestRefDeltaMigrationTouchesNoCanonicalTable pins the isolation contract
// at the DDL layer: the migration creates only the overlay tables. In
// particular it adds no column, index, trigger, or foreign key to
// content_files, so applying it cannot fence or reshape canonical writes.
func TestRefDeltaMigrationTouchesNoCanonicalTable(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL("content_file_ref_deltas")
	for _, forbidden := range []string{
		"ALTER TABLE content_files",
		"REFERENCES content_files",
		"REFERENCES content_file_ref_deltas",
		"ON content_files",
		"TRIGGER",
		"CREATE INDEX CONCURRENTLY",
	} {
		if strings.Contains(migration, forbidden) {
			t.Fatalf("ref-deltas migration must not contain %q:\n%s", forbidden, migration)
		}
	}
}

// TestRefDeltaSchemaSQLDeclaresOverlayTables pins the Go DDL side of the
// parity contract: every table, column, and index RefDeltaSchemaSQL creates
// must exist under the same name in migration 197, which
// TestDDLMigrationParity enforces across the whole tree.
func TestRefDeltaSchemaSQLDeclaresOverlayTables(t *testing.T) {
	t.Parallel()

	schema := RefDeltaSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS content_file_ref_deltas",
		"repo_id TEXT NOT NULL",
		"ref TEXT NOT NULL",
		"relative_path TEXT NOT NULL",
		"content TEXT NULL",
		"content_hash TEXT NOT NULL",
		"is_deleted BOOLEAN NOT NULL DEFAULT FALSE",
		"indexed_at TIMESTAMPTZ NOT NULL",
		"PRIMARY KEY (repo_id, ref, relative_path)",
		"CREATE TABLE IF NOT EXISTS content_file_ref_delta_bases",
		"base_default_sha TEXT NOT NULL",
		"updated_at TIMESTAMPTZ NOT NULL",
		"PRIMARY KEY (repo_id, ref)",
		"content_file_ref_deltas_content_trgm_idx",
		"ON content_file_ref_deltas USING gin (content gin_trgm_ops)",
		"(is_deleted AND content IS NULL) OR (NOT is_deleted AND content IS NOT NULL)",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("RefDeltaSchemaSQL() missing %q:\n%s", want, schema)
		}
	}
}
