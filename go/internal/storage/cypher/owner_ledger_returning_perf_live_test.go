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
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// Batched RETURNING variants of the design (b) ledger upsert
// (unnest-sourced analogues of the single-row statements in
// owner_ledger_returning_semantics_live_test.go).
const (
	ownerRetBatchUpsertWhere = `
INSERT INTO cloud_resource_owner_probe (uid, source_order_key, value, updated_at)
SELECT u, k, v, now() FROM unnest($1::text[], $2::text[], $3::text[]) AS t(u,k,v)
ON CONFLICT (uid) DO UPDATE
    SET source_order_key = EXCLUDED.source_order_key, value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
    WHERE EXCLUDED.source_order_key > cloud_resource_owner_probe.source_order_key
RETURNING uid, source_order_key, value`

	ownerRetBatchUpsertCase = `
INSERT INTO cloud_resource_owner_probe (uid, source_order_key, value, updated_at)
SELECT u, k, v, now() FROM unnest($1::text[], $2::text[], $3::text[]) AS t(u,k,v)
ON CONFLICT (uid) DO UPDATE
    SET source_order_key = CASE WHEN EXCLUDED.source_order_key > cloud_resource_owner_probe.source_order_key
            THEN EXCLUDED.source_order_key ELSE cloud_resource_owner_probe.source_order_key END,
        value = CASE WHEN EXCLUDED.source_order_key > cloud_resource_owner_probe.source_order_key
            THEN EXCLUDED.value ELSE cloud_resource_owner_probe.value END,
        updated_at = now()
RETURNING uid, source_order_key, value`
)

// ownerRetStages splits one design-(b)-family batch into its cost stages so
// the perf report shows where the overhead over the flat floor actually lives.
type ownerRetStages struct{ lock, upsert, readback, graph, commit time.Duration }

func (s ownerRetStages) add(o ownerRetStages) ownerRetStages {
	return ownerRetStages{s.lock + o.lock, s.upsert + o.upsert, s.readback + o.readback, s.graph + o.graph, s.commit + o.commit}
}

func (s ownerRetStages) div(n int) ownerRetStages {
	d := time.Duration(n)
	return ownerRetStages{s.lock / d, s.upsert / d, s.readback / d, s.graph / d, s.commit / d}
}

