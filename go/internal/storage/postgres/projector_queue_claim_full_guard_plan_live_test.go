// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestProjectorClaimFullGuardPlanShape is the #7473 plan proof on the #7115
// harness shape: the spare and hold add index-backed probes and no new
// sequential scans or row locks. The before text is derived from the shipped
// constant, so it cannot drift. Both plans are always logged for the
// evidence record.
func TestProjectorClaimFullGuardPlanShape(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardRaceScope(t, database, at, at.Add(-time.Hour))
	if _, err := database.Exec(`VACUUM ANALYZE fact_work_items`); err != nil {
		t.Fatalf("vacuum analyze: %v", err)
	}
	before, counts := fullGuardClaimBeforeText(claimProjectorWorkQuery)
	for name, count := range counts {
		if count != 1 {
			t.Fatalf("before text reverses %s %d times, want 1", name, count)
		}
	}

	beforePlan := markGuardExplain(t, database, before, at)
	afterPlan := markGuardExplain(t, database, claimProjectorWorkQuery, at)
	t.Logf("BEFORE plan:\n%s", beforePlan)
	t.Logf("AFTER plan:\n%s", afterPlan)

	// The spare and hold must be present in the plan, with the row-form
	// order comparison. Scan choices on six rows are the planner's
	// prerogative (it seq-scans either way); the index-backed proof lives
	// at scale below.
	for _, want := range []string{
		"is_delta",
		"ROW(waiting_generation.ingested_at",
	} {
		if !strings.Contains(afterPlan, want) {
			t.Errorf("after plan lacks %q", want)
		}
	}
	// No new row locks versus the before text.
	if got, want := strings.Count(afterPlan, "LockRows"), strings.Count(beforePlan, "LockRows"); got != want {
		t.Errorf("LockRows nodes: %d after, %d before", got, want)
	}
}

