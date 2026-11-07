// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Cleanup entry env: the stranded seed plus the exact pending count the
// killed leg's record shows. Both are required and explicit: without them
// the entry skips, never deletes.
const (
	envCleanupSeed    = "ESHU_CONTENTION_CLEANUP_SEED"
	envCleanupPending = "ESHU_CONTENTION_CLEANUP_EXPECT_PENDING"
)

// resurveyDelay waits out one satellite reconcile tick (60s) plus margin,
// proving the deleted rows do not come back.
const resurveyDelay = 70 * time.Second

// checkStrandedPreconditions fails closed unless the database holds exactly
// the stranded leg's record: wantPending pending items of this seed and
// nothing else synthetic — no claimed items, no claim rows, no live
// claims, no terminal duplicates, and exactly one instance-family row.
func checkStrandedPreconditions(c Census, snap Snapshot, nonterm, doubleTerm []string, seed, wantPending int64) error {
	if got := c.Prefixed["workflow_work_items|pending"]; got != int(wantPending) {
		return fmt.Errorf("pending items = %d, want exactly %d", got, wantPending)
	}
	for key, n := range c.Prefixed {
		table, _, _ := strings.Cut(key, "|")
		switch table {
		case "workflow_work_items":
			if key != "workflow_work_items|pending" && n != 0 {
				return fmt.Errorf("census %s = %d, want 0", key, n)
			}
		case "workflow_claims":
			if n != 0 {
				return fmt.Errorf("census %s = %d, want 0 claim rows", key, n)
			}
		}
	}
	if snap.ActiveClaims != 0 {
		return fmt.Errorf("active claims = %d, want 0", snap.ActiveClaims)
	}
	if len(doubleTerm) != 0 {
		return fmt.Errorf("double terminal items = %d, want 0", len(doubleTerm))
	}
	prefix := strings.TrimSuffix(seedItemPattern(seed), "%")
	mine, foreign := 0, []string{}
	for _, entry := range nonterm {
		id, status, _ := strings.Cut(entry, ":")
		if !strings.HasPrefix(id, prefix) {
			foreign = append(foreign, entry)
			continue
		}
		mine++
		if status != string(workflow.WorkItemStatusPending) {
			return fmt.Errorf("seed item %q status %q, want pending", id, status)
		}
	}
	_ = foreign
	if int64(mine) != wantPending {
		return fmt.Errorf("seed nonterminal items = %d, want %d", mine, wantPending)
	}
	instances := 0
	for key, n := range c.Prefixed {
		if strings.HasPrefix(key, "collector_instances|") {
			instances += n
		}
	}
	if instances != 1 {
		return fmt.Errorf("instance-family rows = %d, want exactly 1", instances)
	}
	return nil
}

// TestCleanupStrandedSeed reverses one killed leg through the harness's own
// delete paths: exact-prefix DeletePrefixed plus the exact-ID seed instance
// delete. It deletes nothing unless the pre-census matches the killed leg's
// record exactly.
func TestCleanupStrandedSeed(t *testing.T) {
	seedRaw, pendingRaw := os.Getenv(envCleanupSeed), os.Getenv(envCleanupPending)
	if seedRaw == "" || pendingRaw == "" {
		t.Skip(envCleanupSeed + " and " + envCleanupPending + " are required; skipping the stranded-leg cleanup")
	}
	seed, err := strconv.ParseInt(seedRaw, 10, 64)
	if err != nil || seed <= 0 {
		t.Fatalf("%s=%q must be a positive seed", envCleanupSeed, seedRaw)
	}
	wantPending, err := strconv.ParseInt(pendingRaw, 10, 64)
	if err != nil || wantPending <= 0 {
		t.Fatalf("%s=%q must be a positive count", envCleanupPending, pendingRaw)
	}
	dsn := os.Getenv(envSatelliteDSN)
	if dsn == "" {
		t.Fatalf("%s is required; refusing to clean without the satellite DSN", envSatelliteDSN)
	}
	requireLoopbackDSN(t, envSatelliteDSN, dsn)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open satellite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(4)
	ctx := context.Background()
	name, err := CurrentDatabase(ctx, db)
	if err != nil {
		t.Fatalf("current database: %v", err)
	}
	if want := os.Getenv(envSatelliteDB); want == "" || name != want || forbiddenSatelliteDatabases[name] {
		t.Fatalf("satellite DSN reaches database %q; refusing to clean", name)
	}
	obs, err := NewSQLObserver(ctx, db)
	if err != nil {
		t.Fatalf("observer: %v", err)
	}
	census, err := obs.Census(ctx)
	if err != nil {
		t.Fatalf("pre-census: %v", err)
	}
	snap, err := obs.Snapshot(ctx)
	if err != nil {
		t.Fatalf("pre-snapshot: %v", err)
	}
	nonterm, err := obs.NonTerminalItems(ctx, int(wantPending)+1)
	if err != nil {
		t.Fatalf("nonterminal items: %v", err)
	}
	if int64(len(nonterm)) > wantPending {
		t.Fatalf("nonterminal list truncated at %d; refusing to clean blind", len(nonterm))
	}
	doubleTerm, err := obs.DoubleTerminal(ctx)
	if err != nil {
		t.Fatalf("double terminal: %v", err)
	}
	if err := checkStrandedPreconditions(census, snap, nonterm, doubleTerm, seed, wantPending); err != nil {
		t.Fatalf("preconditions: %v (deleted nothing)", err)
	}
	deleted, err := obs.DeletePrefixed(ctx, seed, 1000)
	if err != nil {
		t.Fatalf("delete prefixed: %v", err)
	}
	t.Logf("prefix delete: %v", deleted)
	n, err := obs.DeleteSeedInstance(ctx)
	if err != nil {
		t.Fatalf("delete seed instance: %v", err)
	}
	if n != 1 {
		t.Fatalf("seed instance deleted = %d, want 1", n)
	}
	final, err := obs.Census(ctx)
	if err != nil {
		t.Fatalf("post-census: %v", err)
	}
	if got := final.prefixedTotal(); got != 0 {
		t.Fatalf("residue after cleanup: %d synthetic rows", got)
	}
	t.Logf("post-census clean; waiting %s for one reconcile tick, then re-census", resurveyDelay)
	time.Sleep(resurveyDelay)
	again, err := obs.Census(ctx)
	if err != nil {
		t.Fatalf("resurvey census: %v", err)
	}
	if got := again.prefixedTotal(); got != 0 {
		t.Fatalf("resurrected rows after one tick: %d synthetic rows", got)
	}
	t.Logf("cleanup complete: seed %d reversed, no resurrection", seed)
}
