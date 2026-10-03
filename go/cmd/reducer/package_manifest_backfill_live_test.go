// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func TestPackageManifestBackfillOnlyOneCandidateOwnsPass(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 30*time.Second)
	leader, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open leader connection: %v", err)
	}
	defer leader.Close()
	var locked bool
	if err := leader.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1, $2)", packageManifestBackfillAdvisoryLockClass, packageManifestBackfillAdvisoryLockID).Scan(&locked); err != nil {
		t.Fatalf("acquire leader lock: %v", err)
	}
	if !locked {
		t.Fatal("first candidate did not acquire advisory lock")
	}
	holding := true
	defer func() {
		if holding {
			_ = leader.QueryRowContext(context.Background(), "SELECT pg_advisory_unlock($1, $2)", packageManifestBackfillAdvisoryLockClass, packageManifestBackfillAdvisoryLockID).Scan(&locked)
		}
	}()

	calls := 0
	ran, err := withPackageManifestBackfillLeadership(ctx, db, func(context.Context, *sql.Conn) error {
		calls++
		return nil
	})
	if err != nil || ran || calls != 0 {
		t.Fatalf("contended candidate = (ran=%t, calls=%d, err=%v), want skipped", ran, calls, err)
	}
	if err := leader.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1, $2)", packageManifestBackfillAdvisoryLockClass, packageManifestBackfillAdvisoryLockID).Scan(&locked); err != nil || !locked {
		t.Fatalf("release leader lock = (%t, %v), want true", locked, err)
	}
	holding = false
	ran, err = withPackageManifestBackfillLeadership(ctx, db, func(context.Context, *sql.Conn) error {
		calls++
		return nil
	})
	if err != nil || !ran || calls != 1 {
		t.Fatalf("takeover candidate = (ran=%t, calls=%d, err=%v), want one pass", ran, calls, err)
	}

	crashed, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open crashed owner connection: %v", err)
	}
	if err := crashed.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1, $2)",
		packageManifestBackfillAdvisoryLockClass, packageManifestBackfillAdvisoryLockID).Scan(&locked); err != nil || !locked {
		t.Fatalf("acquire crashed owner lock = (%t, %v), want true", locked, err)
	}
	_ = crashed.Raw(func(any) error { return driver.ErrBadConn })
	_ = crashed.Close()
	ran, err = withPackageManifestBackfillLeadership(ctx, db, func(context.Context, *sql.Conn) error {
		calls++
		return nil
	})
	if err != nil || !ran || calls != 2 {
		t.Fatalf("crashed-owner takeover = (ran=%t, calls=%d, err=%v), want another pass", ran, calls, err)
	}
}

func TestPackageManifestBackfillDoesNotStarveSingleConnectionPool(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	baseCtx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 30*time.Second)
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(baseCtx, 2*time.Second)
	defer cancel()

	ran, err := withPackageManifestBackfillLeadership(ctx, db, func(passCtx context.Context, conn *sql.Conn) error {
		rows, queryErr := conn.QueryContext(passCtx, "SELECT 1")
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		var got int
		if scanErr := rows.Scan(&got); scanErr != nil {
			return scanErr
		}
		if got != 1 {
			t.Errorf("query result = %d, want 1", got)
		}
		return rows.Err()
	})
	if err != nil || !ran {
		t.Fatalf("single-connection backfill = (ran=%t, err=%v), want successful pass", ran, err)
	}
}
