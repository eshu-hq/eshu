// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// seedUpgradeBackfillRepo seeds one active scope/generation/repo with a
// completed code_calls intent and a code_reachability_repository_watermarks row
// whose updated_at is NEWER than the intent's completed_at AND whose
// verdict_schema_epoch is 0 — the exact post-migration, pre-upgrade state of an
// already-indexed repo (#5376 P1). Optional content_entities rows model the
// repo's Ruby (or non-Ruby) code.
func seedUpgradeBackfillRepo(t *testing.T, ctx context.Context, db *sql.DB, suffix string, entities func(scopeID, repoID string) []string) (scopeID, repoID string) {
	t.Helper()
	scopeID = "scope-" + suffix
	generationID := "gen-" + suffix
	repoID = "repo-" + suffix
	sourceRunID := "run-" + suffix
	completedAt := time.Now().UTC().Add(-1 * time.Hour)
	watermarkAt := completedAt.Add(30 * time.Minute) // watermark NEWER than the intent

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed exec %q: %v", q, err)
		}
	}

	exec(`INSERT INTO ingestion_scopes
	  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
	   observed_at, ingested_at, status, active_generation_id, payload)
	  VALUES ($1,'repository','git',$1,'git',$1,$2,$2,'active',$3, jsonb_build_object('repo_id',$4::text))
	  ON CONFLICT (scope_id) DO NOTHING`, scopeID, completedAt, generationID, repoID)
	exec(`INSERT INTO scope_generations
	  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
	  VALUES ($1,$2,'manual',$3,$3,'active',$3) ON CONFLICT (generation_id) DO NOTHING`, generationID, scopeID, completedAt)
	exec(`INSERT INTO shared_projection_acceptance
	  (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
	  VALUES ($1,$2,$3,$4,$5,$5) ON CONFLICT DO NOTHING`, scopeID, repoID, sourceRunID, generationID, completedAt)
	exec(`INSERT INTO shared_projection_intents
	  (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id, repository_id,
	   source_run_id, generation_id, payload, created_at, completed_at)
	  VALUES ($1,'code_calls',$2,$3,$4,$4,$5,$6,'{}'::jsonb,$7,$7)`,
		"intent-"+suffix, repoID, scopeID, repoID, sourceRunID, generationID, completedAt)
	// The pre-upgrade watermark: newer than the intent, epoch 0.
	exec(`INSERT INTO code_reachability_repository_watermarks
	  (scope_id, generation_id, repository_id, truncated, updated_at, verdict_schema_epoch)
	  VALUES ($1,$2,$3,false,$4,0)`, scopeID, generationID, repoID, watermarkAt)

	if entities != nil {
		for _, stmt := range entities(scopeID, repoID) {
			exec(stmt)
		}
	}
	return scopeID, repoID
}

func rubyControllerEntitiesSQL(_, repoID string) []string {
	now := time.Now().UTC().Format(time.RFC3339)
	e := func(id, etype, name string, meta string) string {
		return fmt.Sprintf(`INSERT INTO content_entities
		 (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, metadata, indexed_at)
		 VALUES ('%s','%s','app/x.rb','%s','%s',1,3,'ruby','', '%s'::jsonb, '%s')`,
			id, repoID, etype, name, meta, now)
	}
	return []string{
		e(repoID+":fn:LegacyController:generate", "Function", "generate",
			`{"dead_code_root_kinds":["ruby.rails_controller_action"],"class_context":"LegacyController"}`),
		e(repoID+":class:LegacyController", "Class", "LegacyController",
			`{"qualified_name":"LegacyController","qualified_bases":["ApplicationRecord"]}`),
		e(repoID+":class:ApplicationRecord", "Class", "ApplicationRecord",
			`{"qualified_name":"ApplicationRecord","qualified_bases":["ActiveRecord::Base"]}`),
	}
}

func nonRubyEntitiesSQL(_, repoID string) []string {
	now := time.Now().UTC().Format(time.RFC3339)
	return []string{fmt.Sprintf(`INSERT INTO content_entities
	 (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, metadata, indexed_at)
	 VALUES ('%s:fn:handler','%s','main.go','Function','Handler',1,3,'go','', '{}'::jsonb, '%s')`, repoID, repoID, now)}
}

func openUpgradeBackfillLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #5376 upgrade-backfill proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	return ctx, db
}

func loadedRepoIDs(t *testing.T, ctx context.Context, store *CodeReachabilityStore) map[string]bool {
	t.Helper()
	inputs, err := store.LoadPendingCodeReachabilityInputs(ctx, 100)
	if err != nil {
		t.Fatalf("LoadPendingCodeReachabilityInputs: %v", err)
	}
	got := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		got[in.RepositoryID] = true
	}
	return got
}

