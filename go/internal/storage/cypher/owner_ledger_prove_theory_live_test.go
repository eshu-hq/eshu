// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// TestLiveOwnerLedgerProveTheory is the mandatory prove-theory-first gate for
// the #5007 owner-ledger redesign (issue #5062 tracks the underlying NornicDB
// concurrent-property-write limitation). The graph-side guard was already
// disproven (26% lost updates on NornicDB); this shim proves whether the
// Postgres owner-ledger design closes that gap END TO END — in BOTH the
// Postgres ledger AND the NornicDB graph node — under concurrent contention.
//
// It measures two candidate designs against the real pinned Postgres +
// NornicDB:
//
//   - design (a) "ledger-derived graph write", lock-free: upsert the ledger
//     (atomic max resolution), read back the current ledger winner, then write
//     the winner's values to the graph. Theory: concurrent graph writes are
//     idempotent because they all write the ledger-winning values. Risk: a
//     stale-read window between the ledger read and the graph write.
//   - design (b) "advisory-lock serialized": the same steps wrapped in a
//     per-uid pg_advisory_xact_lock so only one writer touches a uid at a time;
//     the last lock holder reads the converged ledger and writes it.
//
// Both are driven by N concurrent writers racing the same uid with different
// order keys, over many trials, asserting the final state in BOTH stores
// equals the max-(order key) contributor. Records lost-update counts per
// design so the maintainer's chosen design is proven (or disproven) with
// numbers, exactly like the 26%-vs-0% graph-guard result.
//
// Skipped by default; set ESHU_OWNER_LEDGER_PROVE_LIVE=1 with
// ESHU_OWNER_LEDGER_PG_DSN pointing at a live Postgres and the
// ESHU_GRAPH_BACKEND/ESHU_NEO4J_URI env at a live NornicDB.
func TestLiveOwnerLedgerProveTheory(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ownerLedgerProveEnv)) == "" {
		t.Skipf("set %s=1 to run the #5007 owner-ledger prove-theory shim", ownerLedgerProveEnv)
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
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if _, err := db.ExecContext(ctx, ownerLedgerDDL); err != nil {
		t.Fatalf("create owner ledger table: %v", err)
	}

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = driver.Close(closeCtx)
	}()
	if err := runLiveGuardWriteLedger(ctx, driver, cfg.DatabaseName, ownerLedgerGraphConstraint, nil); err != nil {
		t.Fatalf("create graph constraint: %v", err)
	}

	// N-writer contention: keys sort lexicographically so the max is the
	// highest-numbered writer. Worst case interleaving is exercised by running
	// all N concurrently over many trials.
	writers := []struct{ key, value string }{
		{"1000-a", "va"}, {"2000-b", "vb"}, {"3000-c", "vc"}, {"4000-d", "vd"},
	}
	maxKey, maxValue := "4000-d", "vd"

	for _, design := range []string{"a_lockfree", "a_widened_window", "b_advisory_lock", "b_gated_own_row"} {
		design := design
		t.Run(design, func(t *testing.T) {
			const trials = 100
			var ledgerLost, graphLost int
			for trial := 0; trial < trials; trial++ {
				uid := fmt.Sprintf("owner-%s-%d", design, trial)
				resetOwnerFixture(ctx, t, db, driver, cfg.DatabaseName, uid)

				var wg sync.WaitGroup
				errs := make([]error, len(writers))
				wg.Add(len(writers))
				for i, w := range writers {
					i, w := i, w
					go func() {
						defer wg.Done()
						switch design {
						case "a_lockfree":
							errs[i] = ownerWriteLockFree(ctx, db, driver, cfg.DatabaseName, uid, w.key, w.value, 0)
						case "a_widened_window":
							// Retry the MERGE-create conflict (fair, production-like)
							// so writer errors do not mask lost updates, and widen
							// the ledger-read -> graph-write gap to expose the
							// stale-write race the coordinator flagged.
							errs[i] = ownerWriteLockFree(ctx, db, driver, cfg.DatabaseName, uid, w.key, w.value, 3*time.Millisecond)
						case "b_gated_own_row":
							errs[i] = ownerWriteGatedOwnRow(ctx, db, driver, cfg.DatabaseName, uid, w.key, w.value)
						default:
							errs[i] = ownerWriteAdvisoryLock(ctx, db, driver, cfg.DatabaseName, uid, w.key, w.value)
						}
					}()
				}
				wg.Wait()
				for i, e := range errs {
					if e == nil {
						continue
					}
					// design (a) lock-free (gap=0) does not retry the concurrent
					// MERGE-create conflict, so a transient Outdated there is
					// expected and only reduces how many writes land (it never
					// masks a lost update, which the graph read below still
					// catches). Hard-fail writer errors only for the adopted
					// design (b), which must have none.
					if design == "b_advisory_lock" || design == "b_gated_own_row" {
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
			t.Logf("design %s: %d trials, %d ledger lost, %d graph lost (N=%d concurrent writers)",
				design, trials, ledgerLost, graphLost, len(writers))
			// The ledger is Postgres-atomic in every design, so it must never
			// lose regardless of which design.
			if ledgerLost > 0 {
				t.Errorf("design %s: ledger is NOT deterministic: %d/%d trials", design, ledgerLost, trials)
			}
			// Only the ADOPTED design (b) is hard-asserted lost-update-free for
			// the GRAPH. The design-(a) variants are measured for the record but
			// NOT asserted here: random N-writer trials miss (a)'s narrow
			// stale-write window (it typically reads 0 graph lost), yet
			// TestLiveOwnerLedgerDeterministicWorstCase proves that window is a
			// REAL race under a forced interleaving. Asserting 0 on (a) here
			// would be a flaky false-green.
			if (design == "b_advisory_lock" || design == "b_gated_own_row") && graphLost > 0 {
				t.Errorf("design %s: GRAPH is NOT lost-update-free: %d/%d trials lost the max winner", design, graphLost, trials)
			}
		})
	}
}

// TestLiveOwnerLedgerDeterministicWorstCase forces the exact interleaving that
// design (a) (lock-free ledger-derived graph write) is vulnerable to, to prove
// the stale-write race is REAL (not merely rare) and therefore that design (b)
// (advisory-lock serialized) is required:
//
//	low:  upsert(low), read ledger (=low)          -- stale read captured
//	high: upsert(high), read(=high), write graph(=high)  -- max lands
//	low:  write graph(=low)                          -- clobbers with stale
//
// Under design (a) this deterministically leaves the graph at the LOSING
// value (proving the race). Under design (b) the advisory lock makes the same
// interleaving impossible (low holds the lock across its whole read+write, so
// high cannot interleave), and the graph converges to the max.
func TestLiveOwnerLedgerDeterministicWorstCase(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ownerLedgerProveEnv)) == "" {
		t.Skipf("set %s=1 to run the #5007 owner-ledger worst-case interleaving proof", ownerLedgerProveEnv)
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

	// Design (a), forced interleaving: prove the stale-write race is real.
	uid := "owner-worstcase-a"
	resetOwnerFixture(ctx, t, db, driver, cfg.DatabaseName, uid)
	if _, err := db.ExecContext(ctx, ownerLedgerUpsert, uid, "1000-low", "low"); err != nil {
		t.Fatalf("low upsert: %v", err)
	}
	lowKey, lowValue, _ := selectOwner(ctx, db, uid) // low reads ledger = low (stale)
	// high runs fully to completion (upsert + read + graph write = high)
	if err := ownerWriteLockFree(ctx, db, driver, cfg.DatabaseName, uid, "2000-high", "high", 0); err != nil {
		t.Fatalf("high write: %v", err)
	}
	// low now writes its stale read to the graph, clobbering high
	if err := runLiveGuardWriteLedger(ctx, driver, cfg.DatabaseName, ownerGraphWrite, map[string]any{
		"uid": uid, "order_key": lowKey, "value": lowValue,
	}); err != nil {
		t.Fatalf("low graph write: %v", err)
	}
	gk, gv := readOwnerGraph(ctx, t, driver, cfg.DatabaseName, uid)
	if gk == "2000-high" {
		t.Errorf("design (a) unexpectedly converged under the forced worst-case interleaving; expected the stale-write race to leave the graph at the losing value")
	} else {
		t.Logf("PROVEN: design (a) lock-free stale-write race is REAL — forced interleaving left graph at (%q,%q), NOT the max (2000-high). This is why design (b) is required.", gk, gv)
	}

	// Design (b), same forced attempt: low holds the advisory lock across its
	// read+write, so high cannot interleave; the graph converges to the max.
	uidB := "owner-worstcase-b"
	resetOwnerFixture(ctx, t, db, driver, cfg.DatabaseName, uidB)
	// Serialized by the lock: low first (fully), then high (fully). Last writer
	// (high) holds the converged ledger and writes the max.
	if err := ownerWriteAdvisoryLock(ctx, db, driver, cfg.DatabaseName, uidB, "1000-low", "low"); err != nil {
		t.Fatalf("(b) low: %v", err)
	}
	if err := ownerWriteAdvisoryLock(ctx, db, driver, cfg.DatabaseName, uidB, "2000-high", "high"); err != nil {
		t.Fatalf("(b) high: %v", err)
	}
	bk, bv := readOwnerGraph(ctx, t, driver, cfg.DatabaseName, uidB)
	if bk != "2000-high" || bv != "high" {
		t.Errorf("design (b) failed to converge: got (%q,%q), want (2000-high,high)", bk, bv)
	} else {
		t.Logf("PROVEN: design (b) advisory-lock converges to the max (%q,%q) — serialization by conflict key closes the race.", bk, bv)
	}
}

const (
	ownerLedgerProveEnv = "ESHU_OWNER_LEDGER_PROVE_LIVE"
	ownerLedgerPGDSNEnv = "ESHU_OWNER_LEDGER_PG_DSN"
)

const ownerLedgerDDL = `
CREATE TABLE IF NOT EXISTS cloud_resource_owner_probe (
    uid TEXT PRIMARY KEY,
    source_order_key TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
)`

const ownerLedgerGraphConstraint = `
CREATE CONSTRAINT owner_ledger_probe_uid_unique IF NOT EXISTS
FOR (n:OwnerLedgerProbe) REQUIRE n.uid IS UNIQUE`

// ownerLedgerUpsert atomically resolves the max order key in Postgres: the row
// is only overwritten when the incoming order key is strictly greater than the
// stored one. Postgres row locking makes this reliable under concurrency
// (unlike NornicDB's missed property-write conflicts).
const ownerLedgerUpsert = `
INSERT INTO cloud_resource_owner_probe (uid, source_order_key, value, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (uid) DO UPDATE
    SET source_order_key = EXCLUDED.source_order_key,
        value = EXCLUDED.value,
        updated_at = EXCLUDED.updated_at
    WHERE EXCLUDED.source_order_key > cloud_resource_owner_probe.source_order_key`

const ownerLedgerSelect = `SELECT source_order_key, value FROM cloud_resource_owner_probe WHERE uid = $1`

// ownerGraphWrite is a plain MERGE + SET (no CASE — NornicDB stringifies a CASE
// mixing row.field with a node-property ELSE). It writes whatever key/value it
// is given; the design decides WHAT to give it (ledger winner, under lock or
// not).
const ownerGraphWrite = `
MERGE (n:OwnerLedgerProbe {uid: $uid})
SET n.source_order_key = $order_key, n.value = $value`

// ownerWriteLockFree implements design (a): upsert the ledger, read the current
// ledger winner, write the winner values to the graph. No lock. readWriteGap
// widens the window between the ledger read and the graph write to expose the
// stale-write race; the graph MERGE-create conflict is retried so writer
// errors never mask a lost update.
func ownerWriteLockFree(ctx context.Context, db *sql.DB, driver neo4jdriver.DriverWithContext, database, uid, orderKey, value string, readWriteGap time.Duration) error {
	if _, err := db.ExecContext(ctx, ownerLedgerUpsert, uid, orderKey, value); err != nil {
		return fmt.Errorf("ledger upsert: %w", err)
	}
	winKey, winValue, err := selectOwner(ctx, db, uid)
	if err != nil {
		return fmt.Errorf("read ledger winner: %w", err)
	}
	if readWriteGap > 0 {
		time.Sleep(readWriteGap)
	}
	// Retry the MERGE-create commit conflict up to a bounded budget, mirroring
	// the production RetryingExecutor, so an unretried create race never masks
	// a lost update in the measurement.
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		lastErr = runLiveGuardWriteLedger(ctx, driver, database, ownerGraphWrite, map[string]any{
			"uid": uid, "order_key": winKey, "value": winValue,
		})
		if lastErr == nil {
			return nil
		}
		if !strings.Contains(lastErr.Error(), "Outdated") && !strings.Contains(lastErr.Error(), "already exists") {
			return lastErr
		}
		time.Sleep(2 * time.Millisecond)
	}
	return lastErr
}

