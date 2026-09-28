// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// Bounds of a generation-lock timeout, stated before any run (arbiter ruling
// arb-7127-3e-wait): the lock gives up after generationLockTimeout (250 ms)
// and the whole miss answers in under a second. Without the timeout the
// call waits for the holder, so the test context (3 s) expires instead.
const (
	lockTimeoutAtLeast = 250 * time.Millisecond
	lockMissUnder      = time.Second
	holderContext      = 3 * time.Second
)

// holdChain is the deterministic chain-walk fixture of arbiter ruling
// arb-7127-3e-wait (D1): one open transaction updates a non-key column of the
// generation row, then locks the new version FOR UPDATE. A later FOR KEY
// SHARE SKIP LOCKED on the row sees the old version, walks the update chain
// and waits there, whatever its wait policy.
func (l *ledgerDB) holdChain(t *testing.T, generationID string) *sql.Tx {
	t.Helper()
	holder := l.begin(t)
	l.execTx(t, holder, `UPDATE scope_generations SET ingested_at = ingested_at + interval '1 second' WHERE generation_id = $1`, generationID)
	l.execTx(t, holder, `SELECT 1 FROM scope_generations WHERE generation_id = $1 FOR UPDATE`, generationID)
	return holder
}

// requireLockTimeout links scopeID with a holder in place and requires the
// non-counting generation_lock_timeout within the stated bounds.
func requireLockTimeout(t *testing.T, ctx context.Context, w *linksfreshnessstore.LinkWriter, scopeID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, holderContext)
	defer cancel()
	start := time.Now()
	_, err := w.LinkNext(ctx, scopeID)
	elapsed := time.Since(start)
	var retry *linksfreshnessstore.RetryError
	if !errors.As(err, &retry) || retry.Reason != linksfreshnessstore.RetryGenerationLockTimeout {
		t.Fatalf("LinkNext(%s) after %s = %v, want generation_lock_timeout", scopeID, elapsed, err)
	}
	if retry.SQLState != "55P03" {
		t.Fatalf("generation_lock_timeout carries SQLSTATE %q, want 55P03", retry.SQLState)
	}
	if elapsed < lockTimeoutAtLeast || elapsed >= lockMissUnder {
		t.Fatalf("generation_lock_timeout after %s, want at least %s and under %s", elapsed, lockTimeoutAtLeast, lockMissUnder)
	}
	t.Logf("%s: generation_lock_timeout after %s", scopeID, elapsed)
}

