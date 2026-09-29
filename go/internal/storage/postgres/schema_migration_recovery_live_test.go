// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
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
		return applyBootstrapDefinitionsWith(ctx, SQLDB{DB: db}, definitions, slog.Default(), schemaBootstrapCoordination{})
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

type lockWaitLogWriter struct {
	seen chan struct{}
}

func (writer lockWaitLogWriter) Write(data []byte) (int, error) {
	if strings.Contains(string(data), "event_name=bootstrap.postgres.migration.lock_wait") {
		select {
		case writer.seen <- struct{}{}:
		default:
		}
	}
	return len(data), nil
}

func TestBootstrapRetryAllowanceExcludesSuccessfulMigrationTimeLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_RECOVERY_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_RECOVERY_TEST_DSN to an empty disposable PostgreSQL database")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	table := "eshu_7247_retry_" + suffix
	partialTable := "eshu_7247_retry_partial_" + suffix
	firstPath := "test/7247_retry_success_" + suffix + ".sql"
	secondPath := "test/7247_retry_contended_" + suffix + ".sql"
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+table+" (id INT)"); err != nil {
		t.Fatalf("create retry fixture table: %v", err)
	}
	t.Cleanup(func() { _, _ = database.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })
	blocker, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lock-holder transaction: %v", err)
	}
	var rowCount int
	if err := blocker.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&rowCount); err != nil {
		_ = blocker.Rollback()
		t.Fatalf("hold read lock on retry fixture table: %v", err)
	}
	definitions := []Definition{
		{Name: "successful_wait", Path: firstPath, SQL: "SELECT pg_sleep(2)"},
		{Name: "contended_ddl", Path: secondPath, SQL: fmt.Sprintf(
			"CREATE TABLE %s (id INT); ALTER TABLE %s ADD COLUMN recovered BOOLEAN",
			partialTable, table,
		)},
	}
	t.Cleanup(func() {
		_ = blocker.Rollback()
		_, _ = database.ExecContext(context.Background(),
			"DELETE FROM eshu_schema_migrations WHERE path IN ($1, $2)", firstPath, secondPath)
		_, _ = database.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+partialTable)
	})

	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire bootstrap connection: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	lockWait := make(chan struct{}, 1)
	logger := slog.New(slog.NewTextHandler(lockWaitLogWriter{seen: lockWait}, nil))
	executor := schemaConnectionExecutor{
		database: SQLDB{DB: database},
		conn:     connection,
		logger:   logger,
	}
	result := make(chan error, 1)
	go func() {
		result <- executor.applyTrackedDefinitions(ctx, definitions, schemaStatementBounds{
			lockTimeout:     100 * time.Millisecond,
			lockRetryBudget: 1500 * time.Millisecond,
		}, logger)
	}()
	// The first migration takes longer than the entire retry allowance. Wait
	// for the second migration's 100 ms lock timeout, then prove an ordinary
	// writer proceeds during retry backoff while the read transaction remains.
	select {
	case <-lockWait:
	case <-ctx.Done():
		t.Fatalf("wait for first lock retry: %v", ctx.Err())
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO "+table+" (id) VALUES (1)"); err != nil {
		t.Fatalf("ordinary writer blocked during retry backoff: %v", err)
	}
	var partialExists bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", partialTable).Scan(&partialExists); err != nil || partialExists {
		t.Fatalf("partial table exists=%t err=%v before retry, want failed multistatement attempt rolled back", partialExists, err)
	}
	if err := blocker.Commit(); err != nil {
		t.Fatalf("release lock-holder transaction: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("bootstrap after earlier successful migration exceeded retry allowance: %v", err)
	}

	if err := database.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", partialTable).Scan(&partialExists); err != nil || !partialExists {
		t.Fatalf("partial table exists=%t err=%v after retry, want successful migration", partialExists, err)
	}
	var recovered bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns WHERE table_name = $1 AND column_name = 'recovered'
	)`, table).Scan(&recovered); err != nil || !recovered {
		t.Fatalf("contended migration column recovered=%t err=%v, want true", recovered, err)
	}
	var receipts int
	if err := database.QueryRowContext(ctx,
		"SELECT count(*) FROM eshu_schema_migrations WHERE path IN ($1, $2)", firstPath, secondPath,
	).Scan(&receipts); err != nil || receipts != 2 {
		t.Fatalf("migration receipts=%d err=%v, want 2", receipts, err)
	}
}

func TestBootstrapRetryAllowanceExhaustsOnPersistentLockLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_RECOVERY_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_RECOVERY_TEST_DSN to an empty disposable PostgreSQL database")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	table := "eshu_7247_retry_persistent_" + suffix
	partialTable := "eshu_7247_retry_persistent_partial_" + suffix
	path := "test/7247_retry_persistent_" + suffix + ".sql"
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+table+" (id INT)"); err != nil {
		t.Fatalf("create retry fixture table: %v", err)
	}
	t.Cleanup(func() { _, _ = database.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })
	blocker, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lock-holder transaction: %v", err)
	}
	var rowCount int
	if err := blocker.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&rowCount); err != nil {
		_ = blocker.Rollback()
		t.Fatalf("hold read lock on retry fixture table: %v", err)
	}
	t.Cleanup(func() {
		_ = blocker.Rollback()
		_, _ = database.ExecContext(context.Background(), "DELETE FROM eshu_schema_migrations WHERE path = $1", path)
		_, _ = database.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+partialTable)
	})

	connection, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire bootstrap connection: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	executor := schemaConnectionExecutor{
		database: SQLDB{DB: database},
		conn:     connection,
		logger:   slog.Default(),
	}
	definition := Definition{
		Name: "persistent_contention",
		Path: path,
		SQL:  fmt.Sprintf("CREATE TABLE %s (id INT); ALTER TABLE %s ADD COLUMN recovered BOOLEAN", partialTable, table),
	}
	started := time.Now()
	err = executor.applyTrackedDefinitions(ctx, []Definition{definition}, schemaStatementBounds{
		lockTimeout:     100 * time.Millisecond,
		lockRetryBudget: 1500 * time.Millisecond,
	}, slog.Default())
	if err == nil || !coordination.IsLockNotAvailable(err) {
		t.Fatalf("persistent contention error = %v, want exhausted retry budget wrapping 55P03", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("persistent retry elapsed %s, want bounded exhaustion under 5 s", elapsed)
	}
	var partialExists bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", partialTable).Scan(&partialExists); err != nil || partialExists {
		t.Fatalf("partial table exists=%t err=%v after exhausted migration, want rollback", partialExists, err)
	}
	var receipts int
	if err := database.QueryRowContext(ctx,
		"SELECT count(*) FROM eshu_schema_migrations WHERE path = $1", path,
	).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("migration receipts=%d err=%v after exhausted migration, want 0", receipts, err)
	}
}
