// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// #9279 fixture values: the deferred 004 checksums pinned in
// migrations/checksum_alias_test.go (that package cannot be reused directly
// since these live tests need the root Definition list to compute the real
// deferred checksum for restore).
const (
	migration004DeferredPath            = "go/internal/storage/postgres/migrations/004_content_store.sql"
	migration004DeferredShippedChecksum = "3d8ef124d1e6b5641f5d84fe581ede18046fcde70f6aad5adb3934611562f5f3"
	migration004DeferredPre8028Checksum = "027b3563ab35a7824f5add728209998f530497a1ed3a7293844bd5e4ad53fdf4"
	migration004DeferredUnknownChecksum = "2222222222222222222222222222222222222222222222222222222222222222"
	migration004LanguageRepoIndex       = "content_files_language_repo_idx"
)

// deferredDefinitionByName returns the current
// BootstrapDefinitionsWithoutContentSearchIndexes() entry for name, or fails
// the test -- used to compute the real deferred checksum a test must restore
// after deliberately planting a different one in the ledger.
func deferredDefinitionByName(t *testing.T, name string) Definition {
	t.Helper()
	for _, def := range BootstrapDefinitionsWithoutContentSearchIndexes() {
		if def.Name == name {
			return def
		}
	}
	t.Fatalf("no BootstrapDefinitionsWithoutContentSearchIndexes() entry for name %q", name)
	return Definition{}
}

