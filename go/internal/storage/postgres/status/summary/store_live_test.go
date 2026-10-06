// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// The live tests in this file and bloat_live_test.go run against a disposable
// PostgreSQL 18 server. Point them at its administrative database:
//
//	ESHU_STATUS_SUMMARY_PROOF_DSN=postgres://user:pass@127.0.0.1:<port>/postgres?sslmode=disable \
//	ESHU_STATUS_SUMMARY_PROOF_DISPOSABLE=1 \
//	go test ./internal/storage/postgres/status/summary -run 'Live$' -count=1
//
// Every test creates and drops its own database. With no DSN they skip; the
// live-postgres-readiness job sets the DSN and fails if an expected test did
// not run.

// TestStatusSummaryMissingTableLive proves that, before migration 161 has been
// applied, both Read and Upsert classify the undefined table as ErrNotInstalled
// and keep the SQLSTATE reachable.
func TestStatusSummaryMissingTableLive(t *testing.T) {
	ctx, database := openProofDatabase(t)
	store := summary.NewStore(poolStore{database})

	if _, err := store.Read(ctx, summary.ModelActiveWorkSummary); !errors.Is(err, summary.ErrNotInstalled) {
		t.Fatalf("Read() on a database without the table = %v, want ErrNotInstalled", err)
	}
	if advanced, err := store.Upsert(ctx, proofRow(proofAsOf, "a", 3)); !errors.Is(err, summary.ErrNotInstalled) || advanced {
		t.Fatalf("Upsert() on a database without the table = %v, %v, want false, ErrNotInstalled", advanced, err)
	}
}

// TestStatusSummaryMigrationLive proves migration 161 applies twice without
// error, leaves the table empty (Read reports ErrNotFound, not a row), and
// leaves the five storage parameters on the table.
func TestStatusSummaryMigrationLive(t *testing.T) {
	ctx, database := openProofDatabase(t)
	applySummaryMigration(ctx, t, database)
	applySummaryMigration(ctx, t, database) // idempotent: a second boot is a no-op

	var count int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM status_summary_snapshots`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("a fresh migration left %d rows, want 0 (no backfill)", count)
	}
	if _, err := summary.Read(ctx, poolStore{database}, summary.ModelActiveWorkSummary); !errors.Is(err, summary.ErrNotFound) {
		t.Fatalf("Read() on the empty table = %v, want ErrNotFound", err)
	}

	var options string
	if err := database.QueryRowContext(ctx,
		`SELECT array_to_string(reloptions, ',') FROM pg_class
		  WHERE oid = 'status_summary_snapshots'::regclass`).Scan(&options); err != nil {
		t.Fatalf("read reloptions: %v", err)
	}
	for _, want := range []string{
		"fillfactor=50",
		"autovacuum_vacuum_scale_factor=0",
		"autovacuum_vacuum_threshold=50",
		"autovacuum_analyze_scale_factor=0",
		"autovacuum_analyze_threshold=50",
	} {
		if !strings.Contains(options, want) {
			t.Errorf("reloptions = %q, want it to contain %q", options, want)
		}
	}

	var indexes int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_indexes WHERE tablename = 'status_summary_snapshots'`).Scan(&indexes); err != nil {
		t.Fatalf("count indexes: %v", err)
	}
	if indexes != 1 {
		t.Fatalf("table has %d indexes, want only the primary key", indexes)
	}
}

// TestStatusSummaryGuardLive proves the as_of guard: a newer as_of advances the
// row, an older and an equal as_of are rejected and leave the stored row
// untouched. The same sequence against a guard-stripped copy of the production
// statement moves the row backwards, so the guard is what holds it.
func TestStatusSummaryGuardLive(t *testing.T) {
	ctx, database := openProofDatabase(t)
	applySummaryMigration(ctx, t, database)
	store := summary.NewStore(poolStore{database})

	newer := proofRow(proofAsOf.Add(10*time.Second), "newer", 6)
	older := proofRow(proofAsOf, "older", 4)
	equal := proofRow(proofAsOf.Add(10*time.Second), "equal-replay", 6)

	mustUpsert(ctx, t, store, newer, true)
	mustUpsert(ctx, t, store, older, false)
	assertStored(ctx, t, store, newer)
	mustUpsert(ctx, t, store, equal, false)
	assertStored(ctx, t, store, newer)
	advanced := proofRow(proofAsOf.Add(20*time.Second), "advanced", 2)
	mustUpsert(ctx, t, store, advanced, true)
	assertStored(ctx, t, store, advanced)

	// RED comparator: the production statement with its WHERE guard cut off.
	unguarded, _, found := strings.Cut(summary.UpsertSQL, "WHERE existing.as_of < EXCLUDED.as_of")
	if !found {
		t.Fatal("production upsert statement no longer contains the as_of guard this test strips")
	}
	payload, err := summary.EncodeEntries(older.Entries)
	if err != nil {
		t.Fatalf("EncodeEntries(): %v", err)
	}
	if _, err := database.ExecContext(ctx, unguarded,
		older.ModelKey, older.SchemaVersion, older.SourceSHA256, older.AsOf,
		1.0, older.RowCount, string(payload)); err != nil {
		t.Fatalf("unguarded upsert: %v", err)
	}
	got, err := store.Read(ctx, summary.ModelActiveWorkSummary)
	if err != nil {
		t.Fatalf("Read(): %v", err)
	}
	if !got.AsOf.Equal(older.AsOf) {
		t.Fatalf("guard-stripped upsert left as_of = %v, want it to go back to %v: the comparator proves nothing", got.AsOf, older.AsOf)
	}
}