// requireNothingWritten checks a miss left no trace: the cursor did not move,
// no attempt was counted and the scope holds only its root link.
func (l *ledgerDB) requireNothingWritten(t *testing.T, scopeID string, seq int64) {
	t.Helper()
	if _, now := l.cursor(t, scopeID); now != seq {
		t.Fatalf("cursor moved from %d to %d on generation_lock_timeout", seq, now)
	}
	if n := l.cursorAttempts(t, scopeID); n != 0 {
		t.Fatalf("generation_lock_timeout counted an attempt (%d)", n)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_links WHERE scope_id = $1`, scopeID); n != 1 {
		t.Fatalf("%s holds %d links after the miss, want 1 (its root)", scopeID, n)
	}
}

func (l *ledgerDB) cursorAttempts(t *testing.T, scopeID string) int64 {
	t.Helper()
	return l.queryInt(t, `SELECT attempt_count FROM changed_since_scope_cursor WHERE scope_id = $1`, scopeID)
}

// TestActivatingGenerationChainWaitTimesOut is W1 of arbiter ruling
// arb-7127-3e-wait: the chain-walk holder on G makes the link give up with
// generation_lock_timeout within the bounds, write nothing and count
// nothing; after the holder rolls back the link succeeds.
func TestActivatingGenerationChainWaitTimesOut(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedRootedScope(t, w, "w1", "w1-0", "w1-1")
	_, seq := l.cursor(t, "w1")
	holder := l.holdChain(t, "w1-1")
	defer func() { _ = holder.Rollback() }()
	requireLockTimeout(t, l.ctx, w, "w1")
	l.requireNothingWritten(t, "w1", seq)
	_ = holder.Rollback()
	if got := mustLink(t, w, l, "w1"); got.Kind != linksfreshnessstore.LinkKindIncremental || got.PriorGenerationID != "w1-0" {
		t.Fatalf("link after release = %+v, want incremental w1-0 -> w1-1", got)
	}
}

// TestPriorChainWaitTimesOut is W2: the same with the holder on the prior X.
// The rollback released G's KEY SHARE, so retention's candidate lock on G
// takes the row straight after.
func TestPriorChainWaitTimesOut(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedRootedScope(t, w, "w2", "w2-0", "w2-1")
	_, seq := l.cursor(t, "w2")
	holder := l.holdChain(t, "w2-0")
	defer func() { _ = holder.Rollback() }()
	requireLockTimeout(t, l.ctx, w, "w2")
	l.requireNothingWritten(t, "w2", seq)
	retention := l.begin(t)
	var got int64
	if err := retention.QueryRowContext(l.ctx, `SELECT count(*) FROM (SELECT 1 FROM scope_generations
WHERE generation_id = 'w2-1' FOR UPDATE SKIP LOCKED) AS locked`).Scan(&got); err != nil || got != 1 {
		t.Fatalf("retention lock of G after the timeout = %d, %v; want the row (the link released it)", got, err)
	}
	_ = retention.Rollback()
	_ = holder.Rollback()
	if link := mustLink(t, w, l, "w2"); link.Kind != linksfreshnessstore.LinkKindIncremental || link.PriorGenerationID != "w2-0" {
		t.Fatalf("link after release = %+v, want incremental w2-0 -> w2-1", link)
	}
}

// TestLockTimeoutDoesNotLeaveTheTransaction is W7: on a pool of one
// connection, after a lock miss, a lock timeout and a committed link, the
// connection's lock_timeout is the server default: the setting is
// transaction-local.
func TestLockTimeoutDoesNotLeaveTheTransaction(t *testing.T) {
	l := openLedgerDB(t)
	one, err := sql.Open("pgx", l.dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = one.Close() }()
	one.SetMaxOpenConns(1)
	one.SetMaxIdleConns(1)
	w := linksfreshnessstore.NewLinkWriter(postgres.SQLDB{DB: one})
	l.seedRootedScope(t, w, "w7", "w7-0", "w7-1")
	show := func(after string) {
		t.Helper()
		var setting string
		if err := one.QueryRowContext(l.ctx, `SHOW lock_timeout`).Scan(&setting); err != nil {
			t.Fatalf("SHOW lock_timeout: %v", err)
		}
		if setting != "0" {
			t.Fatalf("lock_timeout after %s = %q on the pooled connection, want the default 0", after, setting)
		}
	}
	miss := l.begin(t)
	l.execTx(t, miss, `SELECT 1 FROM scope_generations WHERE generation_id = 'w7-1' FOR UPDATE`)
	if _, err := w.LinkNext(l.ctx, "w7"); err == nil {
		t.Fatal("LinkNext with G held succeeded, want generation_locked")
	}
	_ = miss.Rollback()
	show("a lock miss")
	holder := l.holdChain(t, "w7-1")
	requireLockTimeout(t, l.ctx, w, "w7")
	_ = holder.Rollback()
	show("a lock timeout")
	if link := mustLink(t, w, l, "w7"); link.Kind != linksfreshnessstore.LinkKindIncremental {
		t.Fatalf("link = %+v, want incremental", link)
	}
	show("a committed link")
}

// TestBackfillChainWaitTimesOut is W8: the chain-walk holder on a chain
// generation makes the journal pass give up with the non-counting
// generation_lock_timeout in under a second, and the pass journals nothing,
// sweeper rows included. After the release the next pass journals the chain
// and the sweeper row.
func TestBackfillChainWaitTimesOut(t *testing.T) {
	l := openLedgerDB(t)
	t0 := fixtureEpoch
	l.seedScope(t, "bw")
	for i, gen := range []string{"bw0", "bw1", "bw2"} {
		l.seedGeneration(t, "bw", gen, i > 0, "superseded", t0.Add(time.Duration(i)*time.Hour), t0.Add(time.Duration(i+1)*time.Hour))
	}
	l.seedGeneration(t, "bw", "bw3", true, "active", t0.Add(3*time.Hour), time.Time{})
	l.setActive(t, "bw", "bw3")
	journal := linksfreshnessstore.NewJournalStore(l.store)

	holder := l.holdChain(t, "bw1")
	defer func() { _ = holder.Rollback() }()
	ctx, cancel := context.WithTimeout(l.ctx, holderContext)
	defer cancel()
	start := time.Now()
	_, err := journal.Journal(ctx, 10)
	elapsed := time.Since(start)
	if reason, ok := linksfreshnessstore.RetryReasonOf(err); !ok || reason != linksfreshnessstore.RetryGenerationLockTimeout {
		t.Fatalf("Journal after %s = %v, want generation_lock_timeout", elapsed, err)
	}
	if elapsed >= lockMissUnder {
		t.Fatalf("Journal gave up after %s, want under %s", elapsed, lockMissUnder)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_activations`); n != 0 {
		t.Fatalf("the timed-out pass journaled %d rows, want none (sweeper rows included)", n)
	}
	_ = holder.Rollback()
	if _, err := journal.Journal(l.ctx, 10); err != nil {
		t.Fatalf("Journal after release: %v", err)
	}
	want := []string{"bw|bw0|<null>|backfill", "bw|bw1|bw0|backfill", "bw|bw2|bw1|backfill", "bw|bw3|bw2|backfill"}
	if got := l.journalRows(t); len(got) != len(want) {
		t.Fatalf("journal after release = %v, want %v", got, want)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_activations WHERE source = 'sweeper'`); n == 0 {
		t.Fatal("the pass after release journaled no sweeper row (eshu:global)")
	}
}
