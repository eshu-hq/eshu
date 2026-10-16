// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

// digestRow is one config_artifact_paths row the digest proofs insert.
type digestRow struct {
	rawPath, relativePath, evidenceKind string
}

// goRowDigest recomputes row_digest the way migration 170 defines it:
// sha256 over the UTF8 bytes of length(raw_path) ':' raw_path
// length(relative_path) ':' relative_path evidence_kind. PostgreSQL length()
// counts characters, so the prefix is the rune count, not the byte count.
// Production Go never computes the digest; this copy exists only to prove the
// schema's definition from outside the database.
func goRowDigest(row digestRow) []byte {
	payload := fmt.Sprintf("%d:%s%d:%s%s",
		utf8.RuneCountInString(row.rawPath), row.rawPath,
		utf8.RuneCountInString(row.relativePath), row.relativePath,
		row.evidenceKind)
	sum := sha256.Sum256([]byte(payload))
	return sum[:]
}

// incompressible returns n characters of base64 over random bytes, the path
// shape that broke a four-TEXT-column key at about 2,610 bytes in S2.
func incompressible(t *testing.T, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)[:n]
}

// TestRowDigestMatchesGoRecomputationLive inserts rows whose fields hold the
// separators, multi-byte characters, empty strings and an incompressible
// 20,000-character path, then compares every stored row_digest with a Go
// recomputation and with the same expression recomputed in SQL. The two pipe
// rows prove the length prefix: without it they would share a digest.
func TestRowDigestMatchesGoRecomputationLive(t *testing.T) {
	ctx, db := openDatabase(t)

	rows := []digestRow{
		{"a|b", "c", "k"},
		{"a", "b|c", "k"},
		{"3:a", "b", "k"},
		{"é/ü/日本", "deploy/values.yaml", "helm_values_reference"},
		{"", "", ""},
		{incompressible(t, 20000), "main.tf", "terraform_module_source"},
		{"modules/vpc", "envs/prod/main.tf", "terraform_module_source"},
	}
	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `
INSERT INTO config_artifact_paths (repo_id, raw_path, relative_path, evidence_kind, name_scoped)
VALUES ('repo-digest', $1, $2, $3, true)`, row.rawPath, row.relativePath, row.evidenceKind); err != nil {
			t.Fatalf("insert %q: %v", row.rawPath[:min(len(row.rawPath), 20)], err)
		}
	}

	result, err := db.QueryContext(ctx, `
SELECT raw_path, relative_path, evidence_kind, row_digest,
       row_digest = sha256(convert_to(
           length(raw_path)::text || ':' || raw_path ||
           length(relative_path)::text || ':' || relative_path ||
           evidence_kind, 'UTF8')) AS sql_match
FROM config_artifact_paths WHERE repo_id = 'repo-digest'`)
	if err != nil {
		t.Fatalf("read digests: %v", err)
	}
	defer func() { _ = result.Close() }()
	seen := map[string]bool{}
	count := 0
	for result.Next() {
		var row digestRow
		var stored []byte
		var sqlMatch bool
		if err := result.Scan(&row.rawPath, &row.relativePath, &row.evidenceKind, &stored, &sqlMatch); err != nil {
			t.Fatal(err)
		}
		count++
		if want := goRowDigest(row); !bytes.Equal(stored, want) {
			t.Errorf("row_digest for raw_path %.20q = %x, Go recomputation = %x", row.rawPath, stored, want)
		}
		if !sqlMatch {
			t.Errorf("row_digest for raw_path %.20q differs from the SQL recomputation", row.rawPath)
		}
		if len(stored) != sha256.Size {
			t.Errorf("row_digest length = %d, want %d", len(stored), sha256.Size)
		}
		seen[string(stored)] = true
	}
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	if count != len(rows) || len(seen) != len(rows) {
		t.Fatalf("read %d rows with %d distinct digests, want %d of each", count, len(seen), len(rows))
	}
}

