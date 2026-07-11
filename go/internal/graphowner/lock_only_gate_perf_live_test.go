// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestLiveLockOnlyGateBatchPerfAndEquivalence measures LockOnlyGate's per-batch
// Postgres round-trip overhead on a realistic per-generation posture batch
// (500 uids, matching cypher.DefaultBatchSize / lockChunkSize), and proves the
// gate is invisible to graph output on a NON-contended batch: the same rows
// written flat (no gate) and through LockOnlyGate produce byte-identical
// posture_value properties. It is a measurement plus an equivalence check, not
// a pass/fail perf gate — matching TestLiveOwnerLedgerBatchPerf's convention.
//
// Skipped by default; set ESHU_OWNER_LEDGER_PROVE_LIVE=1, ESHU_OWNER_LEDGER_PG_DSN,
// and the ESHU_GRAPH_BACKEND/ESHU_NEO4J_URI env.
func TestLiveLockOnlyGateBatchPerfAndEquivalence(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OWNER_LEDGER_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OWNER_LEDGER_PROVE_LIVE=1 (+ PG DSN + NornicDB env) to run the #5062 lock-only-gate batch perf/equivalence proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_OWNER_LEDGER_PG_DSN"))
	if dsn == "" {
		t.Fatal("ESHU_OWNER_LEDGER_PG_DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	rawDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = rawDB.Close() }()
	sqldb := postgres.SQLDB{DB: rawDB}
	if err := postgres.NewGraphNodeOwnerStore().EnsureSchema(ctx, sqldb); err != nil {
		t.Fatalf("ensure schema: %v", err)
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
	if err := lockRaceProbeExec(ctx, driver, cfg.DatabaseName,
		`CREATE CONSTRAINT lock_only_perf_probe_uid_unique IF NOT EXISTS FOR (n:LockOnlyPerfProbe) REQUIRE n.uid IS UNIQUE`, nil); err != nil {
		t.Fatalf("create constraint: %v", err)
	}

	const batchRows = 500
	lockGate := NewLockOnlyGate(sqldb)

	batchWrite := func(prefix string) postureNodeWriteFunc {
		return func(ctx context.Context, rows []map[string]any, _, _, _ string) error {
			graphRows := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				graphRows = append(graphRows, map[string]any{
					"uid":           prefix + row["uid"].(string),
					"posture_value": row["posture_value"],
				})
			}
			return lockRaceProbeExec(ctx, driver, cfg.DatabaseName,
				`UNWIND $rows AS row MERGE (n:LockOnlyPerfProbe {uid: row.uid}) SET n.posture_value = row.posture_value`,
				map[string]any{"rows": graphRows})
		}
	}

	rows := make([]map[string]any, batchRows)
	for i := range rows {
		rows[i] = map[string]any{"uid": fmt.Sprintf("perf-%d", i), "posture_value": fmt.Sprintf("state-%d", i)}
	}

	t.Run("non_contended_equivalence", func(t *testing.T) {
		flatPrefix := fmt.Sprintf("equiv-flat-%d-", time.Now().UnixNano())
		gatedPrefix := fmt.Sprintf("equiv-gated-%d-", time.Now().UnixNano())

		if err := batchWrite(flatPrefix)(ctx, rows, "", "", ""); err != nil {
			t.Fatalf("flat batch write: %v", err)
		}
		if err := lockGate.write(ctx, "test_equivalence", rows, "scope-1", "gen-1", "test/equivalence", batchWrite(gatedPrefix)); err != nil {
			t.Fatalf("gated batch write: %v", err)
		}

		mismatches := 0
		for i := range rows {
			flatVal := readLockOnlyPerfProbe(t, driver, cfg.DatabaseName, flatPrefix+fmt.Sprintf("perf-%d", i))
			gatedVal := readLockOnlyPerfProbe(t, driver, cfg.DatabaseName, gatedPrefix+fmt.Sprintf("perf-%d", i))
			wantVal := fmt.Sprintf("state-%d", i)
			if flatVal != wantVal || gatedVal != wantVal || flatVal != gatedVal {
				mismatches++
				if mismatches <= 3 {
					t.Logf("row %d: flat=%q gated=%q want=%q", i, flatVal, gatedVal, wantVal)
				}
			}
		}
		if mismatches > 0 {
			t.Fatalf("LockOnlyGate is NOT invisible on a non-contended batch: %d/%d rows diverged from the flat (ungated) write", mismatches, batchRows)
		}
		t.Logf("non-contended equivalence: %d/%d rows identical between flat and LockOnlyGate-wrapped writes", batchRows, batchRows)
	})

	t.Run("batch_perf", func(t *testing.T) {
		const iters = 20
		flatPrefix := fmt.Sprintf("perfw-flat-%d-", time.Now().UnixNano())
		gatedPrefix := fmt.Sprintf("perfw-gated-%d-", time.Now().UnixNano())
		flatFn := batchWrite(flatPrefix)
		gatedFn := batchWrite(gatedPrefix)

		// prime (cold create) both label sets once
		_ = flatFn(ctx, rows, "", "", "")
		_ = lockGate.write(ctx, "test_perf_prime", rows, "scope-1", "gen-1", "test/perf", gatedFn)

		var flatTotal, gatedTotal time.Duration
		for i := 0; i < iters; i++ {
			s := time.Now()
			if err := flatFn(ctx, rows, "", "", ""); err != nil {
				t.Fatalf("flat batch: %v", err)
			}
			flatTotal += time.Since(s)

			s = time.Now()
			if err := lockGate.write(ctx, "test_perf", rows, "scope-1", "gen-1", "test/perf", gatedFn); err != nil {
				t.Fatalf("gated batch: %v", err)
			}
			gatedTotal += time.Since(s)
		}
		flatAvg := flatTotal / iters
		gatedAvg := gatedTotal / iters
		t.Logf("#5062 LockOnlyGate batch perf (%d uids x %d warm iters): flat graph-only avg=%s, lock-only-gated avg=%s, ratio=%.2fx, per-row overhead=%s (template: #5066's design-(b) owner-ledger gate measured 2.28x / 15us/row for the strictly heavier ledger-upsert+lock+graph path)",
			batchRows, iters, flatAvg, gatedAvg, float64(gatedAvg)/float64(flatAvg), (gatedAvg-flatAvg)/time.Duration(batchRows))
	})
}

func readLockOnlyPerfProbe(t *testing.T, driver neo4jdriver.DriverWithContext, database, uid string) string {
	t.Helper()
	ctx := context.Background()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `MATCH (n:LockOnlyPerfProbe {uid: $uid}) RETURN n.posture_value AS v`, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("read perf probe: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("read perf probe single: %v", err)
	}
	v, _ := record.Get("v")
	vs, _ := v.(string)
	return vs
}
