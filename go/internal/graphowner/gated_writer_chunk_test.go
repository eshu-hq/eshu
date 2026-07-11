// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeChunkTx is a no-op postgres.Transaction: fakeChunkStore below resolves
// ownership in-memory and never touches it, so it only needs to satisfy the
// interface and record whether it was committed or rolled back.
type fakeChunkTx struct {
	committed  bool
	rolledBack bool
}

func (f *fakeChunkTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (f *fakeChunkTx) QueryContext(context.Context, string, ...any) (postgres.Rows, error) {
	return nil, nil
}

func (f *fakeChunkTx) Commit() error {
	f.committed = true
	return nil
}

func (f *fakeChunkTx) Rollback() error {
	f.rolledBack = true
	return nil
}

// fakeChunkBeginner counts every Begin call — the P2-1 fix must bound this to
// ceil(len(rows)/lockChunkSize) transactions instead of one unbounded
// transaction for the whole intent.
type fakeChunkBeginner struct {
	mu      sync.Mutex
	txCount int
	txs     []*fakeChunkTx
}

func (f *fakeChunkBeginner) Begin(context.Context) (postgres.Transaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txCount++
	tx := &fakeChunkTx{}
	f.txs = append(f.txs, tx)
	return tx, nil
}

func (f *fakeChunkBeginner) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.txCount
}

// fakeChunkStore records the size of every ResolveOwnedUIDs call — the P2-1
// fix must bound this to lockChunkSize entries per call instead of the whole
// intent's row count — and always grants ownership of every uid it sees (no
// contention). This test proves the CHUNK BOUNDARY the gate enforces, not the
// max-order-key resolution rule itself: that rule is proven independently by
// TestGraphNodeOwnerStoreIntegration's concurrent_converges_to_max case
// against a live Postgres, and is unaffected by chunking because each uid's
// resolution is independent of every other uid's (see the correctness
// invariant documented on Gate.write).
type fakeChunkStore struct {
	mu         sync.Mutex
	maxEntries int
	calls      int
}

func (f *fakeChunkStore) ResolveOwnedUIDs(
	_ context.Context,
	_ postgres.ExecQueryer,
	entries []postgres.GraphNodeOwnerEntry,
	_ time.Time,
) (map[string]struct{}, int, error) {
	f.mu.Lock()
	f.calls++
	if len(entries) > f.maxEntries {
		f.maxEntries = len(entries)
	}
	f.mu.Unlock()

	owned := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		owned[e.UID] = struct{}{}
	}
	return owned, 0, nil
}

// TestGateWriteChunksCriticalSectionAtLockChunkSize is the P2-1 unit proof:
// driving 1201 distinct-uid rows through Gate.write must never resolve more
// than lockChunkSize uids under one advisory-lock transaction, must open
// exactly ceil(1201/lockChunkSize) transactions, and must not drop, duplicate,
// or reorder any row across the chunk boundary. Against the pre-chunking
// single-transaction implementation this fails both bounds: one
// ResolveOwnedUIDs call would see all 1201 entries (unbounded — the #5007
// P2-1 lock-exhaustion defect) and only one transaction would open.
func TestGateWriteChunksCriticalSectionAtLockChunkSize(t *testing.T) {
	t.Parallel()

	const rowCount = 1201
	rows := make([]map[string]any, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		rows = append(rows, map[string]any{
			"uid":              fmt.Sprintf("uid-%05d", i),
			"source_order_key": fmt.Sprintf("2026-01-01T00:00:00.%09dZ|fact-%05d", i, i),
		})
	}

	beginner := &fakeChunkBeginner{}
	store := &fakeChunkStore{}
	gate := &Gate{db: beginner, store: store}

	var written []map[string]any
	underlying := func(_ context.Context, chunkRows []map[string]any, _ string) error {
		written = append(written, chunkRows...)
		return nil
	}

	if err := gate.write(context.Background(), familyCloudResource, rows, "test/chunk", underlying); err != nil {
		t.Fatalf("gate.write error = %v", err)
	}

	if store.maxEntries > lockChunkSize {
		t.Fatalf("max entries resolved under one tx = %d, want <= lockChunkSize (%d)", store.maxEntries, lockChunkSize)
	}
	wantTx := (rowCount + lockChunkSize - 1) / lockChunkSize
	if beginner.calls() != wantTx {
		t.Fatalf("tx count = %d, want %d (ceil(%d/%d))", beginner.calls(), wantTx, rowCount, lockChunkSize)
	}
	if store.calls != wantTx {
		t.Fatalf("ResolveOwnedUIDs call count = %d, want %d (one per tx)", store.calls, wantTx)
	}

	if len(written) != rowCount {
		t.Fatalf("underlying wrote %d rows total, want %d — chunking must not drop rows", len(written), rowCount)
	}
	seen := make(map[string]struct{}, rowCount)
	for i, row := range written {
		uid, _ := row["uid"].(string)
		if uid == "" {
			t.Fatalf("written row %d missing uid: %v", i, row)
		}
		if _, dup := seen[uid]; dup {
			t.Fatalf("uid %q written more than once across chunks", uid)
		}
		seen[uid] = struct{}{}
		if row["uid"] != rows[i]["uid"] {
			t.Fatalf("written row %d order changed: got uid %v, want %v", i, row["uid"], rows[i]["uid"])
		}
	}

	for i, tx := range beginner.txs {
		if !tx.committed {
			t.Fatalf("tx %d was not committed", i)
		}
		if tx.rolledBack {
			t.Fatalf("tx %d was rolled back after a successful chunk", i)
		}
	}
}

// TestGateWriteChunkSizeIsBoundedByCypherDefaultBatchSize pins lockChunkSize
// to cypher.DefaultBatchSize (500) so the owner-ledger chunk boundary tracks
// the graph writer's own MERGE batch size, and documents the ~6400-slot
// default Postgres advisory-lock budget this bound stays under. See the
// lockChunkSize doc comment and docs/internal/design/5007-cross-scope-node-ownership.md
// for the 20000-lock "out of shared memory" proof this bound fixes.
func TestGateWriteChunkSizeIsBoundedByCypherDefaultBatchSize(t *testing.T) {
	t.Parallel()

	if lockChunkSize != 500 {
		t.Fatalf("lockChunkSize = %d, want 500 (cypher.DefaultBatchSize)", lockChunkSize)
	}
	const defaultSharedLockSlots = 64 * 100 // max_locks_per_transaction * max_connections, Postgres 18 stock defaults
	if lockChunkSize >= defaultSharedLockSlots {
		t.Fatalf("lockChunkSize (%d) must stay well under the default shared advisory-lock budget (%d)", lockChunkSize, defaultSharedLockSlots)
	}
}
