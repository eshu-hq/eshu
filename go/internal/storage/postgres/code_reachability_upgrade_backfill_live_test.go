// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// upgradeBackfillLargeBatchLimit is used both for the loader observation and the
// runner mutation path so a shared dev DB that has accumulated other pending
// repos can never truncate or crowd out this test's seeded repo (the loader
// orders completed_at ASC, repository_id ASC and applies the limit as a SQL
// LIMIT; older leftovers would otherwise sort first and consume a small batch).
const upgradeBackfillLargeBatchLimit = 1_000_000

// registerUpgradeBackfillCleanup deletes every row a seed created for one
// scope/repo when the test finishes (t.Cleanup, LIFO). It is the #5376 P1 F2
// root-cause fix: without it these live tests leave permanent epoch-0 pending
// pollution on a persistent ESHU_POSTGRES_DSN, which then crowds out later
// runs' seeded repos under a bounded ProcessOnce batch. Best-effort: a cleanup
// failure is logged, not fatal.
func registerUpgradeBackfillCleanup(t *testing.T, db *sql.DB, scopeID, repoID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Child-first so the cleanup is correct even if a table's FK is not
		// ON DELETE CASCADE; ingestion_scopes last cascades any stragglers.
		stmts := []struct {
			q    string
			args []any
		}{
			{`DELETE FROM code_root_verdicts WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM code_reachability_rows WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM code_reachability_repository_watermarks WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM shared_projection_intents WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM shared_projection_acceptance WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM content_entities WHERE repo_id=$1`, []any{repoID}},
			{`DELETE FROM scope_generations WHERE scope_id=$1`, []any{scopeID}},
			{`DELETE FROM ingestion_scopes WHERE scope_id=$1`, []any{scopeID}},
		}
		for _, s := range stmts {
			if _, err := db.ExecContext(ctx, s.q, s.args...); err != nil {
				t.Logf("cleanup %q: %v", s.q, err)
			}
		}
	})
}

// seedUpgradeBackfillRepo seeds one active scope/generation/repo with a
// completed code_calls intent and a code_reachability_repository_watermarks row
// whose updated_at is NEWER than the intent's completed_at AND whose
// verdict_schema_epoch is 0 — the exact post-migration, pre-upgrade state of an
// already-indexed repo (#5376 P1). `age` is how far back the intent's
// completed_at is set; optional content_entities model the repo's Ruby (or
// non-Ruby) code. Registers a t.Cleanup that removes every seeded row.
func seedUpgradeBackfillRepo(t *testing.T, ctx context.Context, db *sql.DB, suffix string, age time.Duration, entities func(scopeID, repoID string) []string) (scopeID, repoID string) {
	t.Helper()
	scopeID = "scope-" + suffix
	generationID := "gen-" + suffix
	repoID = "repo-" + suffix
	sourceRunID := "run-" + suffix
	completedAt := time.Now().UTC().Add(-age)
	watermarkAt := completedAt.Add(1 * time.Minute) // watermark NEWER than the intent

	registerUpgradeBackfillCleanup(t, db, scopeID, repoID)

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

// seedStalePendingBacklog seeds n minimal stale pending repos (no content, so
// zero roots) whose completed_at is OLDER than a subsequently-seeded target, so
// they sort FIRST in the loader's completed_at ASC order — the exact backlog
// that made a bounded ProcessOnce skip the target. Each is cleaned up.
func seedStalePendingBacklog(t *testing.T, ctx context.Context, db *sql.DB, testTag string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		suffix := fmt.Sprintf("%s-stale-%d-%d", testTag, i, time.Now().UnixNano())
		seedUpgradeBackfillRepo(t, ctx, db, suffix, 3*time.Hour, nil)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	return ctx, db
}

// testSuffix builds a per-test-unique, traceable id suffix from the test name
// plus a nanosecond stamp, so each test's rows live in their own scope and no
// two invocations collide.
func testSuffix(t *testing.T) string {
	return fmt.Sprintf("%s-%d", strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()), time.Now().UnixNano())
}

func loadedRepoIDs(t *testing.T, ctx context.Context, store *CodeReachabilityStore) map[string]bool {
	t.Helper()
	inputs, err := store.LoadPendingCodeReachabilityInputs(ctx, upgradeBackfillLargeBatchLimit)
	if err != nil {
		t.Fatalf("LoadPendingCodeReachabilityInputs: %v", err)
	}
	got := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		got[in.RepositoryID] = true
	}
	return got
}

func upgradeBackfillRunner(store *CodeReachabilityStore) *reducer.CodeReachabilityProjectionRunner {
	return &reducer.CodeReachabilityProjectionRunner{
		InputLoader: store,
		RowWriter:   store,
		Config:      reducer.CodeReachabilityProjectionRunnerConfig{BatchLimit: upgradeBackfillLargeBatchLimit},
	}
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
	_, repoID := seedUpgradeBackfillRepo(t, ctx, db, testSuffix(t), time.Hour, rubyControllerEntitiesSQL)

	if !loadedRepoIDs(t, ctx, store)[repoID] {
		t.Fatalf("upgrade-backfill: repo %q with a stale (epoch 0) watermark newer than its intent was NOT re-scheduled", repoID)
	}
}

// TestCodeReachabilityUpgradeBackfillRoundTripAndAntiLoop proves the runner
// re-projects a Ruby repo (populating code_root_verdicts), stamps the watermark
// with the current epoch, and does NOT re-schedule it on the next loader call —
// EVEN with a backlog of older stale pending repos ahead of it in the loader's
// order (the determinism proof the reviewer specified).
func TestCodeReachabilityUpgradeBackfillRoundTripAndAntiLoop(t *testing.T) {
	ctx, db := openUpgradeBackfillLiveDB(t)
	store := NewCodeReachabilityStore(SQLDB{DB: db})

	// Backlog of >10 OLDER stale pending repos: they sort before the target.
	seedStalePendingBacklog(t, ctx, db, "rtbacklog", 12)
	scopeID, repoID := seedUpgradeBackfillRepo(t, ctx, db, testSuffix(t), time.Hour, rubyControllerEntitiesSQL)

	if _, err := upgradeBackfillRunner(store).ProcessOnce(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}

	var verdicts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM code_root_verdicts WHERE repository_id=$1`, repoID).Scan(&verdicts); err != nil {
		t.Fatalf("count verdicts: %v", err)
	}
	if verdicts == 0 {
		t.Fatalf("expected code_root_verdicts populated for the Ruby repo after re-projection (even behind a stale backlog)")
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
// projection and never re-scheduled — again behind a stale backlog.
func TestCodeReachabilityUpgradeBackfillZeroVerdictAntiLoop(t *testing.T) {
	ctx, db := openUpgradeBackfillLiveDB(t)
	store := NewCodeReachabilityStore(SQLDB{DB: db})

	seedStalePendingBacklog(t, ctx, db, "zvbacklog", 12)
	scopeID, repoID := seedUpgradeBackfillRepo(t, ctx, db, testSuffix(t), time.Hour, nonRubyEntitiesSQL)

	// Pre-upgrade: the loader schedules it (epoch 0 < current).
	if !loadedRepoIDs(t, ctx, store)[repoID] {
		t.Fatalf("no-Ruby repo with a stale watermark should be scheduled once")
	}
	if _, err := upgradeBackfillRunner(store).ProcessOnce(ctx, time.Now().UTC()); err != nil {
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
