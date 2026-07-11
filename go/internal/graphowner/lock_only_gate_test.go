// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestLockOnlyGateNilGateWritesThrough proves a nil LockOnlyGate preserves
// prior (ungated) behavior, mirroring TestNilGateWritesThrough for Gate.
func TestLockOnlyGateNilGateWritesThrough(t *testing.T) {
	t.Parallel()

	var got []map[string]any
	underlying := func(_ context.Context, rows []map[string]any, _, _, _ string) error {
		got = rows
		return nil
	}
	rows := []map[string]any{{"uid": "a"}, {"uid": "b"}}

	var gate *LockOnlyGate
	w := NewRDSPostureLockedWriter(gate, underlying, nil)
	if err := w.WriteRDSPostureNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("WriteRDSPostureNodes error = %v", err)
	}
	if !reflect.DeepEqual(got, rows) {
		t.Fatalf("pass-through nil gate altered rows: got %v", got)
	}
}

// TestLockOnlyGateNilDBWritesThrough proves a LockOnlyGate with no db wired
// (no Postgres beginner) writes through unchanged, matching Gate's
// no-ledger-wired pass-through path.
func TestLockOnlyGateNilDBWritesThrough(t *testing.T) {
	t.Parallel()

	called := false
	underlying := func(_ context.Context, rows []map[string]any, _, _, _ string) error {
		called = true
		return nil
	}
	gate := NewLockOnlyGate(nil)
	w := NewEC2InternetExposureLockedWriter(gate, underlying, nil)
	if err := w.WriteEC2InternetExposureNodes(context.Background(), []map[string]any{{"uid": "a"}}, "scope-1", "gen-1", "reducer/ec2-internet-exposure"); err != nil {
		t.Fatalf("WriteEC2InternetExposureNodes error = %v", err)
	}
	if !called {
		t.Fatal("underlying was not called by the pass-through (no-db) lock-only gate")
	}
}

// TestLockOnlyGateEmptyRowsWritesThroughNoTx proves an empty batch never opens
// a transaction, matching Gate's empty-batch no-op.
func TestLockOnlyGateEmptyRowsWritesThroughNoTx(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	gate := &LockOnlyGate{db: beginner, store: &fakeLockOnlyStore{}}
	called := false
	underlying := func(_ context.Context, rows []map[string]any, _, _, _ string) error {
		called = true
		if len(rows) != 0 {
			t.Fatalf("underlying called with %d rows, want 0", len(rows))
		}
		return nil
	}
	w := NewS3InternetExposureLockedWriter(gate, underlying, nil)
	if err := w.WriteS3InternetExposureNodes(context.Background(), nil, "scope-1", "gen-1", "reducer/s3-internet-exposure"); err != nil {
		t.Fatalf("WriteS3InternetExposureNodes error = %v", err)
	}
	if !called {
		t.Fatal("underlying was not called for the empty-batch no-op")
	}
	if beginner.calls() != 0 {
		t.Fatalf("empty batch opened %d transactions, want 0", beginner.calls())
	}
}

// fakeLockOnlyStore records every LockUIDs call's uid set (order-independent)
// for assertion, and always succeeds.
type fakeLockOnlyStore struct {
	mu    sync.Mutex
	calls [][]string
	err   error
}

func (f *fakeLockOnlyStore) LockUIDs(_ context.Context, _ postgres.ExecQueryer, uids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), uids...))
	return f.err
}

