// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func TestPackageManifestBackfillWaitsForCancellationAndDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wait := startPackageManifestConsumptionKeyBackfill(ctx, func(runCtx context.Context) error {
		close(started)
		<-runCtx.Done()
		<-release
		close(finished)
		return runCtx.Err()
	}, logger)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("backfill did not start")
	}
	cancel()
	waited := make(chan struct{})
	go func() {
		wait()
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("cleanup returned before in-flight backfill stopped")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not wait for backfill to stop")
	}
	select {
	case <-finished:
	default:
		t.Fatal("backfill still running after cleanup")
	}
}

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

func TestPackageManifestBackfillPollInterval(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ran   bool
		ready bool
		err   error
		want  time.Duration
	}{
		{name: "initial backfill", ran: true, ready: false, want: time.Second},
		{name: "ready repair", ran: true, ready: true, want: 30 * time.Second},
		{name: "contended replica", ran: false, want: 30 * time.Second},
		{name: "failed pass", ran: true, err: context.DeadlineExceeded, want: 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := packageManifestBackfillPollInterval(tc.ran, tc.ready, tc.err); got != tc.want {
				t.Fatalf("poll interval = %v, want %v", got, tc.want)
			}
		})
	}
}
