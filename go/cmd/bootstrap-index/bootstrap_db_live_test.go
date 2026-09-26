// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
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
