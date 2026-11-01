// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// postgresExecer adapts the test pool to the writer-transaction Execer surface.
type postgresExecer struct{ db *sql.DB }

func (e postgresExecer) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return e.db.ExecContext(ctx, query, args...)
}

func reducercontractResult() reducercontract.Result { return reducercontract.Result{} }

func ownedMarker(t *testing.T, c *convergence, itemID string) bool {
	t.Helper()
	owed, _ := fanOutStoredPayload(t, c.db, itemID)[supplychaincore.PayloadKeyFanOutOwed].(bool)
	return owed
}

// recordingWriter remembers every write it was handed and can fail the first one
// before anything commits.
type recordingWriter struct {
	inner      supplychaincore.SupplyChainImpactWriter
	mu         sync.Mutex
	writes     []supplychaincore.SupplyChainImpactWrite
	failFirst  error
	failedOnce bool
}

func (w *recordingWriter) WriteSupplyChainImpactFindings(ctx context.Context, write supplychaincore.SupplyChainImpactWrite) (supplychaincore.SupplyChainImpactWriteResult, error) {
	w.mu.Lock()
	w.writes = append(w.writes, write)
	fail := w.failFirst != nil && !w.failedOnce
	w.failedOnce = true
	w.mu.Unlock()
	if fail {
		return supplychaincore.SupplyChainImpactWriteResult{}, w.failFirst
	}
	return w.inner.WriteSupplyChainImpactFindings(ctx, write)
}

