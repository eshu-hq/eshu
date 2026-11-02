// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const winnersLiveReturningCVE = "CVE-2026-9700"

// winnersLiveBlockedResweeps counts resweep backends waiting on a heavyweight
// lock. pg_stat_activity truncates the query text, so it matches the statement's
// leading CTE, which sits well inside the default track_activity_query_size.
func winnersLiveBlockedResweeps(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%winners_now AS%'`).Scan(&n); err != nil {
		t.Fatalf("count blocked resweeps: %v", err)
	}
	return n
}

// winnersLiveAwaitBlocked polls until at least want resweeps are lock-blocked or
// done reports that the awaited resweep already returned.
func winnersLiveAwaitBlocked(t *testing.T, ctx context.Context, db *sql.DB, want int, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return
		default:
		}
		if winnersLiveBlockedResweeps(t, ctx, db) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %d lock-blocked resweep(s) within 30s", want)
}

type winnersLiveOutcome struct {
	result supplychaincore.SupplyChainImpactWinnersResweep
	err    error
}

// winnersLiveStart runs one resweep on its own goroutine. The returned channel
// carries the outcome and is closed afterwards, so a receive can also serve as
// a "done" signal.
func winnersLiveStart(ctx context.Context, store postgres.SupplyChainImpactWinnersStore, at time.Time) <-chan winnersLiveOutcome {
	out := make(chan winnersLiveOutcome, 1)
	go func() {
		defer close(out)
		result, err := store.ResweepWinners(ctx, at)
		out <- winnersLiveOutcome{result: result, err: err}
	}()
	return out
}

