// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// seedSweeperScopes seeds three scopes whose active generation has no journal
// row: "held" (a1), "free" (b1) and "gone", whose active_generation_id names
// no scope_generations row.
func (l *ledgerDB) seedSweeperScopes(t *testing.T) {
	t.Helper()
	for scopeID, generationID := range map[string]string{"held": "a1", "free": "b1"} {
		l.seedScope(t, scopeID)
		l.seedGeneration(t, scopeID, generationID, false, "active", fixtureEpoch, time.Time{})
		l.setActive(t, scopeID, generationID)
	}
	l.seedScope(t, "gone")
	l.setActive(t, "gone", "gone-never-existed")
}

// TestJournalSweeperSkipsAGenerationRetentionHoldsAndJournalsItNextPass proves
// the mechanics of the sweeper's SKIP LOCKED (#8932). A session holding the
// active generation FOR UPDATE, as generation retention does, makes the pass
// skip that generation without waiting and without an error; the other scopes
// are journaled in the same pass, including the scope whose generation row is
// absent (there is nothing to lock). A pass after the holder ends journals the
// generation if it is still active. In production a skipped generation is one
// that retention is pruning or the purge is deleting, so its activation is
// dropped on purpose and the scope's current active generation is journaled
// by the next pass or by its Ack; no production writer holds an active
// generation FOR UPDATE and then releases it.
func TestJournalSweeperSkipsAGenerationRetentionHoldsAndJournalsItNextPass(t *testing.T) {
	l := openLedgerDB(t)
	l.seedSweeperScopes(t)
	holder := l.begin(t)
	defer func() { _ = holder.Rollback() }()
	l.execTx(t, holder, `SELECT 1 FROM scope_generations WHERE generation_id = 'a1' FOR UPDATE`)

	store := linksfreshnessstore.NewJournalStore(l.store)
	ctx, cancel := context.WithTimeout(l.ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	first, err := store.Journal(ctx, 0)
	if err != nil {
		t.Fatalf("Journal with a1 held: %v after %s", err, time.Since(start))
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Journal with a1 held took %s, want a non-blocking skip", elapsed)
	}
	rows := strings.Join(l.journalRows(t), ",")
	for _, want := range []string{"free|b1|<null>|sweeper", "gone|gone-never-existed|<null>|sweeper"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("journal %q lacks %q", rows, want)
		}
	}
	if strings.Contains(rows, "held|a1") {
		t.Fatalf("journal %q names the held generation", rows)
	}
	// eshu:global is journaled too, so the pass wrote the two scopes above
	// plus it.
	if first.SweeperRows != 3 {
		t.Fatalf("first pass = %+v, want three sweeper rows (free, gone, eshu:global)", first)
	}

	if err := holder.Rollback(); err != nil {
		t.Fatalf("release holder: %v", err)
	}
	second, err := store.Journal(l.ctx, 0)
	if err != nil || second.SweeperRows != 1 {
		t.Fatalf("pass after release = %+v, %v; want the one skipped generation", second, err)
	}
	if rows := strings.Join(l.journalRows(t), ","); !strings.Contains(rows, "held|a1|<null>|sweeper") {
		t.Fatalf("journal %q lacks the skipped generation after release", rows)
	}
}

