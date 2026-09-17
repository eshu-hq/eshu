// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBootstrapSkipsAppliedMigrationsUnderReaderLockLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
	}

	applyDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open apply connection: %v", err)
	}
	t.Cleanup(func() { _ = applyDB.Close() })
	blockerDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open blocker connection: %v", err)
	}
	t.Cleanup(func() { _ = blockerDB.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := ApplyBootstrap(ctx, SQLDB{DB: applyDB}); err != nil {
		t.Fatalf("first ApplyBootstrap: %v", err)
	}

	blocker, err := blockerDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin reader transaction: %v", err)
	}
	defer func() { _ = blocker.Rollback() }()
	var rows int
	if err := blocker.QueryRowContext(ctx, "SELECT count(*) FROM scope_generations").Scan(&rows); err != nil {
		t.Fatalf("hold scope_generations read lock: %v", err)
	}

	replayCtx, replayCancel := context.WithTimeout(ctx, 10*time.Second)
	defer replayCancel()
	if err := ApplyBootstrap(replayCtx, SQLDB{DB: applyDB}); err != nil {
		t.Fatalf("reapply completed schema while a reader holds ACCESS SHARE: %v", err)
	}
}

func TestBootstrapRejectsChangedRecordedMigrationBeforeDDLLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	definitions := BootstrapDefinitions()
	last := definitions[len(definitions)-1]
	if _, err := db.ExecContext(ctx,
		"UPDATE eshu_schema_migrations SET checksum_sha256 = $1 WHERE path = $2 AND variant = 'full'",
		strings.Repeat("0", 64), last.Path,
	); err != nil {
		t.Fatalf("plant checksum drift: %v", err)
	}
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(),
			"UPDATE eshu_schema_migrations SET checksum_sha256 = $1 WHERE path = $2 AND variant = 'full'",
			migrationChecksum(last.SQL), last.Path,
		)
		if err != nil {
			t.Errorf("restore checksum: %v", err)
		}
	})

	reader, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin reader: %v", err)
	}
	defer func() { _ = reader.Rollback() }()
	var rows int
	if err := reader.QueryRowContext(ctx, "SELECT count(*) FROM scope_generations").Scan(&rows); err != nil {
		t.Fatalf("hold read lock: %v", err)
	}

	applyCtx, applyCancel := context.WithTimeout(ctx, 8*time.Second)
	defer applyCancel()
	err = ApplyBootstrap(applyCtx, SQLDB{DB: db})
	if err == nil || !strings.Contains(err.Error(), "checksum changed") {
		t.Fatalf("ApplyBootstrap() error = %v, want checksum drift before DDL", err)
	}
}

func TestBootstrapDeferredContentMigrationUpgradesToFullLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DEFERRED_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DEFERRED_TEST_DSN to an empty disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply deferred bootstrap: %v", err)
	}
	var exists bool
	if err := db.QueryRowContext(ctx,
		"SELECT to_regclass('content_files_content_trgm_idx') IS NOT NULL",
	).Scan(&exists); err != nil {
		t.Fatalf("inspect deferred index: %v", err)
	}
	if exists {
		t.Fatal("deferred bootstrap unexpectedly built the content trigram index")
	}

	reader, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin reader: %v", err)
	}
	var rows int
	if err := reader.QueryRowContext(ctx, "SELECT count(*) FROM content_files").Scan(&rows); err != nil {
		_ = reader.Rollback()
		t.Fatalf("hold content read lock: %v", err)
	}
	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: db}); err != nil {
		_ = reader.Rollback()
		t.Fatalf("reapply deferred bootstrap under reader lock: %v", err)
	}
	if err := reader.Rollback(); err != nil {
		t.Fatalf("release reader: %v", err)
	}

	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("upgrade deferred bootstrap to full: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		"SELECT to_regclass('content_files_content_trgm_idx') IS NOT NULL",
	).Scan(&exists); err != nil {
		t.Fatalf("inspect full index: %v", err)
	}
	if !exists {
		t.Fatal("full bootstrap omitted the content trigram index")
	}

	var content Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "content_store" {
			content = def
			break
		}
	}
	if content.Name == "" {
		t.Fatal("content_store migration missing")
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE eshu_schema_migrations SET checksum_sha256 = $1 WHERE path = $2 AND variant = 'full'",
		strings.Repeat("0", 64), content.Path,
	); err != nil {
		t.Fatalf("plant full checksum drift: %v", err)
	}
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(),
			"UPDATE eshu_schema_migrations SET checksum_sha256 = $1 WHERE path = $2 AND variant = 'full'",
			migrationChecksum(content.SQL), content.Path,
		)
		if err != nil {
			t.Errorf("restore full checksum: %v", err)
		}
	})
	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: db}); err == nil || !strings.Contains(err.Error(), "checksum changed") {
		t.Fatalf("deferred bootstrap error = %v, want full checksum drift", err)
	}
}

func TestBootstrapReplaysUntrackedExistingDatabaseOnceLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_LEGACY_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_LEGACY_TEST_DSN to an empty disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	definitions := BootstrapDefinitions()
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("apply legacy schema without migration ledger: %v", err)
	}
	legacyStarted := time.Now()
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("replay legacy schema without migration ledger: %v", err)
	}
	t.Logf("empty-corpus legacy replay duration: %s", time.Since(legacyStarted))
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("replay and track legacy schema: %v", err)
	}
	var recorded int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM eshu_schema_migrations WHERE variant = 'full'",
	).Scan(&recorded); err != nil {
		t.Fatalf("count recorded migrations: %v", err)
	}
	if recorded != len(definitions) {
		t.Fatalf("recorded migrations = %d, want %d", recorded, len(definitions))
	}

	reader, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin reader: %v", err)
	}
	defer func() { _ = reader.Rollback() }()
	var rows int
	if err := reader.QueryRowContext(ctx, "SELECT count(*) FROM scope_generations").Scan(&rows); err != nil {
		t.Fatalf("hold scope_generations read lock: %v", err)
	}
	trackedStarted := time.Now()
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("reapply recorded schema under reader lock: %v", err)
	}
	t.Logf("empty-corpus tracked replay duration: %s", time.Since(trackedStarted))
}

func TestBootstrapPreservesReceiptsAfterLaterMigrationFailureLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_RETRY_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_RETRY_TEST_DSN to an empty disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	definitions := []Definition{
		{Name: "first", Path: "test/001_first.sql", SQL: "CREATE TABLE first_migration (id INT)"},
		{Name: "second", Path: "test/002_second.sql", SQL: "INVALID SQL"},
	}
	if err := applyBootstrapDefinitions(ctx, SQLDB{DB: db}, definitions); err == nil {
		t.Fatal("first apply unexpectedly succeeded")
	}
	var recorded int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM eshu_schema_migrations").Scan(&recorded); err != nil {
		t.Fatalf("count retained receipts: %v", err)
	}
	if recorded != 1 {
		t.Fatalf("retained receipts = %d, want 1", recorded)
	}
	definitions[1].SQL = "CREATE TABLE second_migration (id INT)"
	if err := applyBootstrapDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("resume after later migration failure: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM eshu_schema_migrations").Scan(&recorded); err != nil {
		t.Fatalf("count final receipts: %v", err)
	}
	if recorded != 2 {
		t.Fatalf("final receipts = %d, want 2", recorded)
	}
}
