// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

// TestRetentionTimingCloneIsDropped is PE1 of arbiter ruling arb-7127-3d-e:
// the clone cloneTemplate makes is gone once the test that made it ends,
// called the way TestRetentionTimingP8 calls it. It clones an empty template
// of its own, so it needs no fixture.
func TestRetentionTimingCloneIsDropped(t *testing.T) {
	admin := strings.TrimSpace(os.Getenv(timingDSNEnv))
	if admin == "" {
		t.Skipf("set %s to run the retention timing harness", timingDSNEnv)
	}
	adminDB, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer func() { _ = adminDB.Close() }()
	ctx := context.Background()
	template := timingTemplateName("leakcheck")
	_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS `+template+` WITH (FORCE)`)
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+template); err != nil {
		t.Fatalf("create %s: %v", template, err)
	}
	var clone string
	defer func() {
		for _, name := range []string{clone, template} {
			if name != "" {
				_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
			}
		}
	}()
	t.Run("clone", func(t *testing.T) {
		// Open and close the admin pool the way TestRetentionTimingP8 does:
		// its deferred Close runs before the clone's Cleanup, so the drop
		// cannot rely on the caller's pool.
		callerDB, err := sql.Open("pgx", admin)
		if err != nil {
			t.Fatalf("open caller admin: %v", err)
		}
		defer func() { _ = callerDB.Close() }()
		db := cloneTemplate(t, admin, callerDB, "leakcheck")
		if err := db.QueryRowContext(t.Context(), `SELECT current_database()`).Scan(&clone); err != nil {
			t.Fatalf("clone name: %v", err)
		}
	})
	var left int
	if err := adminDB.QueryRowContext(ctx, `SELECT count(*) FROM pg_database WHERE datname = $1`, clone).Scan(&left); err != nil {
		t.Fatalf("count clones: %v", err)
	}
	if clone == "" || left != 0 {
		t.Fatalf("clone %q: %d pg_database rows after the test that made it ended, want 0", clone, left)
	}
}
