// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

	bootstrap, err := telemetry.NewBootstrap("eshu-bootstrap-data-plane")
	if err != nil {
		t.Fatalf("create telemetry bootstrap: %v", err)
	}
	var logs bytes.Buffer
	logger := newLogger(bootstrap, &logs)
	executor := bootstrapSQLDB{SQLDB: postgres.SQLDB{DB: db}}
	if err := applyPostgresSchema(ctx, executor, func(string) string { return "" }, logger); err != nil {
		t.Fatalf("apply bootstrap binary Postgres schema: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM eshu_schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migration receipts: %v", err)
	}
	if want := len(postgres.BootstrapDefinitions()); count != want {
		t.Fatalf("migration receipts = %d, want %d", count, want)
	}
	completed := false
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("decode migration log: %v", err)
		}
		if entry["message"] != "postgres schema migrations complete" {
			continue
		}
		completed = true
		if entry["service_name"] != "eshu-bootstrap-data-plane" ||
			entry["runtime_role"] != "bootstrap-data-plane" ||
			entry["event_name"] != "bootstrap.postgres.migrations.complete" {
			t.Fatalf("migration completion log lacks runtime identity: %+v", entry)
		}
	}
	if !completed {
		t.Fatal("structured logger did not receive migration completion")
	}
}
