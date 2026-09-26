// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// legacyReapStaleFingerprintBandSQL is the pre-#7230 band reap, kept verbatim
// as the canary that proves the plan probe below can fail. Under statistics
// that have not seen the repository it plans as a Nested Loop Anti Join whose
// inner content_entities_repo_idx scan re-runs once per band row.
const legacyReapStaleFingerprintBandSQL = `
DELETE FROM code_fingerprint_band band
WHERE band.repo_id = $1
  AND NOT EXISTS (
    SELECT 1 FROM content_entities ce
    WHERE ce.repo_id = $1 AND ce.entity_id = band.entity_id
  )
`

// fingerprintReapLiveDefinitions are the bootstrap definitions the reap
// touches: the content store with every content_entities index a planner
// could pick, the fingerprint side tables, and the infra read model Write
// maintains.
var fingerprintReapLiveDefinitions = map[string]bool{
	"content_store":                             true,
	"content_entities_repo_entity_idx":          true,
	"content_entities_k8s_select_partial_index": true,
	"content_entities_language_type_idx":        true,
	"content_entities_language_type_path_idx":   true,
	"code_function_fingerprint":                 true,
	"code_function_fingerprint_shingles":        true,
	"infra_resource_entities":                   true,
}

// openFingerprintReapLiveDB opens an isolated schema carrying the real
// definitions above. Statistics are per table, so this schema's ANALYZE state
// is private to the test; autovacuum is disabled on the three reap tables so
// an autoanalyze cannot refresh the stale state a test builds.
func openFingerprintReapLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #7230 fingerprint reap proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	schemaName := fmt.Sprintf("fp_reap_7230_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+array.QuoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(cleanupCtx, "DROP SCHEMA "+array.QuoteIdentifier(schemaName)+" CASCADE")
	})

	targetURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse ESHU_POSTGRES_TEST_DSN: %v", err)
	}
	params := targetURL.Query()
	params.Set("search_path", schemaName)
	targetURL.RawQuery = params.Encode()
	database, err := sql.Open("pgx", targetURL.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var definitions []Definition
	for _, definition := range BootstrapDefinitions() {
		if fingerprintReapLiveDefinitions[definition.Name] {
			definitions = append(definitions, definition)
		}
	}
	if len(definitions) != len(fingerprintReapLiveDefinitions) {
		t.Fatalf("isolated schema definitions = %d, want %d", len(definitions), len(fingerprintReapLiveDefinitions))
	}
	if err := ApplyDefinitions(ctx, SQLDB{DB: database}, definitions); err != nil {
		t.Fatalf("apply isolated Postgres schema: %v", err)
	}
	for _, table := range []string{"content_entities", "code_function_fingerprint", "code_fingerprint_band"} {
		mustExecLive(ctx, t, database, "ALTER TABLE "+table+" SET (autovacuum_enabled = false)")
	}
	return ctx, database
}

func mustExecLive(ctx context.Context, t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec %.80q: %v", query, err)
	}
}

// seedFingerprintReapRepo writes entities rows for repoID in the real
// content-entity id shape, fingerprints every ninth one, and gives each
// fingerprinted entity its 32 LSH band rows.
func seedFingerprintReapRepo(ctx context.Context, t *testing.T, database *sql.DB, repoID string, entities int) {
	t.Helper()
	mustExecLive(ctx, t, database, `
INSERT INTO content_entities (
    entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, language, source_cache, indexed_at
)
SELECT 'content-entity:e_' || substr(md5($1 || g), 1, 12), $1, 'src/f' || (g % 50) || '.go',
       CASE WHEN g % 9 = 0 THEN 'Function' ELSE 'Variable' END,
       'n' || g, g, g + 5, 'go', 'x', now()
FROM generate_series(1, $2::int) AS g`, repoID, entities)
	mustExecLive(ctx, t, database, `
INSERT INTO code_function_fingerprint (entity_id, repo_id, fp_exact, token_count, indexed_at)
SELECT entity_id, repo_id, md5(entity_id), 50, now()
FROM content_entities WHERE repo_id = $1 AND entity_type = 'Function'`, repoID)
	mustExecLive(ctx, t, database, `
INSERT INTO code_fingerprint_band (repo_id, band_no, band_hash, entity_id)
SELECT repo_id, b, substr(md5(entity_id || '-' || b), 1, 16), entity_id
FROM code_function_fingerprint CROSS JOIN generate_series(0, 31) AS b
WHERE repo_id = $1`, repoID)
}

