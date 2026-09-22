// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// fakeRetractStore is an in-memory graphNodeOwnerResolver for the #6887
// retract path: it records every LockUIDs and ReleaseOwnedUIDs call against
// the fake transaction and always grants ownership on ResolveOwnedUIDs (the
// write path is not under test here).
type fakeRetractStore struct {
	mu       sync.Mutex
	locked   [][]string
	released [][]string
}

func (f *fakeRetractStore) ResolveOwnedUIDs(
	_ context.Context,
	_ db.ExecQueryer,
	entries []postgres.GraphNodeOwnerEntry,
	_ time.Time,
) (map[string]struct{}, int, error) {
	owned := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		owned[e.UID] = struct{}{}
	}
	return owned, 0, nil
}

func (f *fakeRetractStore) LockUIDs(_ context.Context, _ db.ExecQueryer, uids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.locked = append(f.locked, slices.Clone(uids))
	return nil
}

func (f *fakeRetractStore) ReleaseOwnedUIDs(_ context.Context, _ db.ExecQueryer, uids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, slices.Clone(uids))
	return nil
}

// retractDeleter records every delete call in order.
type retractDeleter struct {
	mu    sync.Mutex
	calls [][]string
	err   error
}

func (d *retractDeleter) delete(_ context.Context, uids []string, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, slices.Clone(uids))
	return d.err
}

// aliveSet returns a nodeLivenessFunc reporting exactly alive as live.
func aliveSet(alive ...string) nodeLivenessFunc {
	set := make(map[string]struct{}, len(alive))
	for _, uid := range alive {
		set[uid] = struct{}{}
	}
	return func(context.Context, db.ExecQueryer, []string) (map[string]struct{}, error) {
		return set, nil
	}
}

func newRetractGate(beginner *fakeChunkBeginner, store *fakeRetractStore) *Gate {
	return &Gate{database: beginner, store: store}
}

// TestGateRetractDeadUIDsDeletesOnlyDead is the #6887 core contract: of the
// candidate uids, the live ones (still admitted in some scope's current
// generation) are never deleted and never released, while the dead ones are
// released from the ledger and deleted under the same per-uid lock.
func TestGateRetractDeadUIDsDeletesOnlyDead(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeRetractStore{}
	deleter := &retractDeleter{}
	gate := newRetractGate(beginner, store)

	retracted, err := gate.RetractDeadUIDs(
		context.Background(), "cloud", []string{"uid-dead", "uid-live"}, "reducer/aws-resources",
		aliveSet("uid-live"), deleter.delete,
	)
	if err != nil {
		t.Fatalf("RetractDeadUIDs returned error: %v", err)
	}
	if retracted != 1 {
		t.Fatalf("retracted = %d, want 1", retracted)
	}
	if len(deleter.calls) != 1 || len(deleter.calls[0]) != 1 || deleter.calls[0][0] != "uid-dead" {
		t.Fatalf("delete calls = %v, want exactly [[uid-dead]]", deleter.calls)
	}
	if len(store.released) != 1 || len(store.released[0]) != 1 || store.released[0][0] != "uid-dead" {
		t.Fatalf("released = %v, want exactly [[uid-dead]]", store.released)
	}
	if beginner.calls() != 1 {
		t.Fatalf("transactions = %d, want 1", beginner.calls())
	}
	if !beginner.txs[0].committed {
		t.Fatal("chunk transaction was not committed")
	}
}

// TestGateRetractDeadUIDsSkipsDeleteWhenAllAlive proves the no-over-delete
// direction: every candidate live means no ledger release, no graph delete,
// but the lock-holding transaction still commits.
func TestGateRetractDeadUIDsSkipsDeleteWhenAllAlive(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeRetractStore{}
	deleter := &retractDeleter{}
	gate := newRetractGate(beginner, store)

	retracted, err := gate.RetractDeadUIDs(
		context.Background(), "cloud", []string{"uid-a", "uid-b"}, "reducer/aws-resources",
		aliveSet("uid-a", "uid-b"), deleter.delete,
	)
	if err != nil {
		t.Fatalf("RetractDeadUIDs returned error: %v", err)
	}
	if retracted != 0 {
		t.Fatalf("retracted = %d, want 0", retracted)
	}
	if len(deleter.calls) != 0 {
		t.Fatalf("delete calls = %v, want none", deleter.calls)
	}
	if len(store.released) != 0 {
		t.Fatalf("released = %v, want none", store.released)
	}
	if !beginner.txs[0].committed {
		t.Fatal("lock-holding transaction was not committed")
	}
}

// TestGateRetractDeadUIDsEmptyIsNoOp opens no transaction and deletes nothing.
func TestGateRetractDeadUIDsEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeRetractStore{}
	deleter := &retractDeleter{}
	gate := newRetractGate(beginner, store)

	retracted, err := gate.RetractDeadUIDs(
		context.Background(), "cloud", nil, "reducer/aws-resources",
		aliveSet(), deleter.delete,
	)
	if err != nil {
		t.Fatalf("RetractDeadUIDs returned error: %v", err)
	}
	if retracted != 0 {
		t.Fatalf("retracted = %d, want 0", retracted)
	}
	if beginner.calls() != 0 {
		t.Fatalf("transactions = %d, want 0", beginner.calls())
	}
	if len(deleter.calls) != 0 {
		t.Fatalf("delete calls = %v, want none", deleter.calls)
	}
}

