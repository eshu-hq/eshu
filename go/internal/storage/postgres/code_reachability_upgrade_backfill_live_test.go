// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer/codeintel"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
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

func loadedRepoIDs(t *testing.T, ctx context.Context, store *reachabilitystore.CodeReachabilityStore) map[string]bool {
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

func upgradeBackfillRunner(store *reachabilitystore.CodeReachabilityStore) *codeintel.CodeReachabilityProjectionRunner {
	return &codeintel.CodeReachabilityProjectionRunner{
		InputLoader: store,
		RowWriter:   store,
		Config:      codeintel.CodeReachabilityProjectionRunnerConfig{BatchLimit: upgradeBackfillLargeBatchLimit},
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
	store := reachabilitystore.NewCodeReachabilityStore(SQLDB{DB: db})
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
	store := reachabilitystore.NewCodeReachabilityStore(SQLDB{DB: db})

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
	if epoch != reachabilitystore.CodeReachabilityVerdictSchemaEpoch {
		t.Fatalf("watermark epoch = %d, want %d", epoch, reachabilitystore.CodeReachabilityVerdictSchemaEpoch)
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
	store := reachabilitystore.NewCodeReachabilityStore(SQLDB{DB: db})

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
	if epoch != reachabilitystore.CodeReachabilityVerdictSchemaEpoch {
		t.Fatalf("no-Ruby watermark epoch = %d, want %d", epoch, reachabilitystore.CodeReachabilityVerdictSchemaEpoch)
	}
	if loadedRepoIDs(t, ctx, store)[repoID] {
		t.Fatalf("anti-loop: zero-verdict no-Ruby repo re-scheduled forever (the naive count==0 defect)")
	}
}

// seedTruncationEpochRepo seeds one repo in the exact pre-#7547 state: a
// watermark stamped at epoch 3 with truncated=false and updated_at newer than
// the last completed code_calls intent, plus a code_calls chain of `hops` edges
// n0->n1->... and `roots` content_entities roots (n0 only). Epoch 3 is a
// literal on purpose: it is the epoch the old logic stamped.
func seedTruncationEpochRepo(t *testing.T, ctx context.Context, db *sql.DB, suffix string, hops int, withRoot bool) (scopeID, repoID string) {
	t.Helper()
	var entities func(string, string) []string
	if withRoot {
		entities = func(_, repoID string) []string {
			return []string{fmt.Sprintf(`INSERT INTO content_entities
			 (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, metadata, indexed_at)
			 VALUES ('%[1]s:n0','%[1]s','main.go','Function','main',1,3,'go','', '{"dead_code_root_kinds":["go.main_function"]}'::jsonb, now())`, repoID)}
		}
	}
	scopeID, repoID = seedUpgradeBackfillRepo(t, ctx, db, suffix, time.Hour, entities)
	for _, q := range []string{
		`INSERT INTO shared_projection_intents
		  (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id, repository_id,
		   source_run_id, generation_id, payload, created_at, completed_at)
		 SELECT 'intent-' || $1::text || '-e' || g, 'code_calls', $2::text, $3::text, $2::text, $2::text, 'run-' || $1::text, 'gen-' || $1::text,
		        jsonb_build_object('caller_entity_id', $2::text || ':n' || g, 'callee_entity_id', $2::text || ':n' || (g + 1),
		                           'resolution_method', 'scip', 'relationship_type', 'CALLS'),
		        base.completed_at, base.completed_at
		 FROM generate_series(0, $4::int - 1) AS g,
		      (SELECT completed_at FROM shared_projection_intents WHERE intent_id = 'intent-' || $1::text) AS base`,
	} {
		if _, err := db.ExecContext(ctx, q, suffix, repoID, scopeID, hops); err != nil {
			t.Fatalf("seed chain: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE code_reachability_repository_watermarks SET verdict_schema_epoch = 3
		WHERE scope_id = $1 AND repository_id = $2`, scopeID, repoID); err != nil {
		t.Fatalf("stamp epoch 3: %v", err)
	}
	return scopeID, repoID
}

func truncationWatermark(t *testing.T, ctx context.Context, db *sql.DB, scopeID, repoID string) (truncated bool, epoch int) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `SELECT truncated, verdict_schema_epoch FROM code_reachability_repository_watermarks
		WHERE scope_id = $1 AND repository_id = $2`, scopeID, repoID).Scan(&truncated, &epoch); err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	return truncated, epoch
}

// truncationReasonByRepo parses the runner's JSON warnings into repo -> reason.
func truncationReasonByRepo(t *testing.T, logs *bytes.Buffer, repoIDs ...string) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, line := range strings.Split(logs.String(), "\n") {
		var rec map[string]any
		if line == "" || json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		for _, repoID := range repoIDs {
			if reason, ok := rec["truncation_reason"].(string); ok && strings.Contains(line, `"`+repoID+`"`) {
				got[repoID] = reason
			}
		}
	}
	return got
}

// TestCodeReachabilityTruncationEpochBumpRestampsWatermarks is the #7547 fixture-scale
// proof that the epoch 3 -> 4 bump re-stamps stale truncated=false watermarks
// for unchanged inputs: a zero-root repo, an 11-hop chain, and a rooted complete
// repo are all reloaded once, the first two become truncated, the complete one
// stays untruncated with identical reachability rows, and a second pass loads
// none of them. Reverting the epoch bump turns the loader assertion RED.
func TestCodeReachabilityTruncationEpochBumpRestampsWatermarks(t *testing.T) {
	ctx, db := openUpgradeBackfillLiveDB(t)
	store := reachabilitystore.NewCodeReachabilityStore(SQLDB{DB: db})
	base := testSuffix(t)
	noRootScope, noRootRepo := seedTruncationEpochRepo(t, ctx, db, base+"-a", 2, false)
	deepScope, deepRepo := seedTruncationEpochRepo(t, ctx, db, base+"-b", 11, true)
	okScope, okRepo := seedTruncationEpochRepo(t, ctx, db, base+"-c", 2, true)

	loaded := loadedRepoIDs(t, ctx, store)
	for _, repoID := range []string{noRootRepo, deepRepo, okRepo} {
		if !loaded[repoID] {
			t.Fatalf("epoch-3 watermark for %q was not re-scheduled at epoch %d", repoID, reachabilitystore.CodeReachabilityVerdictSchemaEpoch)
		}
	}
	const rowsSQL = `SELECT root_entity_id, entity_id, depth, state, confidence, min_resolution_method, evidence, root_kinds
		FROM code_reachability_rows WHERE repository_id = $1`
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS code_reachability_rows_before_7547 AS SELECT * FROM code_reachability_rows WHERE false`); err != nil {
		t.Fatalf("create before table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS code_reachability_rows_before_7547`)
	})
	// Rows for the complete repo as the pre-bump logic left them: project it once
	// (rows are unchanged by #7547), snapshot them, then restore the epoch-3 stamp.
	var logs bytes.Buffer
	runner := upgradeBackfillRunner(store)
	runner.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	if _, err := runner.ProcessOnce(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO code_reachability_rows_before_7547 SELECT * FROM code_reachability_rows WHERE repository_id = $1`, okRepo); err != nil {
		t.Fatalf("snapshot rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE code_reachability_repository_watermarks SET verdict_schema_epoch = 3, truncated = false
		WHERE scope_id = $1 AND repository_id = $2`, okScope, okRepo); err != nil {
		t.Fatalf("restore epoch 3: %v", err)
	}
	if _, err := runner.ProcessOnce(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("second ProcessOnce: %v", err)
	}

	for _, want := range []struct {
		scope, repo string
		truncated   bool
		reason      string
	}{{noRootScope, noRootRepo, true, "no_roots"}, {deepScope, deepRepo, true, "max_depth"}, {okScope, okRepo, false, ""}} {
		truncated, epoch := truncationWatermark(t, ctx, db, want.scope, want.repo)
		if truncated != want.truncated || epoch != reachabilitystore.CodeReachabilityVerdictSchemaEpoch || epoch < 4 {
			t.Fatalf("%s watermark = truncated %v epoch %d, want truncated %v at epoch >= 4", want.repo, truncated, epoch, want.truncated)
		}
		if got := truncationReasonByRepo(t, &logs, want.repo)[want.repo]; got != want.reason {
			t.Fatalf("%s truncation_reason = %q, want %q", want.repo, got, want.reason)
		}
	}
	for _, diff := range []string{
		`SELECT count(*) FROM (SELECT root_entity_id, entity_id, depth, state, confidence, min_resolution_method, evidence, root_kinds FROM code_reachability_rows_before_7547
		  EXCEPT ALL ` + rowsSQL + `) d`,
		`SELECT count(*) FROM (` + rowsSQL + ` EXCEPT ALL SELECT root_entity_id, entity_id, depth, state, confidence, min_resolution_method, evidence, root_kinds FROM code_reachability_rows_before_7547) d`,
	} {
		var n int
		if err := db.QueryRowContext(ctx, diff, okRepo).Scan(&n); err != nil || n != 0 {
			t.Fatalf("complete repo rows changed across re-projection: diff=%d err=%v", n, err)
		}
	}
	loaded = loadedRepoIDs(t, ctx, store)
	for _, repoID := range []string{noRootRepo, deepRepo, okRepo} {
		if loaded[repoID] {
			t.Fatalf("repo %q re-scheduled after it was stamped at the current epoch (anti-loop)", repoID)
		}
	}
}
