// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// TestLiveGraphGuardProveTheory re-runs the #5007/#5062 graph-side guard
// disproof against whatever NornicDB (or Neo4j) the ESHU_NEO4J_URI env points
// at, so the "~26% of concurrent property writes to a shared EXISTING node are
// silently lost" finding can be re-confirmed on a NEW backend build (issue
// #5062: is the defect still present on v1.1.11?).
//
// It is the pure graph-side mechanic — NO Postgres coordination — so it
// isolates the backend's own conflict detection. Two writers race the same
// pre-existing node uid with different order keys, each running the conditional
// max-key update with a retry-on-Outdated loop (the production RetryingExecutor
// mechanic). The node is SEEDED first so the commit-time UNIQUE path (which
// only catches concurrent CREATE) never engages — the warm/contended case.
//
// Skipped unless ESHU_GRAPH_GUARD_PROVE_LIVE=1, with ESHU_NEO4J_URI /
// ESHU_GRAPH_BACKEND pointing at a live backend.
func TestLiveGraphGuardProveTheory(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_GRAPH_GUARD_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_GRAPH_GUARD_PROVE_LIVE=1 to run the #5062 graph-guard prove-theory shim")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		cc, ccl := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccl()
		_ = driver.Close(cc)
	}()
	db := cfg.DatabaseName

	// Two writers; keys sort lexicographically so the max winner is "2000-b".
	writers := []struct{ key, value string }{
		{"1000-a", "va"},
		{"2000-b", "vb"},
	}
	maxKey, maxValue := "2000-b", "vb"

	const trials = 100
	var lost, writerErrs int
	for trial := 0; trial < trials; trial++ {
		uid := fmt.Sprintf("graphguard-%d", trial)
		// Reset + SEED the node so it already EXISTS before the race (warm case).
		if err := runLiveGuardWriteLedger(ctx, driver, db,
			`MATCH (n:GraphGuardProbe {uid: $uid}) DETACH DELETE n`, map[string]any{"uid": uid}); err != nil {
			t.Fatalf("trial %d reset: %v", trial, err)
		}
		if err := runLiveGuardWriteLedger(ctx, driver, db,
			`MERGE (n:GraphGuardProbe {uid: $uid}) SET n.source_order_key = "0000", n.value = "seed"`,
			map[string]any{"uid": uid}); err != nil {
			t.Fatalf("trial %d seed: %v", trial, err)
		}

		var wg sync.WaitGroup
		errs := make([]error, len(writers))
		wg.Add(len(writers))
		for i, w := range writers {
			i, w := i, w
			go func() {
				defer wg.Done()
				errs[i] = graphGuardWrite(ctx, driver, db, uid, w.key, w.value)
			}()
		}
		wg.Wait()
		for _, e := range errs {
			if e != nil {
				writerErrs++
			}
		}

		k, v := readGraphGuard(ctx, t, driver, db, uid)
		if k != maxKey || v != maxValue {
			lost++
			if lost <= 5 {
				t.Logf("trial %d LOST: final (%q,%q) want (%q,%q)", trial, k, v, maxKey, maxValue)
			}
		}
	}
	t.Logf("RESULT: %d/%d trials silently LOST the max winner (writer errors=%d) on backend %q",
		lost, trials, writerErrs, os.Getenv("ESHU_NEO4J_URI"))
	// No hard assertion: this shim MEASURES the backend. A conformant backend
	// reports 0 lost; NornicDB v1.1.9 reported ~26. The number is the proof.
}

// graphGuardWrite is the ADR's disproven mechanic (2): the conditional max-key
// update on a shared existing node, run as a read-modify-write inside ONE
// explicit transaction (MATCH-read the current key, hold the tx open across a
// small gap, then conditional SET), wrapped in a retry-on-Outdated loop exactly
// like the production RetryingExecutor. Holding the tx across the gap is what
// forces the two writers' read-modify-write windows to overlap, so the backend
// MVCC "changed after transaction start" check is actually exercised. No CASE
// (NornicDB stringifies a CASE mixing row.field with a node-property ELSE); a
// plain SET filtered by WHERE.
func graphGuardWrite(ctx context.Context, driver neo4jdriver.DriverWithContext, db, uid, orderKey, value string) error {
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		lastErr = graphGuardWriteOnce(ctx, driver, db, uid, orderKey, value)
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

func graphGuardWriteOnce(ctx context.Context, driver neo4jdriver.DriverWithContext, db, uid, orderKey, value string) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: db})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		// Read-modify-write held open across a gap: read the current key, then
		// (after the window) conditionally overwrite. Two concurrent writers
		// both read the seed, both pass the WHERE, and race the commit — the
		// case a conformant backend aborts for the loser and NornicDB v1.1.9
		// silently kept ~26% of the time.
		r, rerr := tx.Run(ctx, `MATCH (n:GraphGuardProbe {uid: $uid}) RETURN n.source_order_key AS k`, map[string]any{"uid": uid})
		if rerr != nil {
			return nil, rerr
		}
		if _, cerr := r.Single(ctx); cerr != nil {
			return nil, cerr
		}
		time.Sleep(5 * time.Millisecond)
		w, werr := tx.Run(ctx, `
MATCH (n:GraphGuardProbe {uid: $uid})
WHERE n.source_order_key IS NULL OR $order_key > n.source_order_key
SET n.source_order_key = $order_key, n.value = $value`, map[string]any{"uid": uid, "order_key": orderKey, "value": value})
		if werr != nil {
			return nil, werr
		}
		_, cerr := w.Consume(ctx)
		return nil, cerr
	})
	return err
}

func readGraphGuard(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, db, uid string) (string, string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: db})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx,
		`MATCH (n:GraphGuardProbe {uid: $uid}) RETURN n.source_order_key AS k, n.value AS v`,
		map[string]any{"uid": uid})
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
