// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPgxWorkSnapshotOnIsolatedPostgres exercises the actual pgx scan and SQL.
// The DSN must point to a disposable server: BeginSnapshot resets server-wide
// pg_stat_statements statistics, including other databases.
func TestPgxWorkSnapshotOnIsolatedPostgres(t *testing.T) {
	dsn := os.Getenv("READ_API_SNAPSHOT_TEST_DSN")
	if dsn == "" {
		t.Skip("set READ_API_SNAPSHOT_TEST_DSN for an isolated PostgreSQL fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := EnsureWorkMeterExtension(ctx, pool); err != nil {
		t.Fatal(err)
	}
	meter := pgxWorkMeter{pool: pool}
	reset, err := meter.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := meter.EndSnapshot(ctx, reset)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Counters != (WorkCounters{}) || len(empty.Statements) != 0 {
		t.Fatalf("fresh reset snapshot = %+v, want empty", empty)
	}
	var value int
	if err := pool.QueryRow(ctx, "SELECT 17").Scan(&value); err != nil || value != 17 {
		t.Fatalf("seed statement = %d, %v", value, err)
	}
	got, err := meter.EndSnapshot(ctx, reset)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := meter.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != "database_interval" || got.Counters != legacy || got.Counters.Calls < 1 || len(got.Statements) == 0 {
		t.Fatalf("snapshot = %+v, legacy = %+v", got, legacy)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "SELECT 17") {
		t.Fatalf("snapshot retained statement text: %s", encoded)
	}
	if _, err := meter.BeginSnapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := meter.EndSnapshot(ctx, reset); err == nil {
		t.Fatal("accepted a snapshot after a different global reset")
	}
}

func TestPgxWorkSnapshotRejectsDeallocationOnIsolatedPostgres(t *testing.T) {
	dsn := os.Getenv("READ_API_SNAPSHOT_TEST_DSN")
	if dsn == "" {
		t.Skip("set READ_API_SNAPSHOT_TEST_DSN for an isolated PostgreSQL fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := EnsureWorkMeterExtension(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var capacity int
	if err := pool.QueryRow(ctx, "SELECT current_setting('pg_stat_statements.max')::int").Scan(&capacity); err != nil {
		t.Fatal(err)
	}
	if capacity > 100 {
		t.Skipf("deallocation fixture needs pg_stat_statements.max <= 100, got %d", capacity)
	}
	meter := pgxWorkMeter{pool: pool}
	reset, err := meter.BeginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		if _, err := pool.Exec(ctx, fmt.Sprintf("SELECT count(*) FROM pg_catalog.pg_class AS snapshot_probe_%d", i)); err != nil {
			t.Fatalf("seed distinct statement %d: %v", i, err)
		}
	}
	var dealloc int64
	if err := pool.QueryRow(ctx, "SELECT dealloc FROM pg_stat_statements_info").Scan(&dealloc); err != nil {
		t.Fatal(err)
	}
	if dealloc == 0 {
		t.Fatal("fixture did not evict statement entries")
	}
	if _, err := meter.EndSnapshot(ctx, reset); err == nil || !strings.Contains(err.Error(), "deallocation") {
		t.Fatalf("EndSnapshot error = %v, want named deallocation failure", err)
	}
}
