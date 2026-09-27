// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

func (l *ledgerDB) journalRows(t *testing.T) []string {
	t.Helper()
	return l.queryStrings(t, `
SELECT scope_id || '|' || generation_id || '|' || COALESCE(prior_generation_id, '<null>') || '|' || source
FROM changed_since_activations WHERE scope_id NOT LIKE 'eshu:%' ORDER BY activation_seq`)
}

func TestJournalSweeperJournalsActiveGenerationsWithUnknownPrior(t *testing.T) {
	l := openLedgerDB(t)
	l.seedScope(t, "a")
	l.seedGeneration(t, "a", "a0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
	l.seedGeneration(t, "a", "a1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
	l.setActive(t, "a", "a1")
	l.seedScope(t, "no-active")
	l.seedGeneration(t, "no-active", "n0", false, "pending", time.Time{}, time.Time{})

	store := linksfreshnessstore.NewJournalStore(l.store)
	result, err := store.Journal(l.ctx, 0)
	if err != nil {
		t.Fatalf("Journal: %v", err)
	}
	// The bootstrap seeds the eshu:global scope with an active generation;
	// it is journaled like any other scope.
	if result.SweeperRows != 2 || result.BackfillRows != 0 {
		t.Fatalf("Journal = %+v, want two sweeper rows (a1, eshu:global) and no backfill", result)
	}
	if got := strings.Join(l.journalRows(t), ","); got != "a|a1|<null>|sweeper" {
		t.Fatalf("journal = %s", got)
	}
	again, err := store.Journal(l.ctx, 0)
	if err != nil || again.SweeperRows != 0 {
		t.Fatalf("second Journal = %+v, %v; want a no-op", again, err)
	}
}

func TestJournalBackfillWalksRetainedChainInOrder(t *testing.T) {
	l := openLedgerDB(t)
	t0, t1, t2 := fixtureEpoch, fixtureEpoch.Add(time.Hour), fixtureEpoch.Add(2*time.Hour)
	// chain: full c0 -> delta c1 -> delta c2 (active)
	l.seedScope(t, "chain")
	l.seedGeneration(t, "chain", "c0", false, "superseded", t0, t1)
	l.seedGeneration(t, "chain", "c1", true, "superseded", t1, t2)
	l.seedGeneration(t, "chain", "c2", true, "active", t2, time.Time{})
	l.seedGeneration(t, "chain", "c-never", false, "superseded", time.Time{}, t1)
	l.setActive(t, "chain", "c2")
	// ambiguous: two generations activated at the same instant after the root
	l.seedScope(t, "ambiguous")
	l.seedGeneration(t, "ambiguous", "m0", false, "superseded", t0, t1)
	l.seedGeneration(t, "ambiguous", "m1a", true, "superseded", t1, t2)
	l.seedGeneration(t, "ambiguous", "m1b", true, "superseded", t1, t2)
	l.seedGeneration(t, "ambiguous", "m2", true, "active", t2, time.Time{})
	l.setActive(t, "ambiguous", "m2")

	store := linksfreshnessstore.NewJournalStore(l.store)
	result, err := store.Journal(l.ctx, 10)
	if err != nil {
		t.Fatalf("Journal: %v", err)
	}
	want := []string{
		"chain|c0|<null>|backfill", "chain|c1|c0|backfill", "chain|c2|c1|backfill",
		"ambiguous|m2|<null>|sweeper",
	}
	if got := l.journalRows(t); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("journal = %v, want %v", got, want)
	}
	if result.BackfillScopes != 1 || result.BackfillRows != 3 || result.SweeperRows != 2 {
		t.Fatalf("Journal = %+v", result)
	}
}

func TestJournalPassIsExclusiveAcrossReplicas(t *testing.T) {
	l := openLedgerDB(t)
	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	var got bool
	if err := holder.QueryRowContext(l.ctx, `SELECT pg_try_advisory_xact_lock($1::integer, 0)`,
		linksfreshnessstore.SlotLockClass).Scan(&got); err != nil || !got {
		t.Fatalf("hold journal lock: %v %v", got, err)
	}
	result, err := linksfreshnessstore.NewJournalStore(l.store).Journal(l.ctx, 10)
	if err != nil || !result.Skipped {
		t.Fatalf("Journal with the lock held = %+v, %v; want skipped", result, err)
	}
}

func TestOrphanScopeRowsAreDeletedUnlessLinking(t *testing.T) {
	l := openLedgerDB(t)
	l.seedScope(t, "doomed")
	l.seedGeneration(t, "doomed", "x0", false, "active", fixtureEpoch, time.Time{})
	l.insertFacts(t, "doomed", "x0", baseFacts())
	l.journal(t, "doomed", "x0", "")
	mustLink(t, linksfreshnessstore.NewLinkWriter(l.store), l, "doomed")
	l.exec(t, `DELETE FROM ingestion_scopes WHERE scope_id = 'doomed'`)

	store := linksfreshnessstore.NewJournalStore(l.store)
	orphans, err := store.OrphanScopes(l.ctx, 10)
	if err != nil || len(orphans) != 1 || orphans[0] != "doomed" {
		t.Fatalf("OrphanScopes = %v, %v", orphans, err)
	}
	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := holder.ExecContext(l.ctx, `SELECT 1 FROM changed_since_scope_cursor WHERE scope_id = 'doomed' FOR UPDATE`); err != nil {
		t.Fatalf("hold cursor: %v", err)
	}
	if deleted, err := store.DeleteOrphanScope(l.ctx, "doomed"); err != nil || deleted {
		t.Fatalf("DeleteOrphanScope with the cursor held = %v, %v; want skipped", deleted, err)
	}
	_ = holder.Rollback()
	if deleted, err := store.DeleteOrphanScope(l.ctx, "doomed"); err != nil || !deleted {
		t.Fatalf("DeleteOrphanScope = %v, %v", deleted, err)
	}
	for _, table := range []string{
		"changed_since_activations", "changed_since_key_state", "changed_since_scope_cursor",
		"changed_since_links", "changed_since_link_deltas", "changed_since_link_bucket_counts",
	} {
		if n := l.queryInt(t, `SELECT count(*) FROM `+table); n != 0 {
			t.Fatalf("%s keeps %d rows of the deleted scope", table, n)
		}
	}
}

func TestStatsReportsBacklogAndLag(t *testing.T) {
	l := openLedgerDB(t)
	l.seedScope(t, "s")
	l.seedGeneration(t, "s", "s0", false, "active", fixtureEpoch, time.Time{})
	l.journal(t, "s", "s0", "")
	store := linksfreshnessstore.NewJournalStore(l.store)
	store.Now = func() time.Time { return fixtureEpoch.Add(90 * time.Second) }
	stats, err := store.Stats(l.ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.BacklogRows != 1 || stats.LagSeconds != 90 {
		t.Fatalf("Stats = %+v, want backlog 1 and lag 90s", stats)
	}
	scopes, err := store.BacklogScopes(l.ctx, 10)
	if err != nil || len(scopes) != 1 || scopes[0] != "s" {
		t.Fatalf("BacklogScopes = %v, %v", scopes, err)
	}
	mustLink(t, linksfreshnessstore.NewLinkWriter(l.store), l, "s")
	if stats, err = store.Stats(l.ctx); err != nil || stats.BacklogRows != 0 || stats.LagSeconds != 0 {
		t.Fatalf("Stats after link = %+v, %v", stats, err)
	}
}
