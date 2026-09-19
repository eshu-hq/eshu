// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"os"
	"testing"
	"time"
)

// TestOpenDisposableDatabaseLiveMarksConnectionsAsInfraWriters pins that the
// disposable proof database connects the way the service runtimes do, with
// the derive-aware writer setting, so a proof that seeds infra-typed content
// rows cannot leave rolling-upgrade fence marks behind. Set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN (an administrative postgres-database
// DSN) and ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it.
func TestOpenDisposableDatabaseLiveMarksConnectionsAsInfraWriters(t *testing.T) {
	ctx, db := OpenDisposableDatabase(t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		time.Minute)
	var setting string
	if err := db.QueryRowContext(ctx,
		`SELECT coalesce(current_setting('eshu.infra_inventory_writer', true), '')`).Scan(&setting); err != nil {
		t.Fatalf("read session setting: %v", err)
	}
	if setting != "derive" {
		t.Fatalf("eshu.infra_inventory_writer = %q, want derive on disposable proof connections", setting)
	}
}