// plantDeferredChecksumAndRestore overwrites the deferred variant's recorded
// checksum for path and registers a t.Cleanup that restores it to the real,
// current deferred checksum -- the same share-one-database convention
// schema_migration_7002_live_test.go uses, so these #9279 tests can run
// against one disposable database alongside the #7002 tests.
func plantDeferredChecksumAndRestore(t *testing.T, ctx context.Context, db *sql.DB, path, checksum string) {
	t.Helper()
	real := migrationChecksum(deferredDefinitionByName(t, "content_store").SQL)
	res, err := db.ExecContext(ctx,
		"UPDATE eshu_schema_migrations SET checksum_sha256 = $1 WHERE path = $2 AND variant = 'deferred'",
		checksum, path,
	)
	if err != nil {
		t.Fatalf("plant deferred checksum for %s: %v", path, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("plant deferred checksum for %s: updated %d rows, want 1 (is the database deferred-bootstrapped?)", path, n)
	}
	t.Cleanup(func() {
		res, err := db.ExecContext(context.Background(),
			"UPDATE eshu_schema_migrations SET checksum_sha256 = $1 WHERE path = $2 AND variant = 'deferred'",
			real, path,
		)
		if err != nil {
			t.Errorf("restore deferred checksum for %s: %v", path, err)
			return
		}
		if n, _ := res.RowsAffected(); n != 1 {
			t.Errorf("restore deferred checksum for %s: updated %d rows, want 1", path, n)
		}
	})
}

// open9279LiveDB opens the disposable PostgreSQL database these tests prove
// against. It must be empty of Eshu state on first use; every test
// bootstraps what it needs through the production deferred path.
func open9279LiveDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// languageRepoIndexDefLive reads the live definition of the language index an
// operator would inspect after an upgrade.
func languageRepoIndexDefLive(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var def string
	if err := db.QueryRowContext(ctx,
		"SELECT pg_get_indexdef($1::regclass)", migration004LanguageRepoIndex,
	).Scan(&def); err != nil {
		t.Fatalf("read %s definition: %v", migration004LanguageRepoIndex, err)
	}
	return def
}

// TestBootstrapAcceptsPre8028DeferredChecksum004Live reproduces #9279's exact
// upgrade failure end to end: a database whose 004 deferred ledger row
// predates #8028 (recorded 027b3563...) and whose on-disk index is still the
// legacy two-column definition must bootstrap successfully once the deferred
// alias lands, and migrations 171 to 173 must converge the index to the
// covering definition. Before the #9279 fix this fails with "checksum
// changed".
func TestBootstrapAcceptsPre8028DeferredChecksum004Live(t *testing.T) {
	db := open9279LiveDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("initial deferred apply bootstrap: %v", err)
	}

	// Stand where a pre-#8028 install stands. 171 to 173 shipped in the
	// same commit as the deferred constant edit (2e15c61d4c), so an install
	// from just before it holds neither those ledger rows nor the covering
	// index: unrecord 171-173 and downgrade the index to the legacy
	// two-column definition 171's guard keys off.
	for _, path := range []string{
		"go/internal/storage/postgres/migrations/171_drop_content_files_language_repo_idx.sql",
		"go/internal/storage/postgres/migrations/172_content_files_language_covering_idx.sql",
		"go/internal/storage/postgres/migrations/173_content_files_language_covering_idx_vacuum.sql",
	} {
		if _, err := db.ExecContext(ctx,
			"DELETE FROM eshu_schema_migrations WHERE path = $1", path,
		); err != nil {
			t.Fatalf("unrecord %s to simulate the pre-#8028 install: %v", path, err)
		}
	}
	if _, err := db.ExecContext(ctx, "DROP INDEX IF EXISTS "+migration004LanguageRepoIndex); err != nil {
		t.Fatalf("drop covering index to simulate the pre-#8028 install: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"CREATE INDEX "+migration004LanguageRepoIndex+" ON content_files (language, repo_id)"); err != nil {
		t.Fatalf("recreate legacy index to simulate the pre-#8028 install: %v", err)
	}
	if got := languageRepoIndexDefLive(t, ctx, db); strings.Contains(got, "indexed_at") {
		t.Fatalf("legacy fixture already carries indexed_at: %q", got)
	}

	plantDeferredChecksumAndRestore(t, ctx, db, migration004DeferredPath, migration004DeferredPre8028Checksum)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := ApplyBootstrapWithOptions(ctx, SQLDB{DB: db}, BootstrapOptions{
		DeferContentSearchIndexes: true,
		Logger:                    logger,
	}); err != nil {
		t.Fatalf("ApplyBootstrapWithoutContentSearchIndexes() with ledger recording pre-#8028 deferred 004 = %v, want success", err)
	}
	if !strings.Contains(logs.String(), "bootstrap.postgres.migration.checksum_alias_accepted") {
		t.Fatalf("deferred bootstrap with superseded 004 checksum did not log checksum_alias_accepted, got:\n%s", logs.String())
	}

	if got := languageRepoIndexDefLive(t, ctx, db); !strings.Contains(got, "(language, repo_id)") || !strings.Contains(got, "INCLUDE (indexed_at)") {
		t.Fatalf("%s definition after upgrade = %q, want (language, repo_id) INCLUDE (indexed_at)", migration004LanguageRepoIndex, got)
	}
}

// TestBootstrapAcceptsCurrentDeferredChecksum004Live is the control the issue
// requires: installs created or hand-fixed since #8028 (recorded 3d8ef124...,
// equal to the current deferred checksum) keep passing through the plain
// equality branch with no alias involved.
func TestBootstrapAcceptsCurrentDeferredChecksum004Live(t *testing.T) {
	db := open9279LiveDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("initial deferred apply bootstrap: %v", err)
	}
	plantDeferredChecksumAndRestore(t, ctx, db, migration004DeferredPath, migration004DeferredShippedChecksum)

	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrapWithoutContentSearchIndexes() with ledger recording current deferred 004 = %v, want success", err)
	}
}

// TestBootstrapRejectsUnknownDeferredChecksum004Live proves the 004 alias is
// not a blanket bypass: an unrecognized recorded deferred checksum for 004
// still fails loud.
func TestBootstrapRejectsUnknownDeferredChecksum004Live(t *testing.T) {
	db := open9279LiveDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("initial deferred apply bootstrap: %v", err)
	}
	plantDeferredChecksumAndRestore(t, ctx, db, migration004DeferredPath, migration004DeferredUnknownChecksum)

	err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: db})
	if err == nil || !strings.Contains(err.Error(), "checksum changed") || !strings.Contains(err.Error(), "(deferred)") {
		t.Fatalf("ApplyBootstrapWithoutContentSearchIndexes() error = %v, want deferred checksum changed for an unrecognized 004 checksum", err)
	}
}