// winnersLiveSeedOverlap stores three winners and returns the fact id of the one
// the overlap proofs retire and bring back.
func winnersLiveSeedOverlap(t *testing.T, ctx context.Context, db *sql.DB, store postgres.SupplyChainImpactWinnersStore, at time.Time) string {
	t.Helper()
	winnersLiveSeedScope(t, ctx, db, winnersLiveSourceScope)
	winnersLiveSeedFact(t, ctx, db, "f-returning", winnersLiveSourceScope, winnersLiveFinding(winnersLiveReturningCVE, "pkg:npm/returning", 5, nil))
	winnersLiveSeedFact(t, ctx, db, "f-steady-a", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9701", "pkg:npm/steady-a", 4, nil))
	winnersLiveSeedFact(t, ctx, db, "f-steady-b", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9702", "pkg:npm/steady-b", 3, nil))
	if got, err := store.ResweepWinners(ctx, at); err != nil || got.Inserted != 3 {
		t.Fatalf("seed resweep = %+v, %v; want 3 inserted", got, err)
	}
	return "f-returning"
}

// winnersLivePinOnReturningWinner holds a row lock on the returning winner. A
// resweep that retires that winner blocks on the lock, which pins it after its
// snapshot and before its commit. Releasing commits the holder.
func winnersLivePinOnReturningWinner(t *testing.T, ctx context.Context, db *sql.DB) (release func()) {
	t.Helper()
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	var one int
	if err := holder.QueryRowContext(ctx, `SELECT 1 FROM supply_chain_impact_canonical_winners WHERE cve_id = $1 FOR UPDATE`, winnersLiveReturningCVE).Scan(&one); err != nil {
		t.Fatalf("lock returning winner: %v", err)
	}
	var once sync.Once
	release = func() { once.Do(func() { _ = holder.Commit() }) }
	t.Cleanup(release)
	return release
}

func winnersLiveReturningPresent(t *testing.T, ctx context.Context, db *sql.DB) bool {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE cve_id = $1`, winnersLiveReturningCVE).Scan(&n); err != nil {
		t.Fatalf("read returning winner: %v", err)
	}
	return n == 1
}

// TestSupplyChainImpactWinnersResweepOverlapNeverStampsAMissingWinnerLive is the
// #9774 lost-lease interleaving. Resweep A is pinned after its snapshot with a
// winner retired, the winner's fact comes back, and overlapping resweep B starts
// on a newer snapshot. Without the single-flight lock, B compares against the
// pre-A table, sees the winner stored and unchanged, writes nothing, and stamps
// a fresh watermark over a table A then empties of that winner: the read would
// report a winner missing as fresh. The rule this pins: a resweep that cannot
// see the last commit must not stamp, so the watermark stays honest and the next
// tick restores the winner like any other fact change.
func TestSupplyChainImpactWinnersResweepOverlapNeverStampsAMissingWinnerLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9774 winners overlap proof")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tA, tB, tNext := t0.Add(time.Minute), t0.Add(2*time.Minute), t0.Add(3*time.Minute)
	fact := winnersLiveSeedOverlap(t, ctx, db, store, t0)

	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = $1`, fact)
	release := winnersLivePinOnReturningWinner(t, ctx, db)
	a := winnersLiveStart(ctx, store, tA)
	winnersLiveAwaitBlocked(t, ctx, db, 1, nil)

	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = FALSE WHERE fact_id = $1`, fact)
	b := winnersLiveStart(ctx, store, tB)
	bDone := make(chan struct{})
	var bOutcome winnersLiveOutcome
	go func() {
		bOutcome = <-b
		close(bDone)
	}()
	winnersLiveAwaitBlocked(t, ctx, db, 2, bDone)

	release()
	aOutcome := <-a
	<-bDone

	if aOutcome.err != nil || bOutcome.err != nil {
		t.Fatalf("overlapping resweeps must not fail or deadlock: a=%v b=%v", aOutcome.err, bOutcome.err)
	}
	if aOutcome.result.Deleted != 1 || aOutcome.result.Skipped {
		t.Fatalf("resweep A = %+v, want the retired winner deleted", aOutcome.result)
	}
	present := winnersLiveReturningPresent(t, ctx, db)
	watermark := winnersLiveWatermark(t, ctx, db)
	if !present && watermark.After(tA) {
		t.Fatalf("watermark %v is newer than A's stamp %v while a winner visible to B's snapshot is missing: "+
			"the read reports it fresh (B = %+v)", watermark, tA, bOutcome.result)
	}
	if !bOutcome.result.Skipped {
		t.Fatalf("resweep B = %+v, want Skipped while A is in flight", bOutcome.result)
	}
	if !watermark.Equal(tA) {
		t.Fatalf("watermark = %v, want A's stamp %v (a skipped resweep stamps nothing)", watermark, tA)
	}

	// The next tick sees A's commit and restores the winner.
	next, err := store.ResweepWinners(ctx, tNext)
	if err != nil {
		t.Fatalf("next resweep: %v", err)
	}
	if next.Inserted != 1 || next.Unchanged != 2 || next.Updated != 0 || next.Deleted != 0 || next.Skipped {
		t.Fatalf("next resweep = %+v, want the returning winner inserted and two unchanged", next)
	}
	if !winnersLiveReturningPresent(t, ctx, db) {
		t.Fatal("the next tick did not restore the returning winner")
	}
	winnersLiveAssertEqualsRecompute(t, ctx, db, store, tNext.Add(time.Minute))
}

// winnersLiveAssertEqualsRecompute wipes the read model, rebuilds it, and
// requires the rebuilt table to equal the incremental one, ignoring write times.
func winnersLiveAssertEqualsRecompute(t *testing.T, ctx context.Context, db *sql.DB, store postgres.SupplyChainImpactWinnersStore, at time.Time) {
	t.Helper()
	incremental := winnersLiveRead(t, ctx, db)
	winnersLiveExec(t, ctx, db, `DELETE FROM supply_chain_impact_canonical_winners`)
	if got, err := store.ResweepWinners(ctx, at); err != nil || got.Skipped {
		t.Fatalf("recompute resweep = %+v, %v", got, err)
	}
	fresh := winnersLiveRead(t, ctx, db)
	if len(fresh) != len(incremental) {
		t.Fatalf("fresh recompute has %d winners, incremental table had %d", len(fresh), len(incremental))
	}
	for key, row := range incremental {
		if got, ok := fresh[key]; !ok || got.content != row.content {
			t.Errorf("winner %s differs from a fresh recompute:\nincremental %s\nfresh       %s", key, row.content, got.content)
		}
	}
}

// TestSupplyChainImpactWinnersResweepKilledHolderFreesTheResweepLive proves the
// single-flight lock cannot wedge the maintainer: when the backend of an
// in-flight resweep dies, its error reaches the caller, nothing it did survives,
// and the next resweep runs at once with no TTL to wait out.
func TestSupplyChainImpactWinnersResweepKilledHolderFreesTheResweepLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9774 winners killed-holder proof")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	fact := winnersLiveSeedOverlap(t, ctx, db, store, t0)
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = $1`, fact)
	release := winnersLivePinOnReturningWinner(t, ctx, db)
	a := winnersLiveStart(ctx, store, t0.Add(time.Minute))
	winnersLiveAwaitBlocked(t, ctx, db, 1, nil)

	var killed bool
	if err := db.QueryRowContext(ctx, `
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%winners_now AS%'`).Scan(&killed); err != nil || !killed {
		t.Fatalf("terminate the in-flight resweep: %v %v", killed, err)
	}
	if outcome := <-a; outcome.err == nil {
		t.Fatalf("killed resweep = %+v, want an error", outcome.result)
	}
	release()
	if got := winnersLiveWatermark(t, ctx, db); !got.Equal(t0) {
		t.Fatalf("watermark = %v after the killed resweep, want %v", got, t0)
	}
	if !winnersLiveReturningPresent(t, ctx, db) {
		t.Fatal("the killed resweep's delete survived")
	}

	next, err := store.ResweepWinners(ctx, t0.Add(2*time.Minute))
	if err != nil || next.Skipped || next.Deleted != 1 {
		t.Fatalf("resweep after the kill = %+v, %v; want it to run and delete the retired winner", next, err)
	}
}