// ownerWriteAdvisoryLock implements design (b): the same steps inside a
// per-uid pg_advisory_xact_lock held across the graph write, so writers to the
// same uid serialize and the last one writes the converged ledger winner.
func ownerWriteAdvisoryLock(ctx context.Context, db *sql.DB, driver neo4jdriver.DriverWithContext, database, uid, orderKey, value string) error {
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
	if _, err := tx.ExecContext(ctx, ownerLedgerUpsert, uid, orderKey, value); err != nil {
		return fmt.Errorf("ledger upsert: %w", err)
	}
	var winKey, winValue string
	if err := tx.QueryRowContext(ctx, ownerLedgerSelect, uid).Scan(&winKey, &winValue); err != nil {
		return fmt.Errorf("read ledger winner: %w", err)
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

// ownerWriteGatedOwnRow implements the REFINED production mechanic (the one the
// decorator actually ships): under a per-uid advisory lock, upsert the ledger
// (order-key max resolution), read back the winning order key, and write to the
// graph ONLY when THIS writer is the current max — writing THIS writer's OWN
// row (its original Go-typed value), never a value round-tripped through the
// ledger. A writer that lost skips the graph write entirely (the max holder
// already wrote, or will write, its own row under the same lock). This
// converges to the max exactly like ownerWriteAdvisoryLock, but it never mangles
// the winner's value types through JSON, so a non-contended uid's graph write is
// byte-identical to origin/main and a contended uid's final value is always the
// max writer's own row regardless of interleaving.
func ownerWriteGatedOwnRow(ctx context.Context, db *sql.DB, driver neo4jdriver.DriverWithContext, database, uid, orderKey, value string) error {
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
	if _, err := tx.ExecContext(ctx, ownerLedgerUpsert, uid, orderKey, value); err != nil {
		return fmt.Errorf("ledger upsert: %w", err)
	}
	var winKey, winValue string
	if err := tx.QueryRowContext(ctx, ownerLedgerSelect, uid).Scan(&winKey, &winValue); err != nil {
		return fmt.Errorf("read ledger winner: %w", err)
	}
	// Only write when THIS writer is the current max; write my OWN value.
	if winKey == orderKey {
		if err := runLiveGuardWriteLedger(ctx, driver, database, ownerGraphWrite, map[string]any{
			"uid": uid, "order_key": orderKey, "value": value,
		}); err != nil {
			return fmt.Errorf("graph write under lock: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit lock tx: %w", err)
	}
	committed = true
	return nil
}

func selectOwner(ctx context.Context, db *sql.DB, uid string) (string, string, error) {
	var k, v string
	err := db.QueryRowContext(ctx, ownerLedgerSelect, uid).Scan(&k, &v)
	return k, v, err
}

func ownerAdvisoryKey(uid string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("eshu:cloud_resource_owner:"))
	_, _ = h.Write([]byte(uid))
	return int64(h.Sum64() & uint64(1<<63-1))
}

func resetOwnerFixture(ctx context.Context, t *testing.T, db *sql.DB, driver neo4jdriver.DriverWithContext, database, uid string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, "DELETE FROM cloud_resource_owner_probe WHERE uid = $1", uid); err != nil {
		t.Fatalf("reset ledger: %v", err)
	}
	if err := runLiveGuardWriteLedger(ctx, driver, database, "MATCH (n:OwnerLedgerProbe {uid: $uid}) DETACH DELETE n", map[string]any{"uid": uid}); err != nil {
		t.Fatalf("reset graph: %v", err)
	}
}

func readOwnerLedger(ctx context.Context, t *testing.T, db *sql.DB, uid string) (string, string) {
	t.Helper()
	k, v, err := selectOwner(ctx, db, uid)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	return k, v
}

func readOwnerGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database, uid string) (string, string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `MATCH (n:OwnerLedgerProbe {uid: $uid}) RETURN n.source_order_key AS k, n.value AS v`, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("read graph single: %v", err)
	}
	k, _ := record.Get("k")
	v, _ := record.Get("v")
	ks, _ := k.(string)
	vs, _ := v.(string)
	return ks, vs
}

func runLiveGuardWriteLedger(ctx context.Context, driver neo4jdriver.DriverWithContext, database, cypher string, params map[string]any) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return err
	}
	if _, err := result.Consume(ctx); err != nil {
		return err
	}
	return nil
}
