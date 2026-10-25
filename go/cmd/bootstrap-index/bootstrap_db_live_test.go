// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
)

// TestOpenBootstrapDBMarksEveryConnectionDeferredLive proves the pool
// bootstrap-index writes content through carries the bulk-load session setting
// (#7125): a connection that lacks it would keep the write-time derivation on
// and pay the trigger cost this design removes. Set ESHU_POSTGRES_DSN to a
// disposable Postgres to run it.
func TestOpenBootstrapDBMarksEveryConnectionDeferredLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set; skipping the bootstrap postgres session test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	database, err := openBootstrapDB(ctx, func(key string) string {
		if key == "ESHU_POSTGRES_DSN" {
			return dsn
		}
		return ""
	})
	if err != nil {
		t.Fatalf("openBootstrapDB: %v", err)
	}
	defer func() { _ = database.Close() }()

	rows, err := database.QueryContext(ctx, `SELECT current_setting('eshu.secret_lines_derive', true)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var setting string
	if !rows.Next() || rows.Scan(&setting) != nil {
		t.Fatal("no session setting row")
	}
	if setting != "deferred" {
		t.Fatalf("bootstrap-index connection eshu.secret_lines_derive = %q, want deferred", setting)
	}
}

func liveGetenv(dsn string, extra map[string]string) func(string) string {
	return func(key string) string {
		if key == "ESHU_POSTGRES_DSN" {
			return dsn
		}
		return extra[key]
	}
}

// TestOpenBootstrapDBHoldsTheDeferredWriterLockSharedLive: every connection of
// the bootstrap pool holds (5318,2) shared from birth (#8833 ruling-4), and a
// pool opened while another session holds it exclusive (a reducer rebuild)
// waits for it and, bounded by the startup ping, fails with a ping error
// instead of writing. Once the holder ends, the pool opens.
func TestOpenBootstrapDBHoldsTheDeferredWriterLockSharedLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set; skipping the bootstrap postgres session test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	holder, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	conn, err := holder.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(5318, 2)`); err != nil {
		t.Fatal(err)
	}
	_, err = openBootstrapDB(ctx, liveGetenv(dsn, map[string]string{"ESHU_POSTGRES_PING_TIMEOUT": "1s"}))
	if err == nil || !strings.Contains(err.Error(), "ping postgres") {
		t.Fatalf("openBootstrapDB while (5318,2) is held exclusive = %v, want a ping error (the first connection waits on the lock)", err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(5318, 2)`); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	database, err := openBootstrapDB(ctx, liveGetenv(dsn, nil))
	if err != nil {
		t.Fatalf("openBootstrapDB after the holder released: %v", err)
	}
	defer func() { _ = database.Close() }()
	rows, err := database.QueryContext(ctx, `
SELECT count(*) FROM pg_locks
WHERE locktype = 'advisory' AND classid = 5318 AND objid = 2 AND mode = 'ShareLock' AND granted
  AND pid IN (SELECT pid FROM pg_stat_activity WHERE datname = current_database())`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var shared int
	if !rows.Next() || rows.Scan(&shared) != nil || shared < 1 {
		t.Fatalf("shared (5318,2) holders = %d, want the pool's connection", shared)
	}
}
