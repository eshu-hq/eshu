// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// TestActivationObligationRetentionCascadeLive proves the obligation foreign
// key policy against the real generation-retention prune: an obligation on a
// superseded generation must not block the prune, must go with its
// generation, and must leave the active generation's obligation alone.
func TestActivationObligationRetentionCascadeLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_retention")
	now := time.Now().UTC()
	seedScope(t, ctx, database, "retention-obligation")
	seedSupersededGeneration(t, ctx, database,
		"retention-obligation", "retention-obligation-old", now.Add(-100*24*time.Hour))
	for _, generationID := range []string{"retention-obligation-old", "retention-obligation-active"} {
		if err := activation.Insert(ctx, postgres.SQLDB{DB: database}, "retention-obligation", generationID,
			postgres.ProjectorWorkItemID("retention-obligation", generationID)); err != nil {
			t.Fatal(err)
		}
	}
	retention := postgres.NewGenerationRetentionStore(postgres.SQLDB{DB: database})
	retention.Now = func() time.Time { return now }
	result, err := retention.PruneSupersededGenerations(ctx, postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            1_000_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "7584-activation-retention-live",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() with an obligation row = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}
	var remaining []string
	rows, err := database.QueryContext(ctx, `SELECT generation_id FROM activation_obligations ORDER BY generation_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		remaining = append(remaining, id)
	}
	if len(remaining) != 1 || remaining[0] != "retention-obligation-active" {
		t.Fatalf("obligations after prune = %v, want only the active generation's", remaining)
	}
}

// TestActivationObligationPruneLive: the prune deletes only finished rows
// older than the retention window, oldest first, at most limit per call, and
// never an open row.
func TestActivationObligationPruneLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_prune")
	seedScope(t, ctx, database, "prune-obligation")
	type seed struct {
		generation string
		state      string
		finished   string
	}
	seeds := []seed{
		{"prune-completed-old", "completed", "clock_timestamp() - interval '3 hours'"},
		{"prune-obsolete-old", "obsolete", "clock_timestamp() - interval '2 hours'"},
		{"prune-completed-new", "completed", "clock_timestamp()"},
		{"prune-pending", "pending", "NULL"},
	}
	for _, s := range seeds {
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ($1, 'prune-obligation', 'snapshot', now(), now(), 'superseded', now())`, s.generation); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO activation_obligations (scope_id, generation_id, work_item_id, state, finished_at)
VALUES ('prune-obligation', $1, 'w', $2, `+s.finished+`)`, s.generation, s.state); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ('prune-leased', 'prune-obligation', 'snapshot', now(), now(), 'superseded', now());
INSERT INTO activation_obligations (scope_id, generation_id, work_item_id, state, lease_owner, lease_until, created_at)
VALUES ('prune-obligation', 'prune-leased', 'w', 'leased', 'o', clock_timestamp() + interval '1 hour',
    clock_timestamp() - interval '5 hours')`); err != nil {
		t.Fatal(err)
	}
	store := activation.NewStore(postgres.SQLDB{DB: database})
	remaining := func() map[string]bool {
		t.Helper()
		rows, err := database.QueryContext(ctx, `SELECT generation_id FROM activation_obligations`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		got := map[string]bool{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			got[id] = true
		}
		return got
	}
	deleted, err := store.Prune(ctx, time.Hour, 1)
	if err != nil || deleted != 1 {
		t.Fatalf("first prune deleted=%d err=%v, want 1", deleted, err)
	}
	if got := remaining(); got["prune-completed-old"] || !got["prune-obsolete-old"] {
		t.Fatalf("first prune left %v, want the oldest finished row gone first", got)
	}
	deleted, err = store.Prune(ctx, time.Hour, 10)
	if err != nil || deleted != 1 {
		t.Fatalf("second prune deleted=%d err=%v, want 1", deleted, err)
	}
	got := remaining()
	for _, keep := range []string{"prune-completed-new", "prune-pending", "prune-leased"} {
		if !got[keep] {
			t.Fatalf("prune removed %s; remaining=%v", keep, got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("remaining after prune = %v, want three rows", got)
	}
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ByState[activation.StatePending] != 1 || stats.ByState[activation.StateLeased] != 1 ||
		stats.ByState[activation.StateCompleted] != 1 || stats.ByState[activation.StateObsolete] != 0 {
		t.Fatalf("stats = %+v, want pending=1 leased=1 completed=1 obsolete=0", stats.ByState)
	}
	if stats.OldestOpenAge < 4*time.Hour {
		t.Fatalf("oldest open age = %s, want the five-hour leased row", stats.OldestOpenAge)
	}
}

// TestActivationObligationPruneIsNotStarvedByInapplicableRowsLive (it
// kills the CTE-only state-filter mutant M17): inapplicable rows are terminal
// and never pruned, so when one is older than every completed row the prune
// must still delete the completed row instead of selecting the inapplicable
// one first and deleting nothing.
func TestActivationObligationPruneIsNotStarvedByInapplicableRowsLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_prune_starve")
	seedScope(t, ctx, database, "prune-starve")
	for _, s := range []struct{ generation, state, finished string }{
		{"prune-starve-inapplicable", "inapplicable", "clock_timestamp() - interval '5 hours'"},
		{"prune-starve-completed", "completed", "clock_timestamp() - interval '2 hours'"},
	} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ($1, 'prune-starve', 'snapshot', now(), now(), 'superseded', now())`, s.generation); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO activation_obligations (scope_id, generation_id, work_item_id, state, finished_at)
VALUES ('prune-starve', $1, 'w', $2, `+s.finished+`)`, s.generation, s.state); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := activation.NewStore(postgres.SQLDB{DB: database}).Prune(ctx, time.Hour, 1)
	if err != nil || deleted != 1 {
		t.Fatalf("Prune(limit=1) deleted %d err=%v, want the completed row", deleted, err)
	}
	var remaining string
	if err := database.QueryRowContext(ctx, `SELECT string_agg(generation_id, ',') FROM activation_obligations`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != "prune-starve-inapplicable" {
		t.Fatalf("remaining obligations = %q, want only the inapplicable row", remaining)
	}
}
