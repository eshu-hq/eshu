// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestBootstrapRetryAfterRecordedIndexRecoveryFailsLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_RECOVERY_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_RECOVERY_TEST_DSN to an empty disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	definitions := []Definition{
		{Name: "table", Path: "test/001_recovery_table.sql", SQL: "CREATE TABLE eshu_6738_recovery (id INT)"},
		{Name: "index", Path: "test/002_recovery_index.sql", SQL: "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS eshu_6738_recovery_idx ON eshu_6738_recovery (id)"},
	}
	apply := func() error {
		return applyBootstrapDefinitions(ctx, SQLDB{DB: db}, definitions, slog.Default())
	}
	if err := apply(); err != nil {
		t.Fatalf("apply initial migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DROP INDEX eshu_6738_recovery_idx"); err != nil {
		t.Fatalf("drop recorded index before recovery: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO eshu_6738_recovery (id) VALUES (1), (1)"); err != nil {
		t.Fatalf("plant duplicate rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, definitions[1].SQL); err == nil {
		t.Fatal("duplicate rows unexpectedly allowed a unique index")
	}
	var valid bool
	if err := db.QueryRowContext(ctx, `SELECT i.indisvalid FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = 'eshu_6738_recovery_idx'`).Scan(&valid); err != nil || valid {
		t.Fatalf("failed build left index valid or absent: valid=%t err=%v", valid, err)
	}
	if err := apply(); err == nil {
		t.Fatal("recovery unexpectedly succeeded while duplicate rows remain")
	}
	var receipts int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'",
		definitions[1].Path,
	).Scan(&receipts); err != nil {
		t.Fatalf("count index migration receipts after failed recovery: %v", err)
	}
	if receipts != 0 {
		t.Fatalf("failed recovery retained %d success receipts, want 0", receipts)
	}
	// Model a stop after invalid-index cleanup but before the replacement build.
	if _, err := db.ExecContext(ctx, "DROP INDEX eshu_6738_recovery_idx"); err != nil {
		t.Fatalf("drop invalid index before retry: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"DELETE FROM eshu_6738_recovery WHERE ctid IN (SELECT ctid FROM eshu_6738_recovery LIMIT 1)",
	); err != nil {
		t.Fatalf("remove duplicate row: %v", err)
	}
	if err := apply(); err != nil {
		t.Fatalf("retry recovery: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT i.indisvalid FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = 'eshu_6738_recovery_idx'`).Scan(&valid); err != nil || !valid {
		t.Fatalf("recovered index invalid or absent: valid=%t err=%v", valid, err)
	}
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'",
		definitions[1].Path,
	).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("successful recovery receipts = %d, err=%v, want 1", receipts, err)
	}
}
