// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// ownerWriteAdvisoryLockReturning is design (b) with the RETURNING
// optimization: per-uid pg_advisory_xact_lock, then the CASE-always-update
// upsert whose RETURNING yields the current winner directly (no separate
// winner read-back), then the graph write of the returned winner — all inside
// the lock-holding transaction, exactly like the proven design (b).
func ownerWriteAdvisoryLockReturning(ctx context.Context, db *sql.DB, driver neo4jdriver.DriverWithContext, database, uid, orderKey, value string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin lock tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1::bigint)", ownerAdvisoryKey(uid)); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	var winKey, winValue string
	if err := tx.QueryRowContext(ctx, ownerLedgerUpsertCaseReturning, uid, orderKey, value).Scan(&winKey, &winValue); err != nil {
		return fmt.Errorf("ledger upsert returning: %w", err)
	}
	if err := runLiveGuardWriteLedger(ctx, driver, database, ownerGraphWrite, map[string]any{
		"uid": uid, "order_key": winKey, "value": winValue,
	}); err != nil {
		return fmt.Errorf("graph write under lock: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit lock tx: %w", err)
	}
	committed = true
	return nil
}

// TestLiveOwnerLedgerReturningCorrectness re-proves design (b)'s
// lost-update-freedom with the RETURNING upsert substituted for the
// upsert+read-back pair: N=4 concurrent writers race the same uid with
// different order keys over 100 trials; the final state in BOTH the ledger and
// the graph must equal the max-order-key contributor, with zero writer errors.
// A faster-but-racy variant would be a disproof, not a win.
func TestLiveOwnerLedgerReturningCorrectness(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ownerLedgerProveEnv)) == "" {
		t.Skipf("set %s=1 to run the #5007 RETURNING correctness re-proof", ownerLedgerProveEnv)
	}
	dsn := strings.TrimSpace(os.Getenv(ownerLedgerPGDSNEnv))
	if dsn == "" {
		t.Fatalf("%s is required", ownerLedgerPGDSNEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, ownerLedgerDDL); err != nil {
		t.Fatalf("create owner ledger table: %v", err)
	}
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		cc, ccl := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccl()
		_ = driver.Close(cc)
	}()
	if err := runLiveGuardWriteLedger(ctx, driver, cfg.DatabaseName, ownerLedgerGraphConstraint, nil); err != nil {
		t.Fatalf("create graph constraint: %v", err)
	}

	writers := []struct{ key, value string }{
		{"1000-a", "va"}, {"2000-b", "vb"}, {"3000-c", "vc"}, {"4000-d", "vd"},
	}
	maxKey, maxValue := "4000-d", "vd"

	const trials = 100
	var ledgerLost, graphLost int
	for trial := 0; trial < trials; trial++ {
		uid := fmt.Sprintf("owner-bret-%d", trial)
		resetOwnerFixture(ctx, t, db, driver, cfg.DatabaseName, uid)

		var wg sync.WaitGroup
		errs := make([]error, len(writers))
		wg.Add(len(writers))
		for i, w := range writers {
			i, w := i, w
			go func() {
				defer wg.Done()
				errs[i] = ownerWriteAdvisoryLockReturning(ctx, db, driver, cfg.DatabaseName, uid, w.key, w.value)
			}()
		}
		wg.Wait()
		for i, e := range errs {
			if e != nil {
				t.Errorf("trial %d writer %d: %v", trial, i, e)
			}
		}

		lk, lv := readOwnerLedger(ctx, t, db, uid)
		if lk != maxKey || lv != maxValue {
			ledgerLost++
		}
		gk, gv := readOwnerGraph(ctx, t, driver, cfg.DatabaseName, uid)
		if gk != maxKey || gv != maxValue {
			graphLost++
			if graphLost <= 3 {
				t.Logf("trial %d graph lost: got (%q,%q) want (%q,%q)", trial, gk, gv, maxKey, maxValue)
			}
		}
	}
	t.Logf("design (b)+RETURNING: %d trials, %d ledger lost, %d graph lost (N=%d concurrent writers)",
		trials, ledgerLost, graphLost, len(writers))
	if ledgerLost > 0 {
		t.Errorf("design (b)+RETURNING: ledger is NOT deterministic: %d/%d trials", ledgerLost, trials)
	}
	if graphLost > 0 {
		t.Errorf("design (b)+RETURNING: GRAPH is NOT lost-update-free: %d/%d trials lost the max winner", graphLost, trials)
	}
}

