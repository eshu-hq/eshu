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

func TestSQLDBRebuildsInvalidConcurrentIndexLive(t *testing.T) {
	if os.Getenv(schemaLockTimeoutProofEnv) != "1" {
		t.Skip("set ESHU_SCHEMA_LOCK_TIMEOUT_PROOF=1 and ESHU_POSTGRES_DSN to run live schema lock-timeout proof")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run live schema lock-timeout proof")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openSchemaLockTimeoutProofDB(t, dsn)
	tableName := fmt.Sprintf("schema_invalid_concurrent_idx_%d", time.Now().UnixNano())
	indexName := tableName + "_value_idx"
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+tableName); err != nil {
			t.Errorf("cleanup: drop %s: %v", tableName, err)
		}
	})

	if _, err := db.ExecContext(ctx, "CREATE TABLE "+tableName+" (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)"); err != nil {
		t.Fatalf("create proof table: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+tableName+" (id, value) VALUES (1, 7), (2, 7)"); err != nil {
		t.Fatalf("seed duplicate rows: %v", err)
	}

	exec := SQLDB{DB: db}
	def := Definition{
		Name: "invalid_concurrent_unique_index",
		Path: "schema_invalid_concurrent_unique_index.sql",
		SQL:  "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS " + indexName + " ON " + tableName + " (value)",
	}
	if err := ApplyDefinitionsWithLockTimeout(ctx, exec, []Definition{def}, 5*time.Second); err == nil {
		t.Fatal("ApplyDefinitionsWithLockTimeout() duplicate unique index error = nil, want non-nil")
	}
	if valid := proofIndexValidity(t, ctx, db, indexName); valid {
		t.Fatalf("index %s is valid after failed concurrent unique build, want invalid", indexName)
	}

	if _, err := db.ExecContext(ctx, "DELETE FROM "+tableName+" WHERE id = 2"); err != nil {
		t.Fatalf("remove duplicate row: %v", err)
	}
	if err := ApplyDefinitionsWithLockTimeout(ctx, exec, []Definition{def}, 5*time.Second); err != nil {
		t.Fatalf("ApplyDefinitionsWithLockTimeout() rebuild error = %v, want nil", err)
	}
	if valid := proofIndexValidity(t, ctx, db, indexName); !valid {
		t.Fatalf("index %s is invalid after rebuild, want valid", indexName)
	}
}

func proofIndexValidity(t *testing.T, ctx context.Context, db *sql.DB, indexName string) bool {
	t.Helper()
	var valid bool
	if err := db.QueryRowContext(ctx, `
SELECT i.indisvalid
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
WHERE c.relname = $1
`, indexName).Scan(&valid); err != nil {
		t.Fatalf("query index validity for %s: %v", indexName, err)
	}
	return valid
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
