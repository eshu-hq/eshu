// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// strandedCensus builds a Census with the given prefixed item counts, claim
// counts, and instance enabled flags.
func strandedCensus(pending, claimedItems int, claimStatuses map[string]int, instanceEnabled []bool) Census {
	c := Census{Prefixed: map[string]int{}, Other: map[string]int{}}
	if pending > 0 {
		c.Prefixed["workflow_work_items|pending"] = pending
	}
	if claimedItems > 0 {
		c.Prefixed["workflow_work_items|claimed"] = claimedItems
	}
	for status, n := range claimStatuses {
		c.Prefixed["workflow_claims|"+status] = n
	}
	for _, enabled := range instanceEnabled {
		key := "collector_instances|false"
		if enabled {
			key = "collector_instances|true"
		}
		c.Prefixed[key]++
	}
	return c
}

func TestCheckStrandedPreconditions(t *testing.T) {
	seedItems := func(seed int64, n int) []string {
		out := make([]string, 0, n)
		for i := range n {
			out = append(out, ids{seed: seed}.workItem(i)+":pending")
		}
		return out
	}
	clean := func() (Census, *Snapshot, []string, []string) {
		return strandedCensus(100000, 0, nil, []bool{false}),
			&Snapshot{ItemsByStatus: map[string]int{"pending": 100000}, ItemTotal: 100000},
			seedItems(1, 100000), nil
	}
	cases := map[string]func(Census, *Snapshot, []string, []string){
		"clean stranded leg passes": func(c Census, s *Snapshot, nt, dt []string) {},
		"wrong pending count fails": func(c Census, s *Snapshot, nt, dt []string) {
			c.Prefixed["workflow_work_items|pending"] = 99999
		},
		"claimed items fail": func(c Census, s *Snapshot, nt, dt []string) {
			c.Prefixed["workflow_work_items|pending"] = 99990
			c.Prefixed["workflow_work_items|claimed"] = 10
		},
		"claim rows fail": func(c Census, s *Snapshot, nt, dt []string) {
			c.Prefixed["workflow_claims|active"] = 3
		},
		"active claims snapshot fails": func(c Census, s *Snapshot, nt, dt []string) {
			s.ActiveClaims = 2
		},
		"missing instance row fails": func(c Census, s *Snapshot, nt, dt []string) {
			delete(c.Prefixed, "collector_instances|false")
		},
		"extra instance row fails": func(c Census, s *Snapshot, nt, dt []string) {
			c.Prefixed["collector_instances|true"]++
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c, s, nt, dt := clean()
			mutate(c, s, nt, dt)
			err := checkStrandedPreconditions(c, *s, nt, dt, 1, 100000)
			if name == "clean stranded leg passes" && err != nil {
				t.Fatalf("clean: %v", err)
			}
			if name != "clean stranded leg passes" && err == nil {
				t.Fatalf("%s: want mismatch error, got nil", name)
			}
		})
	}
	t.Run("double terminal fails", func(t *testing.T) {
		c, s, nt, _ := clean()
		if err := checkStrandedPreconditions(c, *s, nt, []string{"x"}, 1, 100000); err == nil {
			t.Fatal("want mismatch error, got nil")
		}
	})
	t.Run("truncated nonterminal list fails", func(t *testing.T) {
		c, s, _, dt := clean()
		short := seedItems(1, 10)
		if err := checkStrandedPreconditions(c, *s, short, dt, 1, 100000); err == nil {
			t.Fatal("want mismatch error, got nil")
		}
	})
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