// TestRowKeyRejectsADuplicateRowLive proves the key is the digest of the
// four data columns: the same (raw_path, relative_path, evidence_kind) in one
// repository is a unique violation whatever name_scoped says, and the same row
// in another repository is not.
func TestRowKeyRejectsADuplicateRowLive(t *testing.T) {
	ctx, db := openDatabase(t)

	const insert = `
INSERT INTO config_artifact_paths (repo_id, raw_path, relative_path, evidence_kind, name_scoped)
VALUES ($1, 'deploy/app', 'kustomization.yaml', 'kustomize_resource', $2)`
	if _, err := db.ExecContext(ctx, insert, "repo-a", false); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err := db.ExecContext(ctx, insert, "repo-a", true)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("duplicate row error = %v, want SQLSTATE 23505", err)
	}
	if _, err := db.ExecContext(ctx, insert, "repo-b", false); err != nil {
		t.Fatalf("same row in another repository: %v", err)
	}
}

// TestReadModelCatalogLive checks what the migration installs beyond column
// shape: the wrapper is IMMUTABLE (the generated column needs it), the rows
// table carries the 2% autovacuum threshold, and exactly the four migration
// 184 fence triggers on content_files call a config_artifact function
// (PR3b-1b); their firing order is TestVersionFenceTriggersFireLastLive.
func TestReadModelCatalogLive(t *testing.T) {
	ctx, db := openDatabase(t)

	var volatility string
	if err := db.QueryRowContext(ctx, `
SELECT provolatile FROM pg_proc WHERE proname = 'config_artifact_utf8'`).Scan(&volatility); err != nil {
		t.Fatalf("read config_artifact_utf8: %v", err)
	}
	if volatility != "i" {
		t.Fatalf("config_artifact_utf8 provolatile = %q, want i (IMMUTABLE)", volatility)
	}

	var options string
	if err := db.QueryRowContext(ctx, `
SELECT coalesce(array_to_string(reloptions, ','), '') FROM pg_class WHERE relname = 'config_artifact_paths'`).Scan(&options); err != nil {
		t.Fatalf("read config_artifact_paths reloptions: %v", err)
	}
	if !strings.Contains(options, "autovacuum_vacuum_scale_factor=0.02") {
		t.Fatalf("config_artifact_paths reloptions = %v, want autovacuum_vacuum_scale_factor=0.02", options)
	}

	var fenceTriggers int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_trigger tg
JOIN pg_proc p ON p.oid = tg.tgfoid
WHERE tg.tgrelid = 'content_files'::regclass AND p.proname LIKE 'config_artifact%'`).Scan(&fenceTriggers); err != nil {
		t.Fatalf("read content_files triggers: %v", err)
	}
	if fenceTriggers != len(fenceTriggerNames) {
		t.Fatalf("content_files has %d config_artifact triggers, want the %d version-fence triggers", fenceTriggers, len(fenceTriggerNames))
	}

	for _, table := range []string{
		"config_artifact_content_versions", "config_artifact_repo_state", "config_artifact_paths",
		"config_artifact_backfill_markers", "config_artifact_reconcile_cursor",
	} {
		var rows int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		if rows != 0 {
			t.Fatalf("%s has %d rows after bootstrap, want 0: the seed of an empty content_files is empty", table, rows)
		}
	}
}

// TestMigrationReRunsLive applies migration 170 a second time on a database
// that already has it, as the local supervisor does on every start with no
// ledger, and proves the digest definition survives the CREATE OR REPLACE of
// its wrapper.
func TestMigrationReRunsLive(t *testing.T) {
	ctx, db := openDatabase(t)

	var migration string
	for _, def := range migrations.BootstrapDefinitions() {
		if def.Name == "config_artifact_read_model" {
			migration = def.SQL
		}
	}
	if migration == "" {
		t.Fatal("migration config_artifact_read_model not found")
	}
	if _, err := db.ExecContext(ctx, migration); err != nil {
		t.Fatalf("re-apply migration 170: %v", err)
	}
	row := digestRow{"envs/prod", "terragrunt.hcl", "terragrunt_include_path"}
	var stored []byte
	if err := db.QueryRowContext(ctx, `
INSERT INTO config_artifact_paths (repo_id, raw_path, relative_path, evidence_kind, name_scoped)
VALUES ('repo-rerun', $1, $2, $3, true) RETURNING row_digest`,
		row.rawPath, row.relativePath, row.evidenceKind).Scan(&stored); err != nil {
		t.Fatalf("insert after re-apply: %v", err)
	}
	if want := goRowDigest(row); !bytes.Equal(stored, want) {
		t.Fatalf("row_digest after re-apply = %x, want %x", stored, want)
	}
}
