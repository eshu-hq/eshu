// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// TestLiveOwnerLedgerBatchPerf measures design (b)'s per-batch overhead — a
// batched ledger upsert + per-uid advisory locks + winner read-back, held
// across a batched graph write — against the flat graph-only write baseline on
// a realistic per-generation batch. It reports both wall times so the
// maintainer can confirm the owner-ledger enforcement stays within the
// repo-scale performance contract. It is a measurement, not a pass/fail gate;
// it logs the ratio.
func TestLiveOwnerLedgerBatchPerf(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ownerLedgerProveEnv)) == "" {
		t.Skipf("set %s=1 to run the #5007 owner-ledger batch perf differential", ownerLedgerProveEnv)
	}
	dsn := strings.TrimSpace(os.Getenv(ownerLedgerPGDSNEnv))
	if dsn == "" {
		t.Fatalf("%s is required", ownerLedgerPGDSNEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	const (
		batchRows = 500
		iters     = 20
	)
	uids := make([]string, batchRows)
	for i := range uids {
		uids[i] = fmt.Sprintf("perf-owner-%d", i)
	}
	// prime (cold create) both stores once
	_ = ownerBatchFlatGraph(ctx, driver, cfg.DatabaseName, uids)
	_ = ownerBatchDesignB(ctx, db, driver, cfg.DatabaseName, uids)

	var flatTotal, ledgerTotal time.Duration
	for i := 0; i < iters; i++ {
		s := time.Now()
		if err := ownerBatchFlatGraph(ctx, driver, cfg.DatabaseName, uids); err != nil {
			t.Fatalf("flat batch: %v", err)
		}
		flatTotal += time.Since(s)

		s = time.Now()
		if err := ownerBatchDesignB(ctx, db, driver, cfg.DatabaseName, uids); err != nil {
			t.Fatalf("design-b batch: %v", err)
		}
		ledgerTotal += time.Since(s)
	}
	flatAvg := flatTotal / iters
	ledgerAvg := ledgerTotal / iters
	t.Logf("#5007 owner-ledger batch perf (%d uids x %d warm iters): flat graph-only avg=%s, design(b) ledger+lock+graph avg=%s, ratio=%.2fx, per-row overhead=%s",
		batchRows, iters, flatAvg, ledgerAvg, float64(ledgerAvg)/float64(flatAvg), (ledgerAvg-flatAvg)/time.Duration(batchRows))
}

// ownerBatchFlatGraph is the origin/main-shaped baseline: one UNWIND MERGE+SET
// graph write for the whole batch, no ledger.
func ownerBatchFlatGraph(ctx context.Context, driver neo4jdriver.DriverWithContext, database string, uids []string) error {
	rows := make([]map[string]any, len(uids))
	for i, uid := range uids {
		rows[i] = map[string]any{"uid": uid, "order_key": "1000-x", "value": "v"}
	}
	return runLiveGuardWriteLedger(ctx, driver, database,
		`UNWIND $rows AS row MERGE (n:OwnerLedgerProbe {uid: row.uid}) SET n.source_order_key = row.order_key, n.value = row.value`,
		map[string]any{"rows": rows})
}

// ownerBatchDesignB is design (b) for a whole batch: sorted per-uid advisory
// locks, a batched ledger upsert, a batched winner read-back, then a batched
// graph write of the winner values — all under the locks (one PG tx).
func ownerBatchDesignB(ctx context.Context, db *sql.DB, driver neo4jdriver.DriverWithContext, database string, uids []string) error {
	sorted := append([]string(nil), uids...)
	sort.Strings(sorted)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	// Acquire ALL per-uid advisory locks in ONE round-trip, in sorted key
	// order (the inner ORDER BY materializes the sorted set before the lock
	// function is applied), so concurrent batches with overlapping uids can
	// never deadlock and 500 locks cost one statement, not 500.
	lockKeys := make([]int64, len(sorted))
	for i, uid := range sorted {
		lockKeys[i] = ownerAdvisoryKey(uid)
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(k) FROM (SELECT DISTINCT k FROM unnest($1::bigint[]) AS t(k) ORDER BY k) s`,
		lockKeys); err != nil {
		return err
	}
	// batched upsert via UNNEST
	keys := make([]string, len(sorted))
	vals := make([]string, len(sorted))
	for i := range sorted {
		keys[i] = "1000-x"
		vals[i] = "v"
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO cloud_resource_owner_probe (uid, source_order_key, value, updated_at)
SELECT u, k, v, now() FROM unnest($1::text[], $2::text[], $3::text[]) AS t(u,k,v)
ON CONFLICT (uid) DO UPDATE SET source_order_key = EXCLUDED.source_order_key, value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
  WHERE EXCLUDED.source_order_key > cloud_resource_owner_probe.source_order_key`,
		sorted, keys, vals); err != nil {
		return err
	}
	// batched winner read-back
	rowsRes, err := tx.QueryContext(ctx, `SELECT uid, source_order_key, value FROM cloud_resource_owner_probe WHERE uid = ANY($1::text[])`, sorted)
	if err != nil {
		return err
	}
	graphRows := make([]map[string]any, 0, len(sorted))
	for rowsRes.Next() {
		var uid, k, v string
		if err := rowsRes.Scan(&uid, &k, &v); err != nil {
			_ = rowsRes.Close()
			return err
		}
		graphRows = append(graphRows, map[string]any{"uid": uid, "order_key": k, "value": v})
	}
	_ = rowsRes.Close()
	if err := rowsRes.Err(); err != nil {
		return err
	}
	if err := runLiveGuardWriteLedger(ctx, driver, database,
		`UNWIND $rows AS row MERGE (n:OwnerLedgerProbe {uid: row.uid}) SET n.source_order_key = row.order_key, n.value = row.value`,
		map[string]any{"rows": graphRows}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