// TestLockOnlyGateLocksUIDsBeforeUnderlyingWrite proves the write path locks
// every row's uid, THEN calls underlying with the full chunk, THEN commits —
// the ordering that makes the lock a real critical section around the graph
// write (not a no-op formality).
func TestLockOnlyGateLocksUIDsBeforeUnderlyingWrite(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeLockOnlyStore{}
	gate := &LockOnlyGate{db: beginner, store: store}

	var order []string
	underlying := func(_ context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error {
		order = append(order, "write")
		if scopeID != "scope-1" || generationID != "gen-1" || evidenceSource != "reducer/rds-posture" {
			t.Fatalf("underlying metadata = (%q,%q,%q), want (scope-1,gen-1,reducer/rds-posture)", scopeID, generationID, evidenceSource)
		}
		return nil
	}
	// Wrap store.LockUIDs to also record ordering.
	orderedStore := &orderRecordingLockOnlyStore{inner: store, order: &order}
	gate.store = orderedStore

	rows := []map[string]any{{"uid": "a"}, {"uid": "b"}, {"uid": "c"}}
	w := NewRDSPostureLockedWriter(gate, underlying, nil)
	if err := w.WriteRDSPostureNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("WriteRDSPostureNodes error = %v", err)
	}

	if len(order) != 2 || order[0] != "lock" || order[1] != "write" {
		t.Fatalf("call order = %v, want [lock write]", order)
	}
	if len(store.calls) != 1 || !reflect.DeepEqual(store.calls[0], []string{"a", "b", "c"}) {
		t.Fatalf("LockUIDs calls = %v, want one call with [a b c]", store.calls)
	}
	if beginner.calls() != 1 {
		t.Fatalf("tx count = %d, want 1", beginner.calls())
	}
	if len(beginner.txs) != 1 || !beginner.txs[0].committed || beginner.txs[0].rolledBack {
		t.Fatalf("tx state = %+v, want committed and not rolled back", beginner.txs)
	}
}

type orderRecordingLockOnlyStore struct {
	inner *fakeLockOnlyStore
	order *[]string
}

func (o *orderRecordingLockOnlyStore) LockUIDs(ctx context.Context, tx postgres.ExecQueryer, uids []string) error {
	*o.order = append(*o.order, "lock")
	return o.inner.LockUIDs(ctx, tx, uids)
}

// TestLockOnlyGateUnderlyingErrorRollsBackAndPropagates proves an underlying
// write failure rolls back the lock transaction (never commits a lock over a
// failed write) and returns the error to the caller.
func TestLockOnlyGateUnderlyingErrorRollsBackAndPropagates(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	gate := &LockOnlyGate{db: beginner, store: &fakeLockOnlyStore{}}
	wantErr := errors.New("graph write failed")
	underlying := func(_ context.Context, _ []map[string]any, _, _, _ string) error {
		return wantErr
	}
	w := NewRDSPostureLockedWriter(gate, underlying, nil)
	err := w.WriteRDSPostureNodes(context.Background(), []map[string]any{{"uid": "a"}}, "scope-1", "gen-1", "reducer/rds-posture")
	if !errors.Is(err, wantErr) {
		t.Fatalf("WriteRDSPostureNodes error = %v, want %v", err, wantErr)
	}
	if len(beginner.txs) != 1 || beginner.txs[0].committed || !beginner.txs[0].rolledBack {
		t.Fatalf("tx state = %+v, want rolled back and NOT committed", beginner.txs)
	}
}

// TestLockOnlyGateLockErrorRollsBackAndSkipsWrite proves a failed lock
// acquisition never reaches the underlying graph write.
func TestLockOnlyGateLockErrorRollsBackAndSkipsWrite(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	wantErr := errors.New("lock failed")
	store := &fakeLockOnlyStore{err: wantErr}
	gate := &LockOnlyGate{db: beginner, store: store}
	called := false
	underlying := func(_ context.Context, _ []map[string]any, _, _, _ string) error {
		called = true
		return nil
	}
	w := NewRDSPostureLockedWriter(gate, underlying, nil)
	err := w.WriteRDSPostureNodes(context.Background(), []map[string]any{{"uid": "a"}}, "scope-1", "gen-1", "reducer/rds-posture")
	if err == nil {
		t.Fatal("WriteRDSPostureNodes error = nil, want lock error")
	}
	if called {
		t.Fatal("underlying was called despite a lock failure")
	}
	if len(beginner.txs) != 1 || beginner.txs[0].committed || !beginner.txs[0].rolledBack {
		t.Fatalf("tx state = %+v, want rolled back and NOT committed", beginner.txs)
	}
}

