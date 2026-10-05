// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type fleetSequenceObserver struct {
	mu        sync.Mutex
	sequences []int64
}

func (*fleetSequenceObserver) Observe(string, Stage, Outcome, time.Duration) {}

func (*fleetSequenceObserver) recordsReaderQueryStart(context.Context) bool { return true }

func (o *fleetSequenceObserver) recordReaderQueryStart(_ context.Context, sequence int64, _ readerBackendIdentity) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sequences = append(o.sequences, sequence)
}

func TestFleetQuerySequencesAreAccessWideAcrossConcurrentTransactions(t *testing.T) {
	const workers = 8
	pool := sql.OpenDB(snapshotEventConnector{spy: &snapshotEventSpy{}})
	pool.SetMaxOpenConns(workers)
	t.Cleanup(func() { _ = pool.Close() })
	observer := &fleetSequenceObserver{}
	access := &Access{observer: observer}
	var group sync.WaitGroup
	results := make(chan error, workers)
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			conn, err := pool.Conn(t.Context())
			if err != nil {
				results <- err
				return
			}
			lease := &readerConnection{Conn: conn, member: &physicalReaderMember{}}
			tx, err := beginReadTransactionOwned(t.Context(), lease, access)
			if err != nil {
				_ = lease.Close()
				results <- err
				return
			}
			var snapshot string
			err = tx.QueryRowContext(t.Context(), "SELECT pg_export_snapshot()").Scan(&snapshot)
			results <- errors.Join(err, tx.Rollback())
		}()
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.sequences) != workers || access.querySequence.Load() != workers {
		t.Fatalf("events=%d sequence=%d, want %d", len(observer.sequences), access.querySequence.Load(), workers)
	}
	seen := make(map[int64]bool, workers)
	for _, sequence := range observer.sequences {
		if sequence < 1 || sequence > workers || seen[sequence] {
			t.Fatalf("duplicate or out-of-range sequence %d in %v", sequence, observer.sequences)
		}
		seen[sequence] = true
	}
	if pool.Stats().InUse != 0 {
		t.Fatalf("fleet transactions retained %d connections", pool.Stats().InUse)
	}
}

func TestFleetSnapshotCancellationBeforeBusinessEmitsNoStart(t *testing.T) {
	spy := &snapshotEventSpy{}
	pool := sql.OpenDB(fleetEventConnector{spy: spy})
	pool.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = pool.Close() })
	allocator := newReaderAllocator([]int{2}, 2)
	access := &Access{
		observer: spy, replayTimeout: time.Second,
		lineage:       newWriterLineage(physicalIdentity{systemID: "7", database: "eshu"}, lineageObservation{}, nil),
		readerMembers: []physicalReaderMember{{pool: pool, incarnation: "123", addresses: []net.IP{net.ParseIP("127.0.0.1")}}},
		allocator:     allocator,
	}
	ownerCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reservation, err := allocator.reserve(ownerCtx, []int{0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	point := checkpoint{lsn: "0/1", systemID: "7", database: "eshu"}
	set, err := access.beginSnapshotSetReserved(ownerCtx, ownerCtx, 2, reservation, point)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.After(time.Second)
	for {
		reserved, _ := allocator.pressure()
		if pool.Stats().InUse == 0 && reserved[0] == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("canceled set retained in_use=%d reserved=%d", pool.Stats().InUse, reserved[0])
		case <-time.After(time.Millisecond):
		}
	}
	if spy.starts != 0 || access.querySequence.Load() != 0 {
		t.Fatalf("canceled setup emitted %d starts, sequence=%d", spy.starts, access.querySequence.Load())
	}
	_ = set.Close()
}

func TestFleetFailedFenceEmitsNoBusinessStart(t *testing.T) {
	spy := &snapshotEventSpy{}
	pool := sql.OpenDB(fleetEventConnector{spy: spy})
	t.Cleanup(func() { _ = pool.Close() })
	allocator := newReaderAllocator([]int{1}, 1)
	access := &Access{
		observer: spy, replayTimeout: time.Second,
		lineage:       newWriterLineage(physicalIdentity{systemID: "7", database: "eshu"}, lineageObservation{}, nil),
		readerMembers: []physicalReaderMember{{pool: pool, incarnation: "123", addresses: []net.IP{net.ParseIP("127.0.0.1")}}},
		allocator:     allocator,
	}
	reservation, err := allocator.reserve(t.Context(), []int{0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	point := checkpoint{lsn: "0/1", systemID: "8", database: "eshu"}
	conn, err := access.borrowReserved(t.Context(), point, reservation)
	if conn != nil || !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("failed fence returned conn=%v err=%v", conn, err)
	}
	reserved, _ := allocator.pressure()
	if spy.starts != 0 || access.querySequence.Load() != 0 || pool.Stats().InUse != 0 || reserved[0] != 0 {
		t.Fatalf("failed fence starts=%d sequence=%d in_use=%d reserved=%d", spy.starts, access.querySequence.Load(), pool.Stats().InUse, reserved[0])
	}
}

func TestFleetNonRecordingTransactionKeepsLeaseHealthy(t *testing.T) {
	pool := sql.OpenDB(snapshotEventConnector{spy: &snapshotEventSpy{}})
	t.Cleanup(func() { _ = pool.Close() })
	spans := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer traces.Shutdown(context.Background())
	observer, err := NewObserver(sdkmetric.NewMeterProvider().Meter("test"), traces.Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	access := &Access{observer: observer}
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	lease := &readerConnection{Conn: conn, member: &physicalReaderMember{}}
	tx, err := beginReadTransactionOwned(t.Context(), lease, access)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	var snapshot string
	if err := tx.QueryRowContext(t.Context(), "SELECT pg_export_snapshot()").Scan(&snapshot); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, span := range spans.Ended() {
		for _, event := range span.Events() {
			if event.Name == readerQueryStartEventName {
				t.Fatal("non-recording transaction emitted a query-start event")
			}
		}
	}
	if snapshot == "" || access.querySequence.Load() != 0 || pool.Stats().InUse != 0 {
		t.Fatalf("non-recording snapshot=%t sequence=%d in_use=%d", snapshot != "", access.querySequence.Load(), pool.Stats().InUse)
	}
}