// TestJournalSweeperLockWaitIsBounded is the chain-walk shape of arbiter
// ruling arb-7127-3e-wait applied to the sweeper (#8932). A holder updates
// the active generation's row and locks the new version FOR UPDATE, as
// retention does after an Ack: the sweeper's FOR KEY SHARE SKIP LOCKED sees
// the old version, walks the update chain, and waits whatever its wait
// policy. Required: under the pass's bound the pass fails within a second
// with the non-counting generation_lock_timeout (55P03) and writes nothing.
// RED control: the shipped statement without the bound waits for the holder
// until the test context expires. A third subtest requires the bound to end
// before the cursor statement that follows the sweeper.
func TestJournalSweeperLockWaitIsBounded(t *testing.T) {
	t.Run("pass is bounded", func(t *testing.T) {
		l := openLedgerDB(t)
		l.seedSweeperScopes(t)
		holder := l.holdChain(t, "a1")
		defer func() { _ = holder.Rollback() }()

		ctx, cancel := context.WithTimeout(l.ctx, holderContext)
		defer cancel()
		start := time.Now()
		_, err := linksfreshnessstore.NewJournalStore(l.store).Journal(ctx, 0)
		elapsed := time.Since(start)
		reason, ok := linksfreshnessstore.RetryReasonOf(err)
		if !ok || reason != linksfreshnessstore.RetryGenerationLockTimeout {
			t.Fatalf("Journal after %s = %v, want generation_lock_timeout", elapsed, err)
		}
		if elapsed < lockTimeoutAtLeast || elapsed >= lockMissUnder {
			t.Fatalf("generation_lock_timeout after %s, want at least %s and under %s", elapsed, lockTimeoutAtLeast, lockMissUnder)
		}
		if rows := l.journalRows(t); len(rows) != 0 {
			t.Fatalf("a timed-out pass left rows %v", rows)
		}
		t.Logf("sweeper generation_lock_timeout after %s", elapsed)
	})
	t.Run("bound ends before the cursor insert", requireCursorInsertWaits)
	t.Run("unbounded statement waits", func(t *testing.T) {
		l := openLedgerDB(t)
		l.seedSweeperScopes(t)
		holder := l.holdChain(t, "a1")
		defer func() { _ = holder.Rollback() }()

		ctx, cancel := context.WithTimeout(l.ctx, 1500*time.Millisecond)
		defer cancel()
		tx, err := l.raw.BeginTx(l.ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		start := time.Now()
		_, err = tx.ExecContext(ctx, linksfreshnessstore.JournalActiveGenerationsQueryForTest, fixtureEpoch)
		elapsed := time.Since(start)
		if err == nil || elapsed < time.Second {
			t.Fatalf("unbounded sweeper statement returned after %s with %v; want it to wait for the holder", elapsed, err)
		}
		t.Logf("unbounded sweeper statement waited %s: %v", elapsed, err)
	})
}

// requireCursorInsertWaits pins the reset that ends the sweeper's generation
// lock bound (#8932). It runs as a subtest of TestJournalSweeperLockWaitIsBounded. The cursor statement that
// follows the sweeper creates a cursor row for every journaled scope and, on a
// scope whose cursor insert another transaction has not committed (a link
// transaction's), waits for that transaction. It must wait, as it did before
// the sweeper took a bounded lock: with the 250 ms bound still set it would
// fail the whole pass with 55P03 and journal nothing. A holder that commits
// after 400 ms must not fail the pass.
func requireCursorInsertWaits(t *testing.T) {
	t.Helper()
	const hold = 400 * time.Millisecond
	l := openLedgerDB(t)
	l.seedSweeperScopes(t)
	holder := l.begin(t)
	defer func() { _ = holder.Rollback() }()
	l.execTx(t, holder, `
INSERT INTO changed_since_scope_cursor (scope_id, state_generation_id, state_activation_seq, digest_version, updated_at)
VALUES ('free', NULL, 0, 1, now())`)

	type outcome struct {
		result  linksfreshnessstore.JournalResult
		elapsed time.Duration
		err     error
	}
	done := make(chan outcome, 1)
	ctx, cancel := context.WithTimeout(l.ctx, holderContext)
	defer cancel()
	start := time.Now()
	go func() {
		result, err := linksfreshnessstore.NewJournalStore(l.store).Journal(ctx, 0)
		done <- outcome{result: result, elapsed: time.Since(start), err: err}
	}()
	time.Sleep(hold)
	if err := holder.Commit(); err != nil {
		t.Fatalf("commit the cursor holder: %v", err)
	}
	got := <-done
	if got.err != nil {
		t.Fatalf("Journal with a cursor insert held for %s = %v after %s, want success", hold, got.err, got.elapsed)
	}
	if got.elapsed < hold {
		t.Fatalf("Journal answered after %s, before the %s holder committed: the cursor insert did not wait", got.elapsed, hold)
	}
	if rows := strings.Join(l.journalRows(t), ","); !strings.Contains(rows, "free|b1|<null>|sweeper") {
		t.Fatalf("journal %q lacks the swept generation", rows)
	}
	t.Logf("Journal waited %s for the held cursor insert and committed %+v", got.elapsed, got.result)
}