// ownerRetBatch runs one design-(b)-family batch for the given mechanic:
//
//	"baseline":  WHERE-guarded upsert + unconditional winner read-back (the proven 2.28x shape)
//	"ret_where": WHERE-guarded upsert RETURNING + fallback read-back only for uids RETURNING omitted
//	"ret_case":  CASE-always-update upsert RETURNING (no read-back ever)
//
// All three: one PG tx, sorted batched advisory locks first, graph write of the
// winner values under the locks, commit releases.
func ownerRetBatch(ctx context.Context, db *sql.DB, driver neo4jdriver.DriverWithContext, database, mechanic string, uids []string, key, value string) (ownerRetStages, error) {
	var st ownerRetStages
	sorted := append([]string(nil), uids...)
	sort.Strings(sorted)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return st, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	s := time.Now()
	lockKeys := make([]int64, len(sorted))
	for i, uid := range sorted {
		lockKeys[i] = ownerAdvisoryKey(uid)
	}
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(k) FROM (SELECT DISTINCT k FROM unnest($1::bigint[]) AS t(k) ORDER BY k) s`,
		lockKeys); err != nil {
		return st, err
	}
	st.lock = time.Since(s)

	keys := make([]string, len(sorted))
	vals := make([]string, len(sorted))
	for i := range sorted {
		keys[i] = key
		vals[i] = value
	}

	winners := make(map[string][2]string, len(sorted))
	scanRows := func(rows *sql.Rows) error {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var uid, k, v string
			if err := rows.Scan(&uid, &k, &v); err != nil {
				return err
			}
			winners[uid] = [2]string{k, v}
		}
		return rows.Err()
	}

	s = time.Now()
	switch mechanic {
	case "baseline":
		if _, err := tx.ExecContext(ctx, `
INSERT INTO cloud_resource_owner_probe (uid, source_order_key, value, updated_at)
SELECT u, k, v, now() FROM unnest($1::text[], $2::text[], $3::text[]) AS t(u,k,v)
ON CONFLICT (uid) DO UPDATE SET source_order_key = EXCLUDED.source_order_key, value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
  WHERE EXCLUDED.source_order_key > cloud_resource_owner_probe.source_order_key`,
			sorted, keys, vals); err != nil {
			return st, err
		}
	case "ret_where":
		rows, err := tx.QueryContext(ctx, ownerRetBatchUpsertWhere, sorted, keys, vals)
		if err != nil {
			return st, err
		}
		if err := scanRows(rows); err != nil {
			return st, err
		}
	case "ret_case":
		rows, err := tx.QueryContext(ctx, ownerRetBatchUpsertCase, sorted, keys, vals)
		if err != nil {
			return st, err
		}
		if err := scanRows(rows); err != nil {
			return st, err
		}
	default:
		return st, fmt.Errorf("unknown mechanic %q", mechanic)
	}
	st.upsert = time.Since(s)

	s = time.Now()
	var readback []string
	switch mechanic {
	case "baseline":
		readback = sorted // unconditional winner read-back, the proven shape
	default:
		for _, uid := range sorted { // fallback only for uids RETURNING omitted
			if _, ok := winners[uid]; !ok {
				readback = append(readback, uid)
			}
		}
	}
	if len(readback) > 0 {
		rows, err := tx.QueryContext(ctx,
			`SELECT uid, source_order_key, value FROM cloud_resource_owner_probe WHERE uid = ANY($1::text[])`, readback)
		if err != nil {
			return st, err
		}
		if err := scanRows(rows); err != nil {
			return st, err
		}
	}
	st.readback = time.Since(s)

	graphRows := make([]map[string]any, 0, len(sorted))
	for _, uid := range sorted {
		w, ok := winners[uid]
		if !ok {
			return st, fmt.Errorf("mechanic %s: no winner resolved for uid %s", mechanic, uid)
		}
		graphRows = append(graphRows, map[string]any{"uid": uid, "order_key": w[0], "value": w[1]})
	}
	s = time.Now()
	if err := runLiveGuardWriteLedger(ctx, driver, database,
		`UNWIND $rows AS row MERGE (n:OwnerLedgerProbe {uid: row.uid}) SET n.source_order_key = row.order_key, n.value = row.value`,
		map[string]any{"rows": graphRows}); err != nil {
		return st, err
	}
	st.graph = time.Since(s)

	s = time.Now()
	if err := tx.Commit(); err != nil {
		return st, err
	}
	st.commit = time.Since(s)
	committed = true
	return st, nil
}

// ownerRetFlatGraph is the flat graph-only floor with a parameterized key.
func ownerRetFlatGraph(ctx context.Context, driver neo4jdriver.DriverWithContext, database string, uids []string, key, value string) error {
	rows := make([]map[string]any, len(uids))
	for i, uid := range uids {
		rows[i] = map[string]any{"uid": uid, "order_key": key, "value": value}
	}
	return runLiveGuardWriteLedger(ctx, driver, database,
		`UNWIND $rows AS row MERGE (n:OwnerLedgerProbe {uid: row.uid}) SET n.source_order_key = row.order_key, n.value = row.value`,
		map[string]any{"rows": rows})
}

// TestLiveOwnerLedgerReturningBatchPerf measures the RETURNING optimization
// against the proven design (b) baseline and the flat graph-only floor, on the
// two batch shapes that matter:
//
//   - non_contended: every warm iteration carries a strictly newer order key
//     (a fresh scan of the same resources — the overwhelmingly common case),
//     so every row wins and ret_where needs zero fallback reads;
//   - all_losers: the ledger is seeded with a higher key and every iteration
//     writes a lower one (the losing/duplicate-replay path), so ret_where
//     falls back to a full read-back and ret_case self-overwrites every row.
func TestLiveOwnerLedgerReturningBatchPerf(t *testing.T) {
	if strings.TrimSpace(os.Getenv(ownerLedgerProveEnv)) == "" {
		t.Skipf("set %s=1 to run the #5007 RETURNING batch perf differential", ownerLedgerProveEnv)
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
	_ = runLiveGuardWriteLedger(ctx, driver, cfg.DatabaseName, ownerLedgerGraphConstraint, nil)

	const (
		batchRows = 500
		iters     = 20
	)
	mkUIDs := func(ns string) []string {
		uids := make([]string, batchRows)
		for i := range uids {
			uids[i] = fmt.Sprintf("perfret-%s-%d", ns, i)
		}
		return uids
	}
	mechanics := []string{"baseline", "ret_where", "ret_case"}

	// Keys derive from a per-invocation nanosecond nonce (fixed-width, so
	// string order = numeric order, the ADR's order-key shape). Without it a
	// re-run's keys lose against the previous run's stored winners and the
	// non-contended case silently degenerates into the no-op regime.
	runNonce := time.Now().UTC().UnixNano()

	for _, tc := range []struct {
		name    string
		keyFor  func(iter int) string
		seedKey string // non-empty: seed the ledger so every write loses
	}{
		{name: "non_contended", keyFor: func(iter int) string { return fmt.Sprintf("%019d-%06d", runNonce, iter) }},
		// The all-losers seed starts with 'A' > any digit, so it beats every
		// nonce-derived key on every run.
		{name: "all_losers", keyFor: func(int) string { return "000100-k" }, seedKey: "A999999-seed"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Flat floor on its own namespace.
			flatUIDs := mkUIDs("flat-" + tc.name)
			if err := ownerRetFlatGraph(ctx, driver, cfg.DatabaseName, flatUIDs, "000001-k", "v"); err != nil {
				t.Fatalf("prime flat: %v", err)
			}
			var flatTotal time.Duration
			for i := 0; i < iters; i++ {
				s := time.Now()
				if err := ownerRetFlatGraph(ctx, driver, cfg.DatabaseName, flatUIDs, tc.keyFor(i), "v"); err != nil {
					t.Fatalf("flat batch: %v", err)
				}
				flatTotal += time.Since(s)
			}
			flatAvg := flatTotal / iters
			t.Logf("[%s] flat graph-only floor: avg=%s", tc.name, flatAvg)

			for _, mech := range mechanics {
				uids := mkUIDs(mech + "-" + tc.name)
				if tc.seedKey != "" {
					if _, err := ownerRetBatch(ctx, db, driver, cfg.DatabaseName, "baseline", uids, tc.seedKey, "seed"); err != nil {
						t.Fatalf("seed %s: %v", mech, err)
					}
				} else if _, err := ownerRetBatch(ctx, db, driver, cfg.DatabaseName, mech, uids, "000001-k", "v"); err != nil {
					t.Fatalf("prime %s: %v", mech, err)
				}
				var total time.Duration
				var stages ownerRetStages
				for i := 0; i < iters; i++ {
					s := time.Now()
					st, err := ownerRetBatch(ctx, db, driver, cfg.DatabaseName, mech, uids, tc.keyFor(i), "v")
					if err != nil {
						t.Fatalf("%s batch: %v", mech, err)
					}
					total += time.Since(s)
					stages = stages.add(st)
				}
				avg := total / iters
				sa := stages.div(iters)
				t.Logf("[%s] %s: avg=%s ratio_vs_flat=%.2fx per_row_overhead=%s stages{lock=%s upsert=%s readback=%s graph=%s commit=%s}",
					tc.name, mech, avg, float64(avg)/float64(flatAvg), (avg-flatAvg)/time.Duration(batchRows),
					sa.lock, sa.upsert, sa.readback, sa.graph, sa.commit)
			}
		})
	}

	// Concurrent overlapping batches on the adopted mechanic: two goroutines
	// write the SAME 500 uids with different keys. The sorted advisory locks
	// serialize the overlap by design; this records what that costs in wall
	// time relative to one batch.
	t.Run("concurrent_overlap_ret_case", func(t *testing.T) {
		uids := mkUIDs("overlap")
		if _, err := ownerRetBatch(ctx, db, driver, cfg.DatabaseName, "ret_case", uids, "000001-k", "v"); err != nil {
			t.Fatalf("prime: %v", err)
		}
		const overlapIters = 5
		var wall time.Duration
		runNonce := time.Now().UTC().UnixNano()
		for i := 0; i < overlapIters; i++ {
			lo, hi := fmt.Sprintf("%019d-a", runNonce+int64(2*i)), fmt.Sprintf("%019d-b", runNonce+int64(2*i+1))
			s := time.Now()
			var wg sync.WaitGroup
			errs := make([]error, 2)
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, errs[0] = ownerRetBatch(ctx, db, driver, cfg.DatabaseName, "ret_case", uids, lo, "vl")
			}()
			go func() {
				defer wg.Done()
				_, errs[1] = ownerRetBatch(ctx, db, driver, cfg.DatabaseName, "ret_case", uids, hi, "vh")
			}()
			wg.Wait()
			wall += time.Since(s)
			for j, e := range errs {
				if e != nil {
					t.Fatalf("overlap iter %d writer %d: %v", i, j, e)
				}
			}
			// Convergence check: the max key must be the final state everywhere.
			k, v, err := selectOwner(ctx, db, uids[0])
			if err != nil || k != hi || v != "vh" {
				t.Errorf("overlap iter %d: ledger got (%q,%q,%v), want (%q,vh)", i, k, v, err, hi)
			}
			gk, gv := readOwnerGraph(ctx, t, driver, cfg.DatabaseName, uids[0])
			if gk != hi || gv != "vh" {
				t.Errorf("overlap iter %d: graph got (%q,%q), want (%q,vh)", i, gk, gv, hi)
			}
		}
		t.Logf("[concurrent_overlap] 2 fully-overlapping ret_case batches: avg wall=%s (serialized by the per-uid locks, by design)", wall/overlapIters)
	})
}
