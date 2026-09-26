// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func captureLogger() (*bytes.Buffer, *slog.Logger) {
	var buf bytes.Buffer
	return &buf, slog.New(slog.NewTextHandler(&buf, nil))
}

func acquireOrFatal(ctx context.Context, t *testing.T, pool *sql.DB, options LockOptions) *BulkLoadLock {
	t.Helper()
	lock, err := AcquireBulkLoadLock(ctx, pool, options)
	if err != nil {
		t.Fatalf("AcquireBulkLoadLock: %v", err)
	}
	return lock
}

// TestBulkLoadLockRefusesASecondLoadLive proves the run lock is exclusive across
// database sessions: a second acquirer waits its bound, fails with an error that
// names the holder's backend and the subject, logs the wait with lock=bulk_load,
// and gets the lock at its first try once the holder releases.
func TestBulkLoadLockRefusesASecondLoadLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	second := openDeferredPool(t, config)

	first := acquireOrFatal(ctx, t, db, LockOptions{})
	waitLogs, waitLogger := captureLogger()
	_, err := AcquireBulkLoadLock(ctx, second, LockOptions{
		Wait: 2 * time.Second, Poll: 200 * time.Millisecond, LogEvery: 200 * time.Millisecond, Logger: waitLogger,
	})
	if err == nil {
		t.Fatal("a second AcquireBulkLoadLock succeeded while the first was held")
	}
	if want := fmt.Sprintf("pid=%d", first.pid); !strings.Contains(err.Error(), want) ||
		!strings.Contains(err.Error(), "secret lines bulk load") {
		t.Fatalf("refusal = %v, want it to name %s and the subject", err, want)
	}
	for _, want := range []string{"bootstrap.postgres.ownership.waiting", "lock=bulk_load"} {
		if !strings.Contains(waitLogs.String(), want) {
			t.Fatalf("waiting logs missing %q:\n%s", want, waitLogs)
		}
	}

	if err := first.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	acquiredLogs, acquiredLogger := captureLogger()
	next := acquireOrFatal(ctx, t, second, LockOptions{Wait: 2 * time.Second, Logger: acquiredLogger})
	if !strings.Contains(acquiredLogs.String(), "polls=0") ||
		!strings.Contains(acquiredLogs.String(), "secret_lines.bulk_load_lock_acquired") {
		t.Fatalf("acquire after release should succeed at the first try, logs:\n%s", acquiredLogs)
	}
	if err := next.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

