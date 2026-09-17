// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestBootstrapBinaryRecordsPostgresMigrationsLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_BOOTSTRAP_CMD_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_BOOTSTRAP_CMD_TEST_DSN to an empty disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	executor := bootstrapSQLDB{SQLDB: postgres.SQLDB{DB: db}}
	if err := applyPostgresSchema(ctx, executor, func(string) string { return "" }); err != nil {
		t.Fatalf("apply bootstrap binary Postgres schema: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM eshu_schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migration receipts: %v", err)
	}
	if want := len(postgres.BootstrapDefinitions()); count != want {
		t.Fatalf("migration receipts = %d, want %d", count, want)
	}
}