// TestLiveOwnerLedgerReturningWorstCase re-runs the deterministic worst-case
// interleaving from TestLiveOwnerLedgerDeterministicWorstCase against the
// RETURNING variant, in both directions:
//
//  1. RETURNING WITHOUT the advisory lock (the tempting "the upsert already
//     told me the winner, why lock?" shortcut) — forced interleaving:
//     low case-upserts (RETURNING=low, it is the only row), high completes its
//     full upsert+graph write (graph=high), then low writes its captured
//     RETURNING values to the graph. The graph must end at the LOSING value,
//     proving RETURNING does not close design (a)'s stale-write window and the
//     lock must stay.
//  2. RETURNING WITH the advisory lock (the adopted optimization) — the same
//     sequential low-then-high schedule converges to the max, like design (b).
func TestLiveOwnerLedgerReturningWorstCase(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ownerLedgerProveEnv)) == "" {
		t.Skipf("set %s=1 to run the #5007 RETURNING worst-case interleaving proof", ownerLedgerProveEnv)
	}
	dsn := strings.TrimSpace(os.Getenv(ownerLedgerPGDSNEnv))
	if dsn == "" {
		t.Fatalf("%s is required", ownerLedgerPGDSNEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, ownerLedgerDDL); err != nil {
		t.Fatalf("create owner ledger table: %v", err)
	}
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		cc, ccl := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccl()
		_ = driver.Close(cc)
	}()
	_ = runLiveGuardWriteLedger(ctx, driver, cfg.DatabaseName, ownerLedgerGraphConstraint, nil)

	// (1) RETURNING without the lock: forced stale-write interleaving loses.
	uid := "owner-worstcase-ret-nolock"
	resetOwnerFixture(ctx, t, db, driver, cfg.DatabaseName, uid)
	var lowKey, lowValue string
	if err := db.QueryRowContext(ctx, ownerLedgerUpsertCaseReturning, uid, "1000-low", "low").Scan(&lowKey, &lowValue); err != nil {
		t.Fatalf("low upsert returning: %v", err)
	}
	// high runs fully to completion: upsert RETURNING + graph write = high.
	var highKey, highValue string
	if err := db.QueryRowContext(ctx, ownerLedgerUpsertCaseReturning, uid, "2000-high", "high").Scan(&highKey, &highValue); err != nil {
		t.Fatalf("high upsert returning: %v", err)
	}
	if err := runLiveGuardWriteLedger(ctx, driver, cfg.DatabaseName, ownerGraphWrite, map[string]any{
		"uid": uid, "order_key": highKey, "value": highValue,
	}); err != nil {
		t.Fatalf("high graph write: %v", err)
	}
	// low now writes its captured (stale) RETURNING values to the graph.
	if err := runLiveGuardWriteLedger(ctx, driver, cfg.DatabaseName, ownerGraphWrite, map[string]any{
		"uid": uid, "order_key": lowKey, "value": lowValue,
	}); err != nil {
		t.Fatalf("low graph write: %v", err)
	}
	gk, gv := readOwnerGraph(ctx, t, driver, cfg.DatabaseName, uid)
	if gk == "2000-high" {
		t.Errorf("RETURNING-without-lock unexpectedly converged under the forced worst-case interleaving; expected the stale-write race to leave the graph at the losing value")
	} else {
		t.Logf("PROVEN: RETURNING alone does NOT close the stale-write window — forced interleaving left graph at (%q,%q), not the max. The advisory lock stays.", gk, gv)
	}

	// (2) RETURNING with the lock: converges like design (b).
	uidB := "owner-worstcase-ret-lock"
	resetOwnerFixture(ctx, t, db, driver, cfg.DatabaseName, uidB)
	if err := ownerWriteAdvisoryLockReturning(ctx, db, driver, cfg.DatabaseName, uidB, "1000-low", "low"); err != nil {
		t.Fatalf("(b-RET) low: %v", err)
	}
	if err := ownerWriteAdvisoryLockReturning(ctx, db, driver, cfg.DatabaseName, uidB, "2000-high", "high"); err != nil {
		t.Fatalf("(b-RET) high: %v", err)
	}
	bk, bv := readOwnerGraph(ctx, t, driver, cfg.DatabaseName, uidB)
	if bk != "2000-high" || bv != "high" {
		t.Errorf("design (b)+RETURNING failed to converge: got (%q,%q), want (2000-high,high)", bk, bv)
	} else {
		t.Logf("PROVEN: design (b)+RETURNING converges to the max (%q,%q) — the optimization preserves design (b)'s safety.", bk, bv)
	}
}
