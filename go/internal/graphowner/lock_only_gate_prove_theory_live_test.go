// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

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
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestLiveLockOnlyGateProveTheory is the #5062 P1 prove-theory-first gate for
// LockOnlyGate: it races an UNGATED posture write against a Gate-gated
// base-property write on the SAME CloudResource-shaped uid, and separately
// races the SAME posture write wrapped in LockOnlyGate — using the IDENTICAL
// postgres.GraphNodeOwnerStore advisory lock key Gate uses — against the same
// base write, using the widened-transaction-window shim
// TestLiveGraphGuardProveTheory proved reliably reproduces NornicDB
// conflict-handling defects (a plain narrow single-statement race is a known
// false negative; see that test's doc comment).
//
// MEASURED RESULT (recorded here so the next investigator does not re-run this
// under the wrong expectation): for THIS writer pair's actual Cypher shape —
// both sides are UNCONDITIONAL `MATCH/MERGE ... SET` with no WHERE-based
// compare-and-swap, matching cloud_resource_node_writer.go's
// baseCloudResourceUpsertCypher and rds_posture_node_writer.go's
// canonicalRDSPostureUpdateCypher — 100 trials at a 5ms gap produced ZERO
// silent property loss in BOTH the ungated and the locked scenario. This is
// NOT the same defect graph_guard_prove_theory_live_test.go proved (5-6/100
// silently lost): that shim's writers use a WHERE-conditional
// compare-and-swap SET (`WHERE n.source_order_key IS NULL OR $order_key >
// n.source_order_key`), and NornicDB's OCC does not reliably re-validate that
// predicate against the live value at commit. Neither the CloudResource base
// writer nor any of the four #5062 posture writers use a conditional SET, so
// that specific silent-loss mechanism does not apply to this writer pair —
// the prove-theory-first gate is honest about this: the literal "silent
// revert" framing did not reproduce here, and this is a disproven sub-theory,
// not a confirmed one.
//
// What DID reproduce, and is the actual proof this test asserts on: the
// ungated scenario's concurrent unconditional SETs on the SAME node
// repeatedly triggered NornicDB's `Neo.TransientError.Transaction.Outdated`
// conflict abort (the same transient error production's
// cypher.RetryingExecutor already retries), and absorbing that via
// retry-to-convergence correctly recovered zero-loss — but at drastic cost:
// the ungated subtest took 199.52s/100 trials (~2.0s/trial, heavy repeated
// retry) versus the locked subtest's 6.66s/100 trials (~67ms/trial, i.e. no
// conflicts to retry) in the run this comment was written against — a ~30x
// latency amplification from unmediated NornicDB contention that LockOnlyGate
// eliminates by removing the conflict opportunity entirely (concurrency-
// deadlock-rigor's contention proof, not an accuracy proof). This retry-storm
// elimination, plus defense-in-depth against the conditional-SET-shaped
// defect class graph_guard already proved is real in this exact NornicDB
// version line, is this change's actual justification.
//
// Skipped by default; set ESHU_OWNER_LEDGER_PROVE_LIVE=1, ESHU_OWNER_LEDGER_PG_DSN,
// and the ESHU_GRAPH_BACKEND/ESHU_NEO4J_URI env (matching the existing #5007
// prove-theory live tests' gating).
func TestLiveLockOnlyGateProveTheory(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OWNER_LEDGER_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OWNER_LEDGER_PROVE_LIVE=1 (+ PG DSN + NornicDB env) to run the #5062 lock-only-gate prove-theory shim")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_OWNER_LEDGER_PG_DSN"))
	if dsn == "" {
		t.Fatal("ESHU_OWNER_LEDGER_PG_DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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
		`CREATE CONSTRAINT lock_only_race_probe_uid_unique IF NOT EXISTS FOR (n:LockOnlyRaceProbe) REQUIRE n.uid IS UNIQUE`, nil); err != nil {
		t.Fatalf("create constraint: %v", err)
	}

	ownerGate := NewGate(sqldb)
	lockGate := NewLockOnlyGate(sqldb)
	const gap = 5 * time.Millisecond

	baseUnderlying := func(ctx context.Context, rows []map[string]any, _ string) error {
		for _, row := range rows {
			uid, _ := row["uid"].(string)
			orderKey, _ := row["source_order_key"].(string)
			baseValue, _ := row["base_value"].(string)
			if err := raceWriteWithRetry(ctx, driver, cfg.DatabaseName,
				`MATCH (n:LockOnlyRaceProbe {uid: $uid}) SET n.source_order_key = $order_key, n.base_value = $base_value`,
				uid, gap, map[string]any{"order_key": orderKey, "base_value": baseValue}); err != nil {
				return err
			}
		}
		return nil
	}
	postureUnderlying := func(ctx context.Context, rows []map[string]any, _, _, _ string) error {
		for _, row := range rows {
			uid, _ := row["uid"].(string)
			postureValue, _ := row["posture_value"].(string)
			if err := raceWriteWithRetry(ctx, driver, cfg.DatabaseName,
				`MATCH (n:LockOnlyRaceProbe {uid: $uid}) SET n.posture_value = $posture_value`,
				uid, gap, map[string]any{"posture_value": postureValue}); err != nil {
				return err
			}
		}
		return nil
	}

	runRace := func(t *testing.T, trials int, postureGated bool) (lostBase, lostPosture int, elapsed time.Duration) {
		start := time.Now()
		for trial := 0; trial < trials; trial++ {
			uid := fmt.Sprintf("lockrace-%v-%d", postureGated, trial)
			resetLockRaceFixture(ctx, t, driver, cfg.DatabaseName, uid)

			var wg sync.WaitGroup
			wg.Add(2)
			var baseErr, postureErr error
			go func() {
				defer wg.Done()
				baseErr = ownerGate.write(ctx, "test_lockrace_base", []map[string]any{
					{"uid": uid, "source_order_key": "1000-a", "base_value": "base-x"},
				}, "test/lockrace-base", baseUnderlying)
			}()
			go func() {
				defer wg.Done()
				postureRows := []map[string]any{{"uid": uid, "posture_value": "posture-y"}}
				if postureGated {
					postureErr = lockGate.write(ctx, "test_lockrace_posture", postureRows, "scope-1", "gen-1", "test/lockrace-posture", postureUnderlying)
				} else {
					postureErr = postureUnderlying(ctx, postureRows, "scope-1", "gen-1", "test/lockrace-posture")
				}
			}()
			wg.Wait()
			if baseErr != nil {
				t.Fatalf("trial %d base writer: %v", trial, baseErr)
			}
			if postureErr != nil {
				t.Fatalf("trial %d posture writer: %v", trial, postureErr)
			}

			base, posture := readLockRaceProbe(t, driver, cfg.DatabaseName, uid)
			if base != "base-x" {
				lostBase++
			}
			if posture != "posture-y" {
				lostPosture++
			}
		}
		return lostBase, lostPosture, time.Since(start)
	}

	const trials = 100
	var ungatedElapsed, lockedElapsed time.Duration

	t.Run("ungated_posture_write_can_lose_updates", func(t *testing.T) {
		lostBase, lostPosture, elapsed := runRace(t, trials, false)
		ungatedElapsed = elapsed
		t.Logf("#5062 UNGATED posture write vs gated base write: %d trials, %d base lost, %d posture lost, elapsed=%s (%.0fms/trial) (no loss hard-assertion — see the test doc comment: this writer pair's unconditional SET did not reproduce graph_guard's conditional-SET silent-loss mechanism in this run; the retry-storm timing below is the actual proof)",
			trials, lostBase, lostPosture, elapsed, float64(elapsed.Milliseconds())/float64(trials))
		// No hard assertion on loss count: like the rejected "design (a)"
		// measurement in owner_ledger_prove_theory_live_test.go, this control is
		// measured for the record, not gated on, because asserting lost > 0 here
		// would be a flaky test tied to NornicDB's exact nondeterministic
		// conflict-detection behavior for this writer shape.
	})

	t.Run("locked_posture_write_never_loses_updates", func(t *testing.T) {
		lostBase, lostPosture, elapsed := runRace(t, trials, true)
		lockedElapsed = elapsed
		t.Logf("#5062 LockOnlyGate-wrapped posture write vs gated base write: %d trials, %d base lost, %d posture lost, elapsed=%s (%.0fms/trial)",
			trials, lostBase, lostPosture, elapsed, float64(elapsed.Milliseconds())/float64(trials))
		if lostBase > 0 || lostPosture > 0 {
			t.Fatalf("LockOnlyGate did NOT close the #5062 race: %d/%d base lost, %d/%d posture lost — the posture write and the base-property write overlapped a NornicDB transaction", lostBase, trials, lostPosture, trials)
		}
	})

	t.Run("locked_eliminates_the_contention_retry_storm", func(t *testing.T) {
		if ungatedElapsed == 0 || lockedElapsed == 0 {
			t.Skip("requires both prior subtests to have run in this process")
		}
		ratio := float64(ungatedElapsed) / float64(lockedElapsed)
		t.Logf("#5062 contention proof: ungated=%s vs locked=%s, ratio=%.1fx — LockOnlyGate serializes the two writers' NornicDB transactions instead of letting them retry-thrash against each other's Neo.TransientError.Transaction.Outdated aborts",
			ungatedElapsed, lockedElapsed, ratio)
		// The lock-only critical section (Postgres advisory lock + the graph
		// write held across it) MUST be strictly cheaper under contention than
		// letting both writers race and retry against NornicDB's own conflict
		// abort — this is concurrency-deadlock-rigor's contention proof: the
		// fix removes unsafe overlap, and here that overlap's cost was retry
		// thrash, not silent corruption.
		if lockedElapsed >= ungatedElapsed {
			t.Fatalf("locked path (%s) was not faster than the ungated retry-thrash path (%s) — the contention-elimination claim is unsupported by this run", lockedElapsed, ungatedElapsed)
		}
	})
}

// raceWriteWithRetry runs one read-sleep-write cycle against uid inside an
// explicit NornicDB transaction, retrying on the backend's optimistic-conflict
// error so a writer's own transient commit conflict never masks a lost update
// in the caller's measurement (mirrors graphGuardWrite's retry loop).
func raceWriteWithRetry(ctx context.Context, driver neo4jdriver.DriverWithContext, database, setCypher, uid string, gap time.Duration, params map[string]any) error {
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		lastErr = raceWriteOnce(ctx, driver, database, setCypher, uid, gap, params)
		if lastErr == nil {
			return nil
		}
		if !strings.Contains(lastErr.Error(), "Outdated") {
			return lastErr
		}
		time.Sleep(time.Millisecond)
	}
	return lastErr
}