func analyzeFingerprintReapTables(ctx context.Context, t *testing.T, database *sql.DB) {
	t.Helper()
	mustExecLive(ctx, t, database, "ANALYZE content_entities, code_function_fingerprint, code_fingerprint_band")
}

// reapPlanRecorder is a db.ExecQueryer that runs EXPLAIN (ANALYZE, FORMAT
// JSON) of every statement in a rolled-back transaction, records any plan
// node that ran more than once, then forwards the statement for real.
type reapPlanRecorder struct {
	database   *sql.DB
	statements int
	rescans    []string
}

var _ db.ExecQueryer = (*reapPlanRecorder)(nil)

func (r *reapPlanRecorder) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if err := r.record(ctx, query, args); err != nil {
		return nil, err
	}
	return r.database.QueryContext(ctx, query, args...)
}

func (r *reapPlanRecorder) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := r.record(ctx, query, args); err != nil {
		return nil, err
	}
	return r.database.ExecContext(ctx, query, args...)
}

func (r *reapPlanRecorder) record(ctx context.Context, query string, args []any) error {
	rescans, err := explainRescans(ctx, r.database, query, args)
	if err != nil {
		return err
	}
	r.statements++
	r.rescans = append(r.rescans, rescans...)
	return nil
}

// explainRescans returns every plan node of query whose Actual Loops exceeds
// one. The EXPLAIN ANALYZE runs inside a transaction that is always rolled
// back, so a DELETE is measured without its effect.
func explainRescans(ctx context.Context, database *sql.DB, query string, args []any) ([]string, error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin explain transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var raw string
	if err := tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		return nil, fmt.Errorf("explain %.60q: %w", query, err)
	}
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		return nil, fmt.Errorf("decode explain json: %w", err)
	}
	var rescans []string
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		if loops, _ := node["Actual Loops"].(float64); loops > 1 {
			relation, _ := node["Relation Name"].(string)
			rescans = append(rescans, fmt.Sprintf("%v on %q loops=%v in %.60q", node["Node Type"], relation, loops, strings.TrimSpace(query)))
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	for _, plan := range plans {
		walk(plan.Plan)
	}
	return rescans, nil
}

