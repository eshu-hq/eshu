// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestOpenPostgresLiveMarksConnectionsAsInfraInventoryWriters proves every
// service runtime pool connection carries the derive-aware session setting,
// so migration 109's rolling-upgrade fence skips this binary's content
// writes. Set ESHU_POSTGRES_DSN to run it.
func TestOpenPostgresLiveMarksConnectionsAsInfraInventoryWriters(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the Postgres writer-session proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := OpenPostgres(ctx, func(key string) string {
		if key == "ESHU_POSTGRES_DSN" {
			return dsn
		}
		return ""
	})
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	defer func() { _ = db.Close() }()
	var setting string
	if err := db.QueryRowContext(ctx,
		`SELECT coalesce(current_setting('eshu.infra_inventory_writer', true), '')`).Scan(&setting); err != nil {
		t.Fatalf("read session setting: %v", err)
	}
	if setting != "derive" {
		t.Fatalf("eshu.infra_inventory_writer = %q, want derive on every runtime connection", setting)
	}
}