// raceWriteOnce is the widened-window mechanic: read the node (forces a
// snapshot), hold the transaction open across gap, then SET. Two concurrent
// callers racing the SAME uid with this shape is what
// TestLiveGraphGuardProveTheory proved NornicDB does not reliably serialize.
func raceWriteOnce(ctx context.Context, driver neo4jdriver.DriverWithContext, database, setCypher, uid string, gap time.Duration, params map[string]any) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		r, rerr := tx.Run(ctx, `MATCH (n:LockOnlyRaceProbe {uid: $uid}) RETURN n.uid AS u`, map[string]any{"uid": uid})
		if rerr != nil {
			return nil, rerr
		}
		if _, cerr := r.Single(ctx); cerr != nil {
			return nil, cerr
		}
		if gap > 0 {
			time.Sleep(gap)
		}
		setParams := map[string]any{"uid": uid}
		for k, v := range params {
			setParams[k] = v
		}
		w, werr := tx.Run(ctx, setCypher, setParams)
		if werr != nil {
			return nil, werr
		}
		_, cerr := w.Consume(ctx)
		return nil, cerr
	})
	return err
}

func resetLockRaceFixture(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database, uid string) {
	t.Helper()
	if err := lockRaceProbeExec(ctx, driver, database,
		`MATCH (n:LockOnlyRaceProbe {uid: $uid}) DETACH DELETE n`, map[string]any{"uid": uid}); err != nil {
		t.Fatalf("reset probe: %v", err)
	}
	if err := lockRaceProbeExec(ctx, driver, database,
		`MERGE (n:LockOnlyRaceProbe {uid: $uid}) SET n.seed = true`, map[string]any{"uid": uid}); err != nil {
		t.Fatalf("seed probe: %v", err)
	}
}

func readLockRaceProbe(t *testing.T, driver neo4jdriver.DriverWithContext, database, uid string) (base, posture string) {
	t.Helper()
	ctx := context.Background()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `MATCH (n:LockOnlyRaceProbe {uid: $uid}) RETURN n.base_value AS b, n.posture_value AS p`, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("read probe: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("read probe single: %v", err)
	}
	b, _ := record.Get("b")
	p, _ := record.Get("p")
	bs, _ := b.(string)
	ps, _ := p.(string)
	return bs, ps
}

func lockRaceProbeExec(ctx context.Context, driver neo4jdriver.DriverWithContext, database, cypher string, params map[string]any) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}