// TestProjectorClaimFullGuardPlanShapeAtScale repeats the plan proof at
// 2,000 scopes, where the tiny-table sequential scans give way to index
// probes: the hold's waiting-generation probe must use the primary key
// there, and the per-table sequential-scan set must still match the before
// text. Newer generations are deltas so both the spare and the hold engage.
func TestProjectorClaimFullGuardPlanShapeAtScale(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 8)
	at := time.Now().UTC().Truncate(time.Second)
	for _, stmt := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
		 SELECT 'fscale-'||i, 'repository','git','fscale-'||i,'git','fscale-'||i, now(), now(), 'active' FROM generate_series(1,2000) i`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		 SELECT 'fscale-'||i||'-g1','fscale-'||i,'push', now()-interval '2 hours', now()-interval '2 hours','pending' FROM generate_series(1,2000) i`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		 SELECT 'fscale-'||i||'-g0','fscale-'||i,'push', now()-interval '3 hours', now()-interval '3 hours','pending' FROM generate_series(1,200) i`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, visible_at, payload, created_at, updated_at)
		 SELECT 'projector_fscale-'||i||'_g1','fscale-'||i,'fscale-'||i||'-g1','projector','source_local','pending',0, now()-interval '2 hours','{}'::jsonb, now()-interval '2 hours', now()-interval '2 hours' FROM generate_series(1,2000) i`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, visible_at, payload, created_at, updated_at)
		 SELECT 'projector_fscale-'||i||'_g0','fscale-'||i,'fscale-'||i||'-g0','projector','source_local','pending',0, now()-interval '3 hours','{}'::jsonb, now()-interval '3 hours', now()-interval '3 hours' FROM generate_series(1,200) i`,
		`UPDATE scope_generations SET is_delta = true WHERE generation_id LIKE 'fscale-%-g1'`,
		`UPDATE fact_work_items SET status = 'retrying', visible_at = now() + interval '1 hour', next_attempt_at = now() + interval '1 hour' WHERE work_item_id = 'projector_fscale-1_g0'`,
		`VACUUM ANALYZE fact_work_items`, `ANALYZE scope_generations`, `ANALYZE ingestion_scopes`,
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("scale seed: %v", err)
		}
	}
	before, _ := fullGuardClaimBeforeText(claimProjectorWorkQuery)
	beforePlan := markGuardExplain(t, database, before, at)
	afterPlan := markGuardExplain(t, database, claimProjectorWorkQuery, at)
	for _, want := range []string{
		"Index Scan using fact_work_items_scope_generation_idx on fact_work_items waiting",
		"Index Scan using scope_generations_pkey on scope_generations waiting_generation",
		"Index Scan using scope_generations_pkey on scope_generations held_generation",
	} {
		if !strings.Contains(afterPlan, want) {
			t.Errorf("scale after plan lacks %q", want)
		}
	}
	beforeScans, afterScans := markGuardSeqScans(beforePlan), markGuardSeqScans(afterPlan)
	for table, want := range beforeScans {
		if got := afterScans[table]; got != want {
			t.Errorf("scale Seq Scan on %s: %d after, %d before", table, got, want)
		}
	}
	for table, got := range afterScans {
		if _, ok := beforeScans[table]; !ok {
			t.Errorf("scale Seq Scan on %s: %d after, none before", table, got)
		}
	}
	t.Logf("scale plan lines before=%d after=%d", len(strings.Split(beforePlan, "\n")), len(strings.Split(afterPlan, "\n")))
}

// TestProjectorClaimFullGuardLockSet proves the #7473 lock set behaviorally:
// the claim takes the holder full's work row and its scope's fence row, and
// locks nothing else new. The held delta row is never locked, the spared
// full's generation row is never locked, and ingestion_scopes stays unlocked.
// The claim runs inside an open transaction and a probe session tries each
// row with NOWAIT.
func TestProjectorClaimFullGuardLockSet(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	at := time.Now().UTC().Truncate(time.Second)
	fullGuardSeedScope(t, database, at, "retrying", at.Add(-time.Hour), at.Add(-2*time.Hour), false, true)
	seedClaimMaintenanceScopes(t, database, "scope-o")
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-o", "gen-o", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	ctx := context.Background()

	claimer, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin claimer: %v", err)
	}
	defer func() { _ = claimer.Rollback() }()
	var claimedGeneration string
	if err := claimer.QueryRow(claimProjectorWorkQuery, at, "claimer", at.Add(time.Minute), "").Scan(
		new(string), new(string), new(string), new(string), new(string), new(bool), new(string), new(string),
		&claimedGeneration, new(int), new(time.Time), new(time.Time), new(string), new(string), new(string), new([]byte),
	); err != nil {
		t.Fatalf("claim in open transaction: %v", err)
	}
	if claimedGeneration != "gen-fg1" {
		t.Fatalf("claimed %q, want the gen-fg1 full", claimedGeneration)
	}

	for _, probe := range []struct {
		query      string
		args       []any
		wantLocked bool
		what       string
	}{
		{`SELECT 1 FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE NOWAIT`, []any{projectorWorkItemID("scope-fg", "gen-fg1")}, true, "claimed gen-fg1 work row"},
		{`SELECT 1 FROM projector_scope_claim_fences WHERE scope_id = 'scope-fg' FOR NO KEY UPDATE NOWAIT`, nil, true, "claimed scope-fg fence row"},
		{`SELECT 1 FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE NOWAIT`, []any{projectorWorkItemID("scope-fg", "gen-fg2")}, false, "held gen-fg2 work row"},
		{`SELECT 1 FROM scope_generations WHERE generation_id = 'gen-fg1' FOR NO KEY UPDATE NOWAIT`, nil, false, "spared gen-fg1 generation row"},
		{`SELECT 1 FROM ingestion_scopes WHERE scope_id = 'scope-fg' FOR NO KEY UPDATE NOWAIT`, nil, false, "scope-fg ingestion_scopes row"},
		{`SELECT 1 FROM projector_scope_claim_fences WHERE scope_id = 'scope-o' FOR NO KEY UPDATE NOWAIT`, nil, false, "untouched scope-o fence row"},
	} {
		locked := probeRowLockedArgs(t, database, probe.query, probe.args...)
		if locked != probe.wantLocked {
			t.Fatalf("%s locked = %v, want %v", probe.what, locked, probe.wantLocked)
		}
	}
}