// TestStatusSummaryConcurrentWritersLive runs two unlocked guarded writers per
// round, alternating which one starts first, while a reader polls. The final
// as_of of every round is the maximum of the two, and every as_of the reader
// observes is monotone non-decreasing. Both outcome orders ([1,1] and [1,0])
// are logged so a run that never exercised the race is visible.
func TestStatusSummaryConcurrentWritersLive(t *testing.T) {
	ctx, database := openProofDatabase(t)
	applySummaryMigration(ctx, t, database)
	store := summary.NewStore(poolStore{database})

	var (
		stop     = make(chan struct{})
		readerWG sync.WaitGroup
		observed []time.Time
	)
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if row, err := store.Read(ctx, summary.ModelActiveWorkSummary); err == nil {
				observed = append(observed, row.AsOf)
			}
		}
	}()

	pairs := map[[2]bool]int{}
	const rounds = 30
	for round := 0; round < rounds; round++ {
		base := proofAsOf.Add(time.Duration(2*round) * time.Second)
		older := proofRow(base, "older", 3)
		newer := proofRow(base.Add(time.Second), "newer", 3)
		first, second := newer, older
		if round%2 == 1 {
			first, second = older, newer
		}
		var (
			wg       sync.WaitGroup
			firstOK  bool
			secondOK bool
			errs     [2]error
			gate     = make(chan struct{})
		)
		wg.Add(2)
		go func() { defer wg.Done(); <-gate; firstOK, errs[0] = store.Upsert(ctx, first) }()
		go func() { defer wg.Done(); <-gate; secondOK, errs[1] = store.Upsert(ctx, second) }()
		close(gate)
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: Upsert() error = %v", round, err)
			}
		}
		if round%2 == 0 {
			pairs[[2]bool{firstOK, secondOK}]++ // first is the newer one
		} else {
			pairs[[2]bool{secondOK, firstOK}]++ // normalise to [newer, older]
		}
		got, err := store.Read(ctx, summary.ModelActiveWorkSummary)
		if err != nil {
			t.Fatalf("round %d: Read(): %v", round, err)
		}
		if !got.AsOf.Equal(newer.AsOf) {
			t.Fatalf("round %d: final as_of = %v, want the newer %v", round, got.AsOf, newer.AsOf)
		}
	}
	close(stop)
	readerWG.Wait()

	for i := 1; i < len(observed); i++ {
		if observed[i].Before(observed[i-1]) {
			t.Fatalf("reader saw as_of go backwards at poll %d: %v then %v", i, observed[i-1], observed[i])
		}
	}
	if pairs[[2]bool{true, false}] == 0 && pairs[[2]bool{true, true}] == 0 {
		t.Fatalf("no round ever upserted the newer row: pairs = %v", pairs)
	}
	t.Logf("observed %d reader polls, all monotone; [newer upserted, older upserted] outcome counts over %d rounds: %v",
		len(observed), rounds, pairs)
}