// TestBulkLoadLockDiesWithItsSessionLive proves a killed holder does not leave
// the lock behind: terminating its backend (what a pod kill does) lets the next
// acquirer in, and the dead holder's Release reports that exclusivity was void
// instead of returning nil.
func TestBulkLoadLockDiesWithItsSessionLive(t *testing.T) {
	ctx, db, config := openDatabase(t)
	second := openDeferredPool(t, config)

	first := acquireOrFatal(ctx, t, second, LockOptions{})
	if _, err := db.ExecContext(ctx, `SELECT pg_terminate_backend($1)`, first.pid); err != nil {
		t.Fatalf("terminate the holder's backend: %v", err)
	}
	next := acquireOrFatal(ctx, t, db, LockOptions{Wait: 5 * time.Second, Poll: 100 * time.Millisecond})
	if err := first.Release(ctx); err == nil {
		t.Fatal("Release of a lock whose session was terminated returned nil, want an error")
	}
	if err := next.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

// TestBulkLoadLockKeepsReadyHonestLive is the P2-D1 proof, in two subtests so
// neither can pass vacuously. Without the lock, two overlapping loads publish
// ready over rows the older load still writes without derivation (the probe can
// differ). With the lock, the second load is refused until the first finalized,
// and the table equals the derivation after each load.
func TestBulkLoadLockKeepsReadyHonestLive(t *testing.T) {
	t.Run("without_lock", func(t *testing.T) {
		ctx, db, config := openDatabase(t)
		sqlDB := postgres.SQLDB{DB: db}
		poolB := openDeferredPool(t, config)
		poolA := openDeferredPool(t, config)

		epochB, err := BeginDeferral(ctx, sqlDB)
		if err != nil {
			t.Fatal(err)
		}
		writeRecords(ctx, t, poolB, "repo-b", corpus("repo-b", 8))
		epochA, err := BeginDeferral(ctx, sqlDB)
		if err != nil {
			t.Fatal(err)
		}
		writeRecords(ctx, t, poolA, "repo-a", corpus("repo-a", 8))
		result, err := Finalize(ctx, sqlDB, epochA, Options{})
		if err != nil || !result.Published {
			t.Fatalf("Finalize(A) = %+v, %v, want published", result, err)
		}
		writeLate(ctx, t, poolB)
		if ready, err := Ready(ctx, db); err != nil || !ready {
			t.Fatalf("Ready() = %v, %v, want true (the hazard: ready while B still writes)", ready, err)
		}
		if missing, _ := parityCounts(ctx, t, db); missing == 0 {
			t.Fatal("overlapping loads left no missing findings; the probe cannot differ")
		}
		if again, err := Finalize(ctx, sqlDB, epochB, Options{}); err != nil || again.Claimed {
			t.Fatalf("Finalize(B) = %+v, %v, want unclaimed", again, err)
		}
	})

	t.Run("with_lock", func(t *testing.T) {
		ctx, db, config := openDatabase(t)
		sqlDB := postgres.SQLDB{DB: db}
		poolB := openDeferredPool(t, config)
		poolA := openDeferredPool(t, config)
		lockPoolA := openDeferredPool(t, config)

		lockB := acquireOrFatal(ctx, t, db, LockOptions{})
		epochB, err := BeginDeferral(ctx, sqlDB)
		if err != nil {
			t.Fatal(err)
		}
		writeRecords(ctx, t, poolB, "repo-b", corpus("repo-b", 8))
		if _, err := AcquireBulkLoadLock(ctx, lockPoolA, LockOptions{
			Wait: 2 * time.Second, Poll: 200 * time.Millisecond,
		}); err == nil {
			t.Fatal("load A acquired the lock while load B was between BeginDeferral and Finalize")
		}
		writeLate(ctx, t, poolB)
		result, err := Finalize(ctx, sqlDB, epochB, Options{})
		if err != nil || !result.Published {
			t.Fatalf("Finalize(B) = %+v, %v, want published", result, err)
		}
		if err := lockB.Release(ctx); err != nil {
			t.Fatal(err)
		}
		if ready, err := Ready(ctx, db); err != nil || !ready {
			t.Fatalf("Ready() = %v, %v, want true", ready, err)
		}
		requireParity(ctx, t, db, "after load B under the lock")

		lockA := acquireOrFatal(ctx, t, lockPoolA, LockOptions{Wait: 2 * time.Second})
		epochA, err := BeginDeferral(ctx, sqlDB)
		if err != nil {
			t.Fatal(err)
		}
		writeRecords(ctx, t, poolA, "repo-a", corpus("repo-a", 8))
		if result, err := Finalize(ctx, sqlDB, epochA, Options{}); err != nil || !result.Published {
			t.Fatalf("Finalize(A) = %+v, %v, want published", result, err)
		}
		if err := lockA.Release(ctx); err != nil {
			t.Fatal(err)
		}
		requireParity(ctx, t, db, "after load A under the lock")
	})
}

// writeLate writes one more repo-b file with a finding through the deferred
// pool, the write a still-running load performs after another load finalized.
func writeLate(ctx context.Context, t *testing.T, pool *sql.DB) {
	t.Helper()
	writeRecords(ctx, t, pool, "repo-b", append(corpus("repo-b", 8),
		secretRecord("repo-b/late_write.go", "package p\npassword = \"hunter99999999\"\n", "go")))
}
