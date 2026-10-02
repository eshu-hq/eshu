// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestSnapshotReservationCapacityMarkerExcludesDialAndCancellation(t *testing.T) {
	dialErr := errors.New("seeded reader dial failure")
	pool := sql.OpenDB(metadataConnector{cause: dialErr})
	defer pool.Close()
	pool.SetMaxOpenConns(4)
	access := &Access{
		reader:          pool,
		replayTimeout:   30 * time.Millisecond,
		snapshotSetGate: make(chan struct{}, 1),
		readerPermits:   make(chan struct{}, 4),
	}
	access.snapshotSetGate <- struct{}{}
	checkpointCtx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{owner: access})
	reader := fencedQueryer{access: access}

	_, err := reader.BeginReadOnlySnapshotSet(checkpointCtx, 4)
	if !errors.Is(err, db.ErrSnapshotReservationCapacity) {
		t.Fatalf("permit timeout classification = %v", err)
	}
	if got := len(access.readerPermits); got != 0 {
		t.Fatalf("permit timeout changed available tokens: %d", got)
	}

	for range 4 {
		access.readerPermits <- struct{}{}
	}
	_, err = reader.BeginReadOnlySnapshotSet(checkpointCtx, 4)
	if !errors.Is(err, dialErr) || errors.Is(err, db.ErrSnapshotReservationCapacity) {
		t.Fatalf("dial failure classification = %v", err)
	}
	if got := len(access.readerPermits); got != 4 {
		t.Fatalf("dial failure leaked permit: available=%d", got)
	}

	for range 4 {
		<-access.readerPermits
	}
	requestCtx, cancel := context.WithTimeout(checkpointCtx, 5*time.Millisecond)
	defer cancel()
	_, err = reader.BeginReadOnlySnapshotSet(requestCtx, 4)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, db.ErrSnapshotReservationCapacity) {
		t.Fatalf("request cancellation classification = %v", err)
	}

	_, err = reader.BeginReadOnlySnapshotSet(t.Context(), 4)
	if !errors.Is(err, ErrMissingCheckpoint) || errors.Is(err, db.ErrSnapshotReservationCapacity) {
		t.Fatalf("missing checkpoint classification = %v", err)
	}
}

func TestReaderConnectionPermitReturnedOnceAcrossCancelAndClose(t *testing.T) {
	pool := sql.OpenDB(terminalErrorConnector{})
	defer pool.Close()
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	permits := make(chan struct{}, 1)
	lease := &readerConnection{Conn: conn, permits: permits}
	tx, err := lease.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	reader := newReadTransaction(ctx, tx, lease, &Access{})
	cancel()
	var closers sync.WaitGroup
	for range 8 {
		closers.Add(1)
		go func() {
			defer closers.Done()
			_ = reader.Rollback()
			_ = lease.Close()
		}()
	}
	closers.Wait()
	if got := len(permits); got != 1 {
		t.Fatalf("permit returned %d times, want once", got)
	}
	if got := pool.Stats().InUse; got != 0 {
		t.Fatalf("closed reader holds %d connections", got)
	}
}

func TestAccessPingSharesReaderPermits(t *testing.T) {
	pool := sql.OpenDB(terminalErrorConnector{})
	defer pool.Close()
	pool.SetMaxOpenConns(1)
	access := &Access{writer: pool, reader: pool, readerPermits: make(chan struct{}, 1)}
	waitCtx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := access.Ping(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ping bypassed held reader permit: %v", err)
	}
	access.readerPermits <- struct{}{}
	if err := access.Ping(t.Context()); err != nil {
		t.Fatalf("ping after permit release: %v", err)
	}
	if got := len(access.readerPermits); got != 1 {
		t.Fatalf("ping leaked reader permit: available=%d", got)
	}
}