// TestLockOnlyGateChunksAtLockChunkSize proves the lock-only path chunks
// large row batches at lockChunkSize, mirroring
// TestGateWriteChunksCriticalSectionAtLockChunkSize for Gate: it must never
// lock more than lockChunkSize uids under one transaction, must open exactly
// ceil(rows/lockChunkSize) transactions, and must not drop, duplicate, or
// reorder rows across chunk boundaries.
func TestLockOnlyGateChunksAtLockChunkSize(t *testing.T) {
	t.Parallel()

	const rowCount = 1201
	rows := make([]map[string]any, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		rows = append(rows, map[string]any{"uid": fmt.Sprintf("uid-%05d", i)})
	}

	beginner := &fakeChunkBeginner{}
	store := &fakeLockOnlyStore{}
	gate := &LockOnlyGate{db: beginner, store: store}

	var written []map[string]any
	underlying := func(_ context.Context, chunkRows []map[string]any, _, _, _ string) error {
		written = append(written, chunkRows...)
		return nil
	}
	w := NewRDSPostureLockedWriter(gate, underlying, nil)
	if err := w.WriteRDSPostureNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("WriteRDSPostureNodes error = %v", err)
	}

	wantTx := (rowCount + lockChunkSize - 1) / lockChunkSize
	if beginner.calls() != wantTx {
		t.Fatalf("tx count = %d, want %d (ceil(%d/%d))", beginner.calls(), wantTx, rowCount, lockChunkSize)
	}
	if len(store.calls) != wantTx {
		t.Fatalf("LockUIDs call count = %d, want %d (one per tx)", len(store.calls), wantTx)
	}
	for i, call := range store.calls {
		if len(call) > lockChunkSize {
			t.Fatalf("chunk %d locked %d uids, want <= lockChunkSize (%d)", i, len(call), lockChunkSize)
		}
	}
	if len(written) != rowCount {
		t.Fatalf("underlying wrote %d rows total, want %d — chunking must not drop rows", len(written), rowCount)
	}
	for i, row := range written {
		if row["uid"] != rows[i]["uid"] {
			t.Fatalf("written row %d order changed: got uid %v, want %v", i, row["uid"], rows[i]["uid"])
		}
	}
}

// TestRDSPostureLockedWriterRetractPassesThroughUnwrapped proves Retract is
// forwarded directly to the underlying retract function, NOT gated: retraction
// has no per-row uid list to lock (it targets a scope, not explicit uids), so
// lock-only gating does not apply to it. See the LockOnlyGate doc comment.
func TestRDSPostureLockedWriterRetractPassesThroughUnwrapped(t *testing.T) {
	t.Parallel()

	var gotScopeIDs []string
	var gotGeneration, gotEvidence string
	retract := func(_ context.Context, scopeIDs []string, generationID, evidenceSource string) error {
		gotScopeIDs = scopeIDs
		gotGeneration = generationID
		gotEvidence = evidenceSource
		return nil
	}
	gate := &LockOnlyGate{db: &fakeChunkBeginner{}, store: &fakeLockOnlyStore{}}
	w := NewRDSPostureLockedWriter(gate, nil, retract)
	if err := w.RetractRDSPostureNodes(context.Background(), []string{"scope-1"}, "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("RetractRDSPostureNodes error = %v", err)
	}
	if !reflect.DeepEqual(gotScopeIDs, []string{"scope-1"}) || gotGeneration != "gen-1" || gotEvidence != "reducer/rds-posture" {
		t.Fatalf("retract forwarded (%v,%q,%q), want ([scope-1],gen-1,reducer/rds-posture)", gotScopeIDs, gotGeneration, gotEvidence)
	}
}
