// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

// migrationName is the definition name of the read-model migration: the file
// name without its number and extension.
const migrationName = "config_artifact_read_model"

// migrationSQL returns the shipped read-model migration with SQL comments
// removed and whitespace collapsed, so the shape assertions compare statements
// and not layout.
func migrationSQL(t *testing.T) string {
	t.Helper()
	for _, def := range migrations.BootstrapDefinitions() {
		if def.Name == migrationName {
			return normalizeSQL(def.SQL)
		}
	}
	t.Fatalf("migration %q not found in BootstrapDefinitions()", migrationName)
	return ""
}

var (
	sqlLineComment = regexp.MustCompile(`--[^\n]*`)
	sqlWhitespace  = regexp.MustCompile(`\s+`)
)

func normalizeSQL(sql string) string {
	return strings.TrimSpace(sqlWhitespace.ReplaceAllString(sqlLineComment.ReplaceAllString(sql, " "), " "))
}

// TestMigrationCreatesTheReadModelTables pins the #7126 PR3b-1a schema: the
// three read-model tables, the IMMUTABLE UTF8 wrapper, the generated digest
// key over the length-prefixed fields, the autovacuum setting on the rows
// table, and the backfill marker and walker cursor tables in the shape of
// migration 109.
func TestMigrationCreatesTheReadModelTables(t *testing.T) {
	t.Parallel()

	sql := migrationSQL(t)
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS config_artifact_content_versions ( repo_id TEXT PRIMARY KEY, version BIGINT NOT NULL, bumped_xid xid8 NOT NULL, bumped_at TIMESTAMPTZ NOT NULL );",
		"CREATE TABLE IF NOT EXISTS config_artifact_repo_state ( repo_id TEXT PRIMARY KEY, content_version BIGINT NULL, extractor_version INTEGER NOT NULL, row_count INTEGER NULL, files_listed INTEGER NOT NULL DEFAULT 0, candidate_count INTEGER NOT NULL DEFAULT 0, candidates_read INTEGER NOT NULL DEFAULT 0, list_truncated BOOLEAN NOT NULL DEFAULT FALSE, scope_id TEXT NOT NULL DEFAULT '', generation_id TEXT NOT NULL DEFAULT '', derived_at TIMESTAMPTZ NULL, failure_count INTEGER NOT NULL DEFAULT 0, last_error_class TEXT NULL, last_failure_at TIMESTAMPTZ NULL, last_failure_extractor_version INTEGER NULL );",
		"CREATE OR REPLACE FUNCTION config_artifact_utf8(t text) RETURNS bytea LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT RETURN convert_to(t, 'UTF8');",
		"CREATE TABLE IF NOT EXISTS config_artifact_paths ( repo_id TEXT NOT NULL, raw_path TEXT NOT NULL, relative_path TEXT NOT NULL, evidence_kind TEXT NOT NULL, name_scoped BOOLEAN NOT NULL, row_digest BYTEA GENERATED ALWAYS AS (sha256(config_artifact_utf8( length(raw_path)::text || ':' || raw_path || length(relative_path)::text || ':' || relative_path || evidence_kind))) STORED, PRIMARY KEY (repo_id, row_digest) ) WITH (autovacuum_vacuum_scale_factor = 0.02);",
		"CREATE TABLE IF NOT EXISTS config_artifact_backfill_markers ( marker_name TEXT PRIMARY KEY, completed_at TIMESTAMPTZ NOT NULL );",
		"CREATE TABLE IF NOT EXISTS config_artifact_reconcile_cursor ( walk_name TEXT PRIMARY KEY, cursor TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL );",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("migration %s is missing\n  %s\ngot:\n  %s", migrationName, want, sql)
		}
	}
}

// TestMigrationLeavesContentFilesAlone pins the PR3b-1a boundary (S3 ruling
// 4.1): the migration creates tables only. The version-fence triggers and the
// version-0 seed belong to PR3b-1b, after the S3 arm 3 and arm 5 reruns, so
// this file must not touch content_files, create a trigger, or write a row.
func TestMigrationLeavesContentFilesAlone(t *testing.T) {
	t.Parallel()

	sql := migrationSQL(t)
	for _, forbidden := range []string{
		`content_files`,
		`TRIGGER`,
		`INSERT`,
		`UPDATE`,
		`DELETE`,
		`TRUNCATE`,
		`ALTER\s+TABLE`,
		`CREATE\s+INDEX`,
	} {
		if regexp.MustCompile(`(?i)\b` + forbidden + `\b`).MatchString(sql) {
			t.Errorf("migration %s contains %q; PR3b-1a creates the read-model tables only", migrationName, forbidden)
		}
	}
}