// TestCodeReachabilityUpgradeBackfillReschedules is the #5376 P1 FAILING-FIRST
// regression: an already-indexed repo whose reachability watermark is newer than
// its last completed code intent (so the pre-P1 loader skips it forever) but
// whose verdict_schema_epoch is 0 MUST be re-scheduled by the loader, so
// BuildCodeRootVerdicts runs for it on an upgraded deployment. Red on the
// pre-P1 loader (no epoch predicate); green after.
func TestCodeReachabilityUpgradeBackfillReschedules(t *testing.T) {
	ctx, db := openUpgradeBackfillLiveDB(t)
	store := NewCodeReachabilityStore(SQLDB{DB: db})
	suffix := fmt.Sprintf("p1a-%d", time.Now().UnixNano())
	_, repoID := seedUpgradeBackfillRepo(t, ctx, db, suffix, rubyControllerEntitiesSQL)

	if !loadedRepoIDs(t, ctx, store)[repoID] {
		t.Fatalf("upgrade-backfill: repo %q with a stale (epoch 0) watermark newer than its intent was NOT re-scheduled", repoID)
	}
}

// TestCodeReachabilityUpgradeBackfillRoundTripAndAntiLoop proves the runner
// re-projects a Ruby repo (populating code_root_verdicts), stamps the watermark
// with the current epoch, and does NOT re-schedule it on the next loader call.
func TestCodeReachabilityUpgradeBackfillRoundTripAndAntiLoop(t *testing.T) {
	ctx, db := openUpgradeBackfillLiveDB(t)
	store := NewCodeReachabilityStore(SQLDB{DB: db})
	suffix := fmt.Sprintf("p1b-%d", time.Now().UnixNano())
	scopeID, repoID := seedUpgradeBackfillRepo(t, ctx, db, suffix, rubyControllerEntitiesSQL)

	runner := &reducer.CodeReachabilityProjectionRunner{InputLoader: store, RowWriter: store, Config: reducer.CodeReachabilityProjectionRunnerConfig{BatchLimit: 10}}
	if _, err := runner.ProcessOnce(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}

	var verdicts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM code_root_verdicts WHERE repository_id=$1`, repoID).Scan(&verdicts); err != nil {
		t.Fatalf("count verdicts: %v", err)
	}
	if verdicts == 0 {
		t.Fatalf("expected code_root_verdicts populated for the Ruby repo after re-projection")
	}
	var epoch int
	if err := db.QueryRowContext(ctx, `SELECT verdict_schema_epoch FROM code_reachability_repository_watermarks WHERE scope_id=$1 AND repository_id=$2`, scopeID, repoID).Scan(&epoch); err != nil {
		t.Fatalf("read epoch: %v", err)
	}
	if epoch != CodeReachabilityVerdictSchemaEpoch {
		t.Fatalf("watermark epoch = %d, want %d", epoch, CodeReachabilityVerdictSchemaEpoch)
	}
	if loadedRepoIDs(t, ctx, store)[repoID] {
		t.Fatalf("anti-loop: repo re-scheduled after its watermark was stamped with the current epoch")
	}
}

// TestCodeReachabilityUpgradeBackfillZeroVerdictAntiLoop is the case naive
// "verdict count == 0 => re-schedule" can never pass: a NO-Ruby repo legitimately
// produces zero verdicts, yet must be stamped with the current epoch after one
// projection and never re-scheduled.
func TestCodeReachabilityUpgradeBackfillZeroVerdictAntiLoop(t *testing.T) {
	ctx, db := openUpgradeBackfillLiveDB(t)
	store := NewCodeReachabilityStore(SQLDB{DB: db})
	suffix := fmt.Sprintf("p1c-%d", time.Now().UnixNano())
	scopeID, repoID := seedUpgradeBackfillRepo(t, ctx, db, suffix, nonRubyEntitiesSQL)

	// Pre-upgrade: the loader schedules it (epoch 0 < current).
	if !loadedRepoIDs(t, ctx, store)[repoID] {
		t.Fatalf("no-Ruby repo with a stale watermark should be scheduled once")
	}
	runner := &reducer.CodeReachabilityProjectionRunner{InputLoader: store, RowWriter: store, Config: reducer.CodeReachabilityProjectionRunnerConfig{BatchLimit: 10}}
	if _, err := runner.ProcessOnce(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}

	var verdicts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM code_root_verdicts WHERE repository_id=$1`, repoID).Scan(&verdicts); err != nil {
		t.Fatalf("count verdicts: %v", err)
	}
	if verdicts != 0 {
		t.Fatalf("no-Ruby repo must produce zero verdicts, got %d", verdicts)
	}
	var epoch int
	if err := db.QueryRowContext(ctx, `SELECT verdict_schema_epoch FROM code_reachability_repository_watermarks WHERE scope_id=$1 AND repository_id=$2`, scopeID, repoID).Scan(&epoch); err != nil {
		t.Fatalf("read epoch: %v", err)
	}
	if epoch != CodeReachabilityVerdictSchemaEpoch {
		t.Fatalf("no-Ruby watermark epoch = %d, want %d", epoch, CodeReachabilityVerdictSchemaEpoch)
	}
	if loadedRepoIDs(t, ctx, store)[repoID] {
		t.Fatalf("anti-loop: zero-verdict no-Ruby repo re-scheduled forever (the naive count==0 defect)")
	}
}