// TestGateRetractDeadUIDsDeletesInSortedOrder pins determinism: the delete
// batches run in sorted uid order regardless of candidate input order, so
// retries and replays converge on the same statement sequence.
func TestGateRetractDeadUIDsDeletesInSortedOrder(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeRetractStore{}
	deleter := &retractDeleter{}
	gate := newRetractGate(beginner, store)

	uids := []string{"uid-c", "uid-a", "uid-b"}
	if _, err := gate.RetractDeadUIDs(
		context.Background(), "cloud", uids, "reducer/aws-resources",
		aliveSet(), deleter.delete,
	); err != nil {
		t.Fatalf("RetractDeadUIDs returned error: %v", err)
	}
	if len(deleter.calls) != 1 {
		t.Fatalf("delete calls = %v, want 1 sorted batch", deleter.calls)
	}
	want := []string{"uid-a", "uid-b", "uid-c"}
	if !slices.Equal(deleter.calls[0], want) {
		t.Fatalf("delete batch = %v, want sorted %v", deleter.calls[0], want)
	}
	if !slices.Equal(uids, []string{"uid-c", "uid-a", "uid-b"}) {
		t.Fatalf("input slice was mutated: %v", uids)
	}
}

// TestGateRetractDeadUIDsRollsBackOnLivenessError proves fail-closed: a
// live-check failure deletes nothing and rolls the chunk back (no release).
func TestGateRetractDeadUIDsRollsBackOnLivenessError(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeRetractStore{}
	deleter := &retractDeleter{}
	gate := newRetractGate(beginner, store)

	livenessErr := errors.New("liveness probe failed")
	_, err := gate.RetractDeadUIDs(
		context.Background(), "cloud", []string{"uid-x"}, "reducer/aws-resources",
		func(context.Context, db.ExecQueryer, []string) (map[string]struct{}, error) {
			return nil, livenessErr
		}, deleter.delete,
	)
	if !errors.Is(err, livenessErr) {
		t.Fatalf("err = %v, want liveness failure", err)
	}
	if len(deleter.calls) != 0 {
		t.Fatalf("delete calls = %v, want none on liveness error", deleter.calls)
	}
	if len(store.released) != 0 {
		t.Fatalf("released = %v, want none on liveness error", store.released)
	}
	if !beginner.txs[0].rolledBack || beginner.txs[0].committed {
		t.Fatal("chunk transaction must roll back on liveness error")
	}
}

// TestGateRetractDeadUIDsRollsBackOnDeleteError proves the ledger release
// never commits without its graph delete: a delete failure rolls the whole
// chunk (including the release) back so a retry re-proves liveness.
func TestGateRetractDeadUIDsRollsBackOnDeleteError(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeRetractStore{}
	deleter := &retractDeleter{err: errors.New("graph delete failed")}
	gate := newRetractGate(beginner, store)

	if _, err := gate.RetractDeadUIDs(
		context.Background(), "cloud", []string{"uid-x"}, "reducer/aws-resources",
		aliveSet(), deleter.delete,
	); err == nil {
		t.Fatal("RetractDeadUIDs with failing deleter = nil, want error")
	}
	if !beginner.txs[0].rolledBack || beginner.txs[0].committed {
		t.Fatal("chunk transaction must roll back on delete error")
	}
}

// TestGateRetractDeadUIDsSkipsWithoutLedger pins fail-closed: with no ledger
// wired there is no per-uid lock and no in-transaction live-check, so the
// retract issues nothing — the failure mode is today's stale node, never an
// over-delete. Production always wires the ledger.
func TestGateRetractDeadUIDsSkipsWithoutLedger(t *testing.T) {
	t.Parallel()

	deleter := &retractDeleter{}
	var gate *Gate

	retracted, err := gate.RetractDeadUIDs(
		context.Background(), "cloud", []string{"uid-x"}, "reducer/aws-resources",
		aliveSet(), deleter.delete,
	)
	if err != nil {
		t.Fatalf("RetractDeadUIDs returned error: %v", err)
	}
	if retracted != 0 {
		t.Fatalf("retracted = %d, want 0 without ledger", retracted)
	}
	if len(deleter.calls) != 0 {
		t.Fatalf("delete calls = %v, want none without ledger", deleter.calls)
	}

	gate = NewGate(nil)
	retracted, err = gate.RetractDeadUIDs(
		context.Background(), "cloud", []string{"uid-y"}, "reducer/aws-resources",
		aliveSet(), deleter.delete,
	)
	if err != nil {
		t.Fatalf("RetractDeadUIDs returned error: %v", err)
	}
	if retracted != 0 {
		t.Fatalf("retracted = %d, want 0 without database", retracted)
	}
	if len(deleter.calls) != 0 {
		t.Fatalf("delete calls = %v, want none without database", deleter.calls)
	}
}

