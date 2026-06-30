// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const schemaLockTimeoutProofEnv = "ESHU_SCHEMA_LOCK_TIMEOUT_PROOF"

func TestSQLDBSchemaLockTimeoutLive(t *testing.T) {
	if os.Getenv(schemaLockTimeoutProofEnv) != "1" {
		t.Skip("set ESHU_SCHEMA_LOCK_TIMEOUT_PROOF=1 and ESHU_POSTGRES_DSN to run live schema lock-timeout proof")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run live schema lock-timeout proof")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	applyDB := openSchemaLockTimeoutProofDB(t, dsn)
	applyDB.SetMaxOpenConns(1)
	applyDB.SetMaxIdleConns(1)
	blockerDB := openSchemaLockTimeoutProofDB(t, dsn)

	tableName := fmt.Sprintf("schema_lock_timeout_proof_%d", time.Now().UnixNano())
	indexName := tableName + "_id_idx"
	t.Cleanup(func() {
		if _, err := applyDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+tableName); err != nil {
			t.Errorf("cleanup: drop %s: %v", tableName, err)
		}
	})

	exec := SQLDB{DB: applyDB}
	err := ApplyDefinitionsWithLockTimeout(ctx, exec, []Definition{
		{
			Name: "proof_table",
			Path: "schema_lock_timeout_proof_table.sql",
			SQL:  "CREATE TABLE " + tableName + " (id INTEGER NOT NULL)",
		},
		{
			Name: "proof_index",
			Path: "schema_lock_timeout_proof_index.sql",
			SQL:  "CREATE INDEX CONCURRENTLY " + indexName + " ON " + tableName + " (id)",
		},
	}, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("ApplyDefinitionsWithLockTimeout() with concurrent index error = %v, want nil", err)
	}

	blockerTx, err := blockerDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin blocker transaction: %v", err)
	}
	defer func() { _ = blockerTx.Rollback() }()
	if _, err := blockerTx.ExecContext(ctx, "LOCK TABLE "+tableName+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock proof table: %v", err)
	}

	start := time.Now()
	err = ApplyDefinitionsWithLockTimeout(ctx, exec, []Definition{
		{
			Name: "blocked_alter",
			Path: "schema_lock_timeout_blocked_alter.sql",
			SQL:  "ALTER TABLE " + tableName + " ADD COLUMN IF NOT EXISTS blocked TEXT",
		},
	}, 150*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("ApplyDefinitionsWithLockTimeout() blocked DDL error = nil, want lock timeout")
	}
	if !strings.Contains(err.Error(), "apply blocked_alter") {
		t.Fatalf("blocked DDL error = %v, want wrapped definition name", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("blocked DDL elapsed = %s, want under 2s", elapsed)
	}

	if err := blockerTx.Rollback(); err != nil {
		t.Fatalf("rollback blocker transaction: %v", err)
	}

	var lockTimeout string
	if err := applyDB.QueryRowContext(ctx, "SHOW lock_timeout").Scan(&lockTimeout); err != nil {
		t.Fatalf("show lock_timeout: %v", err)
	}
	if lockTimeout != "0" && lockTimeout != "0ms" {
		t.Fatalf("lock_timeout after failed schema apply = %q, want reset to 0", lockTimeout)
	}
	err = ApplyDefinitionsWithLockTimeout(ctx, exec, []Definition{
		{
			Name: "after_reset_alter",
			Path: "schema_lock_timeout_after_reset_alter.sql",
			SQL:  "ALTER TABLE " + tableName + " ADD COLUMN IF NOT EXISTS after_reset TEXT",
		},
	}, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("ApplyDefinitionsWithLockTimeout() after reset error = %v, want nil", err)
	}
}

func openSchemaLockTimeoutProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	return db
}
