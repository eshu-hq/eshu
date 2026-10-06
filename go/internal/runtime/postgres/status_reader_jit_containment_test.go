// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// statusJITProbeSQL reads the effective jit setting (the value SHOW jit
// prints) and names the backend that answered, so a later probe can prove it
// reused the connection the status transaction ran on.
const statusJITProbeSQL = `SELECT current_setting('jit'),
	pg_backend_pid()::text || '@' || coalesce(host(inet_server_addr()), 'local') || ':' || coalesce(inet_server_port()::text, '')`

func probeStatusJIT(ctx context.Context, q db.Queryer) (setting, backend string, err error) {
	rows, err := q.QueryContext(ctx, statusJITProbeSQL)
	if err != nil {
		return "", "", err
	}
	if !rows.Next() {
		return "", "", errors.Join(errors.New("jit probe returned no row"), rows.Err(), rows.Close())
	}
	if err := rows.Scan(&setting, &backend); err != nil {
		return "", "", errors.Join(err, rows.Close())
	}
	return setting, backend, errors.Join(rows.Err(), rows.Close())
}

// assertStatusSnapshotJITContained proves, on a live server, that the status
// transaction runs with jit=off and that the setting ends with the
// transaction on both Commit and Rollback: the next statement on the same
// backend reports the server default again. reuseAttempts bounds how many
// pool probes may run before the status backend is reached.
func assertStatusSnapshotJITContained(t *testing.T, access *Access, reuseAttempts int) {
	t.Helper()
	ctx, err := access.ContextWithCheckpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	store := access.Reader()
	// The probe can answer either way only if the server default is on.
	if setting, _, err := probeStatusJIT(ctx, store); err != nil || setting != "on" {
		t.Fatalf("fixture jit default=%q err=%v; the containment probe needs jit=on", setting, err)
	}
	// A guarded snapshot that is not a status read keeps the server default.
	plain, err := store.BeginReadOnlySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	setting, _, err := probeStatusJIT(ctx, plain)
	if rollbackErr := plain.Rollback(); err != nil || rollbackErr != nil || setting != "on" {
		t.Fatalf("plain snapshot jit=%q err=%v rollback=%v; only the status read may disable JIT", setting, err, rollbackErr)
	}
	probeErr := errors.New("seeded status read failure")
	for _, fail := range []bool{false, true} {
		var inside, backend string
		reader := NewSnapshotStatusReader(store, func(q db.Queryer) status.Reader {
			return snapshotReaderStub{read: func(ctx context.Context, _ status.SnapshotSelection) (status.RawSnapshot, error) {
				var err error
				inside, backend, err = probeStatusJIT(ctx, q)
				if err == nil && fail {
					err = probeErr
				}
				return status.RawSnapshot{}, err
			}}
		}, nil)
		_, err := reader.ReadStatusSnapshot(ctx, time.Now())
		if fail && !errors.Is(err, probeErr) || !fail && err != nil {
			t.Fatalf("rollback=%v status read error=%v", fail, err)
		}
		if inside != "off" || backend == "" {
			t.Fatalf("rollback=%v jit inside status transaction=%q backend=%q, want off", fail, inside, backend)
		}
		reused := false
		for attempt := 0; attempt < reuseAttempts && !reused; attempt++ {
			after, afterBackend, err := probeStatusJIT(ctx, store)
			if err != nil {
				t.Fatal(err)
			}
			if after != "on" {
				t.Fatalf("rollback=%v backend %s kept jit=%q after the status transaction ended", fail, afterBackend, after)
			}
			reused = afterBackend == backend
		}
		if !reused {
			t.Fatalf("rollback=%v no probe reached status backend %s in %d attempts; containment unproven", fail, backend, reuseAttempts)
		}
	}
}

// TestStatusSnapshotJITOffIsTransactionScoped runs on an owned disposable
// primary through a one-connection guarded reader, so the post-transaction
// probe must reuse the status backend.
func TestStatusSnapshotJITOffIsTransactionScoped(t *testing.T) {
	assertStatusSnapshotJITContained(t, testAccess(t, "", 1), 1)
}

// TestStatusSnapshotJITOffIsTransactionScopedOnHotStandby repeats the proof
// on a physical streaming standby: SET LOCAL is allowed during recovery.
func TestStatusSnapshotJITOffIsTransactionScopedOnHotStandby(t *testing.T) {
	standby := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if standby == "" {
		t.Skip("owned physical standby not configured")
	}
	assertStatusSnapshotJITContained(t, testAccess(t, standby, 1), 1)
}

// TestStatusSnapshotJITOffIsTransactionScopedOnReaderFleet repeats the proof
// on the direct-member reader fleet, whose transactions end through finish on
// both exits and return the connection to the member pool.
func TestStatusSnapshotJITOffIsTransactionScopedOnReaderFleet(t *testing.T) {
	assertStatusSnapshotJITContained(t, openFleetAccess(t, 2*time.Second, 8), 64)
}