// TestContentWriterFingerprintReapScansOnceUnderStaleStatisticsLive pins
// #7230. Bootstrap grows content_entities and the fingerprint side tables from
// empty, so a repository is routinely reaped while the last ANALYZE saw at most
// 100 repositories and not this one. The planner then estimates one row on both
// sides, and the pre-#7230 NOT EXISTS anti-join ran as a nested loop that
// rescanned the repository's entities once per side-table row (230-1,735 s
// per repository on the reference host). Every statement the reap issues must now run each plan
// node exactly once under that stale state, including the deletes for entities
// that are really gone.
func TestContentWriterFingerprintReapScansOnceUnderStaleStatisticsLive(t *testing.T) {
	ctx, database := openFingerprintReapLiveDB(t)
	for i := range 10 {
		seedFingerprintReapRepo(ctx, t, database, fmt.Sprintf("repository:r_seen_%02d", i), 1000)
	}
	analyzeFingerprintReapTables(ctx, t, database)

	const repoID = "repository:r_unseen"
	seedFingerprintReapRepo(ctx, t, database, repoID, 2000)
	// Ten fingerprinted entities vanish, so the reap also runs its deletes.
	mustExecLive(ctx, t, database, `
DELETE FROM content_entities WHERE entity_id IN (
    SELECT entity_id FROM code_function_fingerprint WHERE repo_id = $1 ORDER BY entity_id LIMIT 10)`, repoID)

	// The proof is only meaningful when the planner really is blind to the
	// repository: one estimated row for thousands of real ones.
	var raw string
	if err := database.QueryRowContext(ctx,
		"EXPLAIN (FORMAT JSON) SELECT entity_id FROM content_entities WHERE repo_id = $1", repoID,
	).Scan(&raw); err != nil {
		t.Fatalf("explain the stale estimate: %v", err)
	}
	if !strings.Contains(raw, `"Plan Rows": 1,`) {
		t.Fatalf("precondition: want a one-row estimate for the unseen repository, got plan %s", raw)
	}
	canary, err := explainRescans(ctx, database, legacyReapStaleFingerprintBandSQL, []any{repoID})
	if err != nil {
		t.Fatalf("explain the legacy band reap: %v", err)
	}
	if len(canary) == 0 {
		t.Fatal("canary: the legacy anti-join no longer rescans under stale statistics, so this probe cannot fail")
	}
	t.Logf("legacy band reap under stale statistics: %s", canary[0])

	recorder := &reapPlanRecorder{database: database}
	writer := NewContentWriter(recorder)
	if _, err := writer.reapStaleFingerprints(ctx, repoID); err != nil {
		t.Fatalf("reapStaleFingerprints: %v", err)
	}
	if recorder.statements == 0 {
		t.Fatal("the reap issued no statements, so there is nothing to prove")
	}
	// Two stale-id reads plus one delete per side table: the deletes for the
	// vanished entities must be inside the proof, not skipped.
	if recorder.statements != 4 {
		t.Fatalf("reap statements = %d, want 4 (2 stale-id reads + 2 deletes)", recorder.statements)
	}
	if len(recorder.rescans) > 0 {
		t.Fatalf("reap plan nodes re-ran under stale statistics (%d):\n%s", len(recorder.rescans), strings.Join(recorder.rescans, "\n"))
	}
	assertNoStaleFingerprintRows(ctx, t, database, repoID)
}

// assertNoStaleFingerprintRows fails when any side-table row of repoID has no
// content_entities row in the same repository.
func assertNoStaleFingerprintRows(ctx context.Context, t *testing.T, database *sql.DB, repoID string) {
	t.Helper()
	fp, band := legacyStaleFingerprintRows(ctx, t, database, repoID)
	if len(fp) != 0 || len(band) != 0 {
		t.Fatalf("stale side rows survived the reap: %d fp, %d band", len(fp), len(band))
	}
}

// Row keys shared by the oracle and the whole-table snapshots, so a repo's
// expected survivors are a plain set difference.
const (
	fingerprintRowKeySQL = `fp.repo_id || '|' || fp.entity_id`
	bandRowKeySQL        = `band.repo_id || '|' || band.entity_id || '/' || band.band_no || '/' || band.band_hash`
)

// legacyStaleFingerprintRows returns, as sorted keys, the rows the pre-#7230
// anti-join reaps would delete for repoID: the oracle for equivalence. The
// predicates are the former reapStaleFingerprintSQL and
// reapStaleFingerprintBandSQL verbatim.
func legacyStaleFingerprintRows(ctx context.Context, t *testing.T, database *sql.DB, repoID string) ([]string, []string) {
	t.Helper()
	fp := queryLiveKeys(ctx, t, database, `
SELECT `+fingerprintRowKeySQL+` FROM code_function_fingerprint fp
WHERE fp.repo_id = $1
  AND NOT EXISTS (
    SELECT 1 FROM content_entities ce
    WHERE ce.repo_id = $1 AND ce.entity_id = fp.entity_id
  )`, repoID)
	band := queryLiveKeys(ctx, t, database, `
SELECT `+bandRowKeySQL+` FROM code_fingerprint_band band
WHERE band.repo_id = $1
  AND NOT EXISTS (
    SELECT 1 FROM content_entities ce
    WHERE ce.repo_id = $1 AND ce.entity_id = band.entity_id
  )`, repoID)
	return fp, band
}

func queryLiveKeys(ctx context.Context, t *testing.T, database *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query %.60q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan key: %v", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate keys: %v", err)
	}
	sort.Strings(keys)
	return keys
}