// TestStatusSummaryCrashSafetyLive proves the single-row write is atomic: a
// backend killed after the upsert statement but before commit leaves the stored
// row exactly as it was, and the next pass replaces it.
func TestStatusSummaryCrashSafetyLive(t *testing.T) {
	ctx, database := openProofDatabase(t)
	applySummaryMigration(ctx, t, database)
	store := summary.NewStore(poolStore{database})

	stored := proofRow(proofAsOf, "stored", 5)
	mustUpsert(ctx, t, store, stored, true)

	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn(): %v", err)
	}
	defer func() { _ = conn.Close() }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx(): %v", err)
	}
	var pid int
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("pg_backend_pid(): %v", err)
	}
	partial := proofRow(proofAsOf.Add(5*time.Second), "partial", 2)
	if advanced, err := summary.Upsert(ctx, txStore{tx}, partial); err != nil || !advanced {
		t.Fatalf("in-transaction Upsert() = %v, %v, want true, nil", advanced, err)
	}
	var terminated bool
	if err := database.QueryRowContext(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("pg_terminate_backend(%d) = %v, %v, want true, nil", pid, terminated, err)
	}
	if err := tx.Commit(); err == nil {
		t.Fatal("Commit() on a terminated backend = nil, want an error")
	}
	assertStored(ctx, t, store, stored)

	next := proofRow(proofAsOf.Add(10*time.Second), "next", 5)
	mustUpsert(ctx, t, store, next, true)
	assertStored(ctx, t, store, next)
}

// TestStatusSummaryWriterLockLive proves the advisory lock: a second
// transaction cannot take it while the first holds it, it is free again after
// the first rolls back, and a killed holder releases it with its backend.
func TestStatusSummaryWriterLockLive(t *testing.T) {
	ctx, database := openProofDatabase(t)

	first := beginTx(ctx, t, database)
	if held, err := summary.TryLock(ctx, txStore{first}); err != nil || !held {
		t.Fatalf("first TryLock() = %v, %v, want true, nil", held, err)
	}
	second := beginTx(ctx, t, database)
	if held, err := summary.TryLock(ctx, txStore{second}); err != nil || held {
		t.Fatalf("second TryLock() while held = %v, %v, want false, nil", held, err)
	}
	if err := first.Rollback(); err != nil {
		t.Fatalf("first Rollback(): %v", err)
	}
	if held, err := summary.TryLock(ctx, txStore{second}); err != nil || !held {
		t.Fatalf("second TryLock() after release = %v, %v, want true, nil", held, err)
	}
	_ = second.Rollback()

	// A crashed holder: kill the backend that holds the lock.
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn(): %v", err)
	}
	defer func() { _ = conn.Close() }()
	crashed, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx(): %v", err)
	}
	var pid int
	if err := crashed.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("pg_backend_pid(): %v", err)
	}
	if held, err := summary.TryLock(ctx, txStore{crashed}); err != nil || !held {
		t.Fatalf("crashing holder TryLock() = %v, %v, want true, nil", held, err)
	}
	var terminated bool
	if err := database.QueryRowContext(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("pg_terminate_backend(%d) = %v, %v", pid, terminated, err)
	}
	// Release the dead connection so closing the pool at cleanup does not wait
	// for the abandoned transaction until the test context expires.
	_ = crashed.Rollback()
	replacement := beginTx(ctx, t, database)
	defer func() { _ = replacement.Rollback() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		held, err := summary.TryLock(ctx, txStore{replacement})
		if err != nil {
			t.Fatalf("replacement TryLock(): %v", err)
		}
		if held {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the advisory lock stayed held 10s after its backend was terminated")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func beginTx(ctx context.Context, t *testing.T, database *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx(): %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func mustUpsert(ctx context.Context, t *testing.T, store summary.Store, row summary.Row, wantAdvanced bool) {
	t.Helper()
	advanced, err := store.Upsert(ctx, row)
	if err != nil {
		t.Fatalf("Upsert(as_of=%v) error = %v", row.AsOf, err)
	}
	if advanced != wantAdvanced {
		t.Fatalf("Upsert(as_of=%v) advanced = %v, want %v", row.AsOf, advanced, wantAdvanced)
	}
}

// assertStored reads the row back and compares every stored field, including
// the payload, so a guard-rejected write that still changed a column fails.
func assertStored(ctx context.Context, t *testing.T, store summary.Store, want summary.Row) {
	t.Helper()
	got, err := store.Read(ctx, want.ModelKey)
	if err != nil {
		t.Fatalf("Read(): %v", err)
	}
	if !got.AsOf.Equal(want.AsOf) {
		t.Fatalf("stored as_of = %v, want %v", got.AsOf, want.AsOf)
	}
	if got.SchemaVersion != want.SchemaVersion || got.SourceSHA256 != want.SourceSHA256 ||
		got.RowCount != want.RowCount || got.PassDuration != want.PassDuration {
		t.Fatalf("stored row = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(got.Entries, want.Entries) {
		t.Fatalf("stored entries = %#v, want %#v", got.Entries, want.Entries)
	}
	if got.ComputedAt.IsZero() {
		t.Fatal("stored computed_at is zero, want the database clock")
	}
}