// TestGateRetractDeadUIDsChunksAtLockChunkSize is the #5007 P2-1 bound for
// the delete path: lockChunkSize+1 candidates open exactly two transactions,
// so no single transaction can exhaust the advisory-lock table.
func TestGateRetractDeadUIDsChunksAtLockChunkSize(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeRetractStore{}
	deleter := &retractDeleter{}
	gate := newRetractGate(beginner, store)

	uids := make([]string, 0, lockChunkSize+1)
	for i := 0; i < lockChunkSize+1; i++ {
		uids = append(uids, fmt.Sprintf("uid-%05d", i))
	}
	retracted, err := gate.RetractDeadUIDs(
		context.Background(), "cloud", uids, "reducer/aws-resources",
		aliveSet(), deleter.delete,
	)
	if err != nil {
		t.Fatalf("RetractDeadUIDs returned error: %v", err)
	}
	if retracted != lockChunkSize+1 {
		t.Fatalf("retracted = %d, want %d", retracted, lockChunkSize+1)
	}
	if beginner.calls() != 2 {
		t.Fatalf("transactions = %d, want 2", beginner.calls())
	}
	for _, tx := range beginner.txs {
		if !tx.committed {
			t.Fatal("every chunk transaction must commit")
		}
	}
}

// recordingHandler collects slog records so a test can assert the retract's
// operator warnings without touching the process default logger.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *recordingHandler) WithGroup(string) slog.Handler { return h }

// TestGateRetractDeadUIDsLogsGhostOnDeleteError pins the operator signal for
// a failed graph delete: the ledger release rolls back while any earlier
// delete batch stayed gone, so the warning must carry the family and the
// exact uid set to reconcile, and the returned error must be attributable
// to the delete step.
func TestGateRetractDeadUIDsLogsGhostOnDeleteError(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeRetractStore{}
	deleter := &retractDeleter{err: errors.New("graph delete failed")}
	handler := &recordingHandler{}
	gate := newRetractGate(beginner, store)
	gate.Logger = slog.New(handler)

	_, err := gate.RetractDeadUIDs(
		context.Background(), "cloud", []string{"uid-b", "uid-a"}, "reducer/aws-resources",
		aliveSet(), deleter.delete,
	)
	if err == nil {
		t.Fatal("RetractDeadUIDs with failing deleter = nil, want error")
	}
	if !strings.Contains(err.Error(), "graphowner: delete retract nodes for cloud") {
		t.Fatalf("error = %q, want the delete step named", err)
	}
	if !beginner.txs[0].rolledBack || beginner.txs[0].committed {
		t.Fatal("chunk transaction must roll back on delete error")
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if len(handler.records) != 1 || handler.records[0].Level != slog.LevelWarn {
		t.Fatalf("records = %d, want exactly one warning", len(handler.records))
	}
	var family string
	var uids any
	handler.records[0].Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "family":
			family = a.Value.String()
		case "uids":
			uids = a.Value.Any()
		}
		return true
	})
	if family != "cloud" {
		t.Fatalf("warning family = %q, want cloud", family)
	}
	got, ok := uids.([]string)
	if !ok || !slices.Equal(got, []string{"uid-a", "uid-b"}) {
		t.Fatalf("warning uids = %v, want the sorted dead set [uid-a uid-b]", uids)
	}
}

// TestCloudResourceRetracterRefusesBeforeLockingWhenAdmissionUndrained pins
// the pre-lock fence: a refusal returns the sentinel-wrapping error before
// LockUIDs is ever called, using one short rolled-back transaction and no
// delete, so a routine multi-scope race costs one index probe and no lock
// churn on the write path.
func TestCloudResourceRetracterRefusesBeforeLockingWhenAdmissionUndrained(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeRetractStore{}
	deleter := &retractDeleter{}
	gate := newRetractGate(beginner, store)
	retracter := &CloudResourceRetracter{
		gate:        gate,
		deleteNodes: deleter.delete,
		requireDrained: func(context.Context, db.ExecQueryer) error {
			return fmt.Errorf("%w: scope-b/gen-2=pending", reducercontract.ErrCloudAdmissionUndrained)
		},
	}

	retracted, err := retracter.RetractDeadCloudResourceNodes(
		context.Background(), []string{"uid-a", "uid-b"}, "reducer/aws-resources")
	if !errors.Is(err, reducercontract.ErrCloudAdmissionUndrained) {
		t.Fatalf("err = %v, want the undrained sentinel", err)
	}
	if retracted != 0 {
		t.Fatalf("retracted = %d, want 0", retracted)
	}
	if len(store.locked) != 0 {
		t.Fatalf("LockUIDs calls = %v, want none before the fence passes", store.locked)
	}
	if len(deleter.calls) != 0 {
		t.Fatalf("delete calls = %v, want none", deleter.calls)
	}
	if beginner.calls() != 1 || !beginner.txs[0].rolledBack || beginner.txs[0].committed {
		t.Fatalf("fence transaction: calls=%d txs=%+v, want one rolled-back transaction", beginner.calls(), beginner.txs)
	}
}