// TestOwedMarkerLifecycleLive: the marker is written inside the writer's
// transaction, survives a fan-out that fails after the commit, makes the retry
// fan out the recovery set, and is gone once the fan-out finishes.
func TestOwedMarkerLifecycleLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	flaky := &flakyFindFanOut{SupplyChainImpactFanOut: c.fanOut}
	c.handler.FanOut = flaky
	writer := &recordingWriter{inner: c.handler.Writer}
	c.handler.Writer = writer
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)

	intent := c.raceClaim(convergenceSource)
	if _, err := c.handler.Handle(c.ctx, intent); err == nil {
		t.Fatal("first attempt error = nil, want the injected holder read failure")
	}
	if !ownedMarker(t, c, intent.IntentID) {
		t.Fatal("the item has no owed marker after a commit whose fan-out failed")
	}
	// The retry: the commit already happened, so its rewrite changes nothing and
	// only the marker says a fan-out is owed.
	if err := c.queue.Fail(c.ctx, intent, raceRetryableErr{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at=now()-interval '1 second', next_attempt_at=NULL WHERE work_item_id=$1`, intent.IntentID); err != nil {
		t.Fatal(err)
	}
	c.drain()
	recovery := false
	for _, write := range writer.writes {
		recovery = recovery || (write.ScopeID == convergenceSource && write.Recovery)
	}
	if !recovery {
		t.Fatalf("no source write asked for the recovery set: the marker says an attempt committed (writes %d)", len(writer.writes))
	}
	if ownedMarker(t, c, intent.IntentID) {
		t.Fatal("the owed marker survived a fan-out that reopened every holder")
	}
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// TestFailureBeforeTheCommitLeavesNoMarkerLive: an attempt that fails before its
// writer commits leaves nothing behind, so its retry asks for no recovery set and
// fans out only its own true changes.
func TestFailureBeforeTheCommitLeavesNoMarkerLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	writer := &recordingWriter{inner: c.handler.Writer, failFirst: raceRetryableErr{}}
	c.handler.Writer = writer
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)

	intent := c.raceClaim(convergenceSource)
	if _, err := c.handler.Handle(c.ctx, intent); err == nil {
		t.Fatal("first attempt error = nil, want the pre-commit failure")
	}
	if ownedMarker(t, c, intent.IntentID) {
		t.Fatal("an attempt that never committed left an owed marker")
	}
	if err := c.queue.Fail(c.ctx, intent, raceRetryableErr{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at=now()-interval '1 second', next_attempt_at=NULL WHERE work_item_id=$1`, intent.IntentID); err != nil {
		t.Fatal(err)
	}
	c.drain()
	var source []supplychaincore.SupplyChainImpactWrite
	for _, write := range writer.writes {
		if write.ScopeID == convergenceSource {
			source = append(source, write)
		}
	}
	if len(source) != 2 || source[1].Recovery {
		t.Fatalf("source writes=%d, retry Recovery=%v, want 2 writes and no recovery set", len(source), len(source) > 1 && source[1].Recovery)
	}
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// TestZombiePassCannotCommitAndItsReplacementFansOutOnceLive: a pass whose lease
// expired and was reclaimed tries to commit. Its marker update is fenced on the
// attempt, so the whole write rolls back; the replacement attempt commits, fans
// out, and the holder reruns exactly once.
func TestZombiePassCannotCommitAndItsReplacementFansOutOnceLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.handled = nil
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)

	zombie := c.raceClaim(convergenceSource)
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET claim_until = now() - interval '1 second' WHERE work_item_id = $1`, zombie.IntentID); err != nil {
		t.Fatal(err)
	}
	current := c.raceClaim(convergenceSource)
	if current.IntentID != zombie.IntentID || current.AttemptCount != 2 {
		t.Fatalf("reclaim = %s attempt %d, want the same item at attempt 2", current.IntentID, current.AttemptCount)
	}

	before := c.copies()
	if _, err := c.handler.Handle(c.ctx, zombie); err == nil {
		t.Fatal("the zombie pass committed: its write must roll back on the lost lease")
	}
	if after := c.copies(); after[convergenceSource] != before[convergenceSource] {
		t.Fatalf("source copy moved from %q to %q: the zombie's rows were not rolled back", before[convergenceSource], after[convergenceSource])
	}
	if ownedMarker(t, c, zombie.IntentID) {
		t.Fatal("the zombie left an owed marker")
	}

	result, err := c.handler.Handle(c.ctx, current)
	if err != nil {
		t.Fatalf("replacement pass: %v", err)
	}
	if err := c.queue.Ack(c.ctx, current, result); err != nil {
		t.Fatal(err)
	}
	c.drain()
	holderRuns := 0
	for _, entry := range c.handled {
		if strings.HasPrefix(entry, convergenceHolder+"#") {
			holderRuns++
		}
	}
	if holderRuns != 1 {
		t.Fatalf("holder ran %d times (handled %v), want exactly once", holderRuns, c.handled)
	}
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// TestUpdateFanOutStateKeepsTheOwedMarkerWhileHoldersAreOwedLive: the marker is
// set through the recorder, survives a continuation rewrite, and the explicit
// ClearOwed removes it.
func TestUpdateFanOutStateKeepsTheOwedMarkerWhileHoldersAreOwedLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "source", "g1")
	id := fanOutSeedItem(t, ctx, db, "source", "g1")
	store := fanOutLiveStore(db)
	queue := fanOutLiveQueue(db)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim = %v %v", ok, err)
	}
	if err := store.OwedRecorder(intent).RecordFanOutOwed(ctx, postgresExecer{db}); err != nil {
		t.Fatalf("RecordFanOutOwed() error = %v", err)
	}
	owed := func() bool {
		v, _ := fanOutStoredPayload(t, db, id)[supplychaincore.PayloadKeyFanOutOwed].(bool)
		return v
	}
	if !owed() {
		t.Fatal("the marker was not recorded")
	}
	if err := store.UpdateFanOutState(ctx, intent, supplychaincore.FanOutState{ReplaceRemaining: true, Remaining: []string{"holder-b"}}); err != nil {
		t.Fatal(err)
	}
	if !owed() {
		t.Fatal("a continuation rewrite cleared the marker while a holder was still owed")
	}
	if err := store.UpdateFanOutState(ctx, intent, supplychaincore.FanOutState{ReplaceRemaining: true, ClearOwed: true}); err != nil {
		t.Fatal(err)
	}
	if owed() {
		t.Fatal("ClearOwed left the marker in place")
	}
	// A lost lease is a retryable error, not a silent success.
	if err := queue.Ack(ctx, intent, reducercontractResult()); err != nil {
		t.Fatal(err)
	}
	err = store.OwedRecorder(intent).RecordFanOutOwed(ctx, postgresExecer{db})
	var retryable interface{ Retryable() bool }
	if err == nil || !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("RecordFanOutOwed() after the ack = %v, want a retryable lost-lease error", err)
	}
}