// TestSupplyChainImpactWinnersResweepReportsACommitFailureLive proves a commit
// failure reaches the caller. The single result row arrives before the commit,
// so a resweep that returns on that row reports success for a rolled-back tick.
func TestSupplyChainImpactWinnersResweepReportsACommitFailureLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9774 winners commit-failure proof")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	fact := winnersLiveSeedOverlap(t, ctx, db, store, t0)
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = $1`, fact)
	winnersLiveExec(t, ctx, db, `
CREATE FUNCTION winners_refuse_commit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'winners commit refused'; END $$`)
	winnersLiveExec(t, ctx, db, `
CREATE CONSTRAINT TRIGGER winners_refuse_commit AFTER UPDATE ON supply_chain_impact_winners_materialization
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION winners_refuse_commit()`)

	got, err := store.ResweepWinners(ctx, t0.Add(time.Minute))
	if err == nil {
		t.Fatalf("resweep = %+v with a nil error, want the commit failure", got)
	}
	if !winnersLiveReturningPresent(t, ctx, db) {
		t.Fatal("the rolled-back resweep's delete survived")
	}
	if wm := winnersLiveWatermark(t, ctx, db); !wm.Equal(t0) {
		t.Fatalf("watermark = %v after a failed commit, want %v", wm, t0)
	}

	// The failed tick must leave nothing behind that blocks the next one.
	winnersLiveExec(t, ctx, db, `DROP TRIGGER winners_refuse_commit ON supply_chain_impact_winners_materialization`)
	next, err := store.ResweepWinners(ctx, t0.Add(2*time.Minute))
	if err != nil || next.Skipped || next.Deleted != 1 {
		t.Fatalf("resweep after the failed commit = %+v, %v; want it to run and delete the retired winner", next, err)
	}
}

// TestSupplyChainImpactWinnersResweepWatermarkReadsTheCommittedStampLive proves the read
// behind the age gauge: no value before the first resweep, the stamp after it.
func TestSupplyChainImpactWinnersResweepWatermarkReadsTheCommittedStampLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9774 winners watermark read proof")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	if _, ok, err := store.Watermark(ctx); err != nil || ok {
		t.Fatalf("Watermark() before any resweep: ok=%v err=%v, want no value", ok, err)
	}
	stamp := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if _, err := store.ResweepWinners(ctx, stamp); err != nil {
		t.Fatalf("resweep: %v", err)
	}
	if got, ok, err := store.Watermark(ctx); err != nil || !ok || !got.Equal(stamp) {
		t.Fatalf("Watermark() = %v, %v, %v; want %v", got, ok, err, stamp)
	}
}

// TestSupplyChainImpactWinnersResweepConvergesUnderContentionLive runs several
// looping resweeps against a churn writer, the shape of N replicas whose leases
// keep lapsing. No resweep may fail or deadlock, and one settle tick must leave
// the table equal to a fresh recompute.
func TestSupplyChainImpactWinnersResweepConvergesUnderContentionLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9774 winners contention proof")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	const winners, sweepers, sweepsEach = 200, 4, 15
	winnersLiveSeedScope(t, ctx, db, winnersLiveSourceScope)
	for i := range winners {
		id := fmt.Sprintf("f-churn-%d", i)
		winnersLiveSeedFact(t, ctx, db, id, winnersLiveSourceScope, winnersLiveFinding(fmt.Sprintf("CVE-2026-%d", 10000+i), "pkg:npm/churn", 1+i%9, nil))
	}

	// The writer stops on a signal between statements, never by cancelling one:
	// ExecContext returns on cancel without waiting for the server, so a statement
	// could still commit after the writer "stopped", and the settled table would
	// then disagree with the recompute for a reason that has nothing to do with
	// the resweep.
	var stopWriter atomic.Bool
	var writerErr error
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := 0; !stopWriter.Load(); i++ {
			fact := fmt.Sprintf("f-churn-%d", i%winners)
			if _, err := db.ExecContext(ctx, `UPDATE fact_records SET is_tombstone = NOT is_tombstone WHERE fact_id = $1`, fact); err != nil {
				writerErr = err
				return
			}
		}
	}()

	var committed, skipped atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, sweepers*sweepsEach)
	stamp := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for s := range sweepers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range sweepsEach {
				result, err := store.ResweepWinners(ctx, stamp.Add(time.Duration(s*sweepsEach+i)*time.Second))
				switch {
				case err != nil:
					errs <- err
				case result.Skipped:
					skipped.Add(1)
				default:
					committed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	stopWriter.Store(true)
	<-writerDone
	close(errs)
	for err := range errs {
		t.Errorf("resweep under contention: %v", err)
	}
	if writerErr != nil {
		t.Fatalf("churn writer: %v", writerErr)
	}
	if committed.Load() == 0 {
		t.Fatal("no resweep committed under contention")
	}
	t.Logf("resweeps committed=%d skipped=%d", committed.Load(), skipped.Load())

	settle := stamp.Add(time.Hour)
	if got, err := store.ResweepWinners(ctx, settle); err != nil || got.Skipped {
		t.Fatalf("settle resweep = %+v, %v", got, err)
	}
	winnersLiveAssertEqualsRecompute(t, ctx, db, store, settle.Add(time.Minute))
}
