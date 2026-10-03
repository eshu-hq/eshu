// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestFleetSharedFailuresDoNotRetryAnotherMember(t *testing.T) {
	for _, err := range []error{
		&pgconn.PgError{Code: "28P01"},
		x509.HostnameError{Host: "wrong-reader"},
		x509.UnknownAuthorityError{},
		ErrWrongTopology,
	} {
		if !sharedFleetFailure(err) {
			t.Fatalf("shared error %T was retryable", err)
		}
	}
	if sharedFleetFailure(memberLocalTopology{}) {
		t.Fatal("member-local topology mismatch was treated as global")
	}
}

func TestFleetPingBoundsWriterSaturation(t *testing.T) {
	access := openFleetRegressionAccess(t, 400*time.Millisecond)
	access.pingTimeout = 120 * time.Millisecond
	access.writer.SetMaxOpenConns(1)
	held, err := access.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = access.Ping(ctx)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("writer saturation Ping error=%v, want bounded deadline", err)
	}
	if elapsed >= 300*time.Millisecond {
		t.Fatalf("writer saturation Ping took %s, want within PingTimeout", elapsed)
	}
}

func TestFleetSkipsLaggedReaderWithinOneReplayBudget(t *testing.T) {
	access := openFleetRegressionAccess(t, 450*time.Millisecond)
	ctx := t.Context()
	if _, err := access.readerMembers[0].pool.ExecContext(ctx, "SELECT pg_wal_replay_pause()"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := access.readerMembers[0].pool.ExecContext(context.Background(), "SELECT pg_wal_replay_resume()"); err != nil {
			t.Error(err)
		}
	})
	if _, err := access.writer.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS eshu_fleet_replay_proof (value integer)"); err != nil {
		t.Fatal(err)
	}
	if _, err := access.writer.ExecContext(ctx, "INSERT INTO eshu_fleet_replay_proof VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	checkpointCtx, err := access.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	access.nextReader.Store(0)
	started := time.Now()
	set, err := access.Reader().(db.ReadSnapshotSetBeginner).BeginReadOnlySnapshotSet(checkpointCtx, 4)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if elapsed >= 400*time.Millisecond {
		t.Fatalf("lagged A delayed healthy B for %s", elapsed)
	}
	if got := access.readerMembers[1].pool.Stats().InUse; got != 4 {
		t.Fatalf("healthy B holds %d snapshot connections, want 4", got)
	}
}
