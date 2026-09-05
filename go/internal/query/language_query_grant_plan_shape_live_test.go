// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_postgres_language_grant_plan

// The plan-shape guard for the one predicate #5167 batch 2a adds to a paged
// Postgres read: repo_id = ANY($n) on content_entities, emitted by
// buildLanguageTypeEntityFilters for a scoped caller who named no repository.
//
// The measured timings live in
// docs/internal/evidence/5167-code-family-batch-2-proofs.md. Numbers are not
// what this pins -- they move with the machine. What it pins is the SHAPE the
// numbers depend on, which is what a schema, statistics or builder change
// would silently break:
//
//   - a narrow grant reaches an Index Cond, so the read is bounded by the
//     grant rather than by how far an ordered walk has to travel; and
//   - no grant width turns the read into a Seq Scan over content_entities.
//
// The statement under test is not written here. It is CAPTURED from the
// production path: the recording driver records exactly what
// SearchEntitiesByLanguageAndTypeForAccess sent, and that text is what gets
// EXPLAINed. A builder change therefore changes what this test measures
// instead of leaving it measuring a stale copy.
//
// No CI job builds this tag. Run it against a disposable PostgreSQL 16:
//
//	ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN=... \
//	ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=yes \
//	go test ./internal/query -tags live_postgres_language_grant_plan \
//	  -run TestLivePostgresLanguageQueryGrantPlanShape -count=1
package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	// Enough rows, spread over enough repositories, that a sequential scan is
	// not trivially the cheapest plan. The proofs note measured 2,000,000; the
	// shapes asserted here hold from this size up, and this seeds in seconds.
	grantPlanSeedRows  = 300000
	grantPlanSeedRepos = 600
)

// TestLivePostgresLanguageQueryGrantPlanShape is the plan-shape half of the
// #5167 batch 2a Postgres evidence. See the file header for what it pins and
// why the statement is captured rather than written.
func TestLivePostgresLanguageQueryGrantPlanShape(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		5*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedGrantPlanCorpus(ctx, t, db)

	for _, tc := range []struct {
		name          string
		grant         int
		wantIndexCond bool
	}{
		// A narrow grant is the case the proofs note measures as the most
		// expensive, and the Index Cond is why it stays bounded at all: the
		// read is limited to the granted repositories' rows instead of walking
		// content_entities_path_idx until the LIMIT fills.
		{name: "narrow grant is bounded by an index condition", grant: 1, wantIndexCond: true},
		// A wide grant filters out almost nothing. It must still not degrade
		// into a table scan.
		{name: "wide grant does not become a table scan", grant: grantPlanSeedRepos, wantIndexCond: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grant := grantPlanRepoIDs(ctx, t, db, tc.grant)
			statement, args := captureShippedGrantStatement(ctx, t, grant)
			if !strings.Contains(statement, "repo_id = ANY(") {
				t.Fatalf("captured statement carries no grant predicate, so this test is not judging the grant:\n%s", statement)
			}
			plan := explainJSON(ctx, t, db, statement, args)
			t.Logf("grant=%d plan:\n%s", tc.grant, plan)

			if strings.Contains(plan, `"Node Type": "Seq Scan"`) {
				t.Fatalf("grant=%d reads content_entities with a Seq Scan; the grant predicate must stay index-backed at every width:\n%s", tc.grant, plan)
			}
			gotIndexCond := grantReachesIndexCond(plan)
			if tc.wantIndexCond && !gotIndexCond {
				t.Fatalf("grant=%d does not reach an Index Cond on repo_id, so a narrow grant no longer bounds the read; re-measure before trusting the proofs note:\n%s", tc.grant, plan)
			}
		})
	}
}

// captureShippedGrantStatement runs the production content read against a
// recording driver and returns the statement it actually sent, with the grant
// argument re-wrapped through pgarray.Array so a real backend can bind it.
// Everything else is the recorder's own value.
func captureShippedGrantStatement(ctx context.Context, t *testing.T, grant []string) (string, []any) {
	t.Helper()

	recordingDB, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{
			"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
			"start_line", "end_line", "language", "source_cache", "metadata",
		},
	}})
	reader := NewContentReader(recordingDB)
	if _, err := reader.SearchEntitiesByLanguageAndTypeForAccess(ctx, languageEntitySearch{
		Language:             "go",
		EntityType:           "Function",
		Limit:                50,
		AllowedRepositoryIDs: grant,
	}); err != nil {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess(): %v", err)
	}

	for i, query := range recorder.queries {
		if !strings.Contains(query, "FROM content_entities") {
			continue
		}
		args := make([]any, 0, len(recorder.args[i]))
		for _, recorded := range recorder.args[i] {
			// The grant reaches the driver already serialized by
			// pgarray.Array's Value(). Re-wrap that one argument; a backend
			// binds text[] from the wrapper, not from its rendering.
			if text, ok := recorded.(string); ok && strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") {
				args = append(args, pgarray.Array(grant))
				continue
			}
			args = append(args, recorded)
		}
		return query, args
	}
	t.Fatalf("the production read issued no content_entities query: %#v", recorder.queries)
	return "", nil
}

// explainJSON returns the EXPLAIN plan for statement as JSON text. ANALYZE is
// deliberately absent: this asserts the plan the planner CHOOSES, and running
// it would add timings that make the guard flaky without making it stricter.
func explainJSON(ctx context.Context, t *testing.T, db *sql.DB, statement string, args []any) string {
	t.Helper()

	var plan string
	row := db.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON, BUFFERS false) "+statement, args...)
	if err := row.Scan(&plan); err != nil {
		t.Fatalf("EXPLAIN the shipped statement: %v\nstatement:\n%s", err, statement)
	}
	return plan
}

// grantReachesIndexCond reports whether repo_id appears in an Index Cond
// anywhere in the plan, meaning the grant is narrowing the scan itself rather
// than filtering rows the scan already read.
func grantReachesIndexCond(plan string) bool {
	var nodes []map[string]any
	if err := json.Unmarshal([]byte(plan), &nodes); err != nil {
		return false
	}
	found := false
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if cond, ok := typed["Index Cond"].(string); ok && strings.Contains(cond, "repo_id") {
				found = true
			}
			if cond, ok := typed["Recheck Cond"].(string); ok && strings.Contains(cond, "repo_id") {
				found = true
			}
			for _, nested := range typed {
				walk(nested)
			}
		case []any:
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	return found
}

// grantPlanRepoIDs returns the n SMALLEST seeded repositories, by row count.
//
// Smallest, not first: repository sizes are skewed, and which plan the narrow
// case gets depends on how many rows the grant can match, not on how many
// repositories it names. A grant naming the one monorepo matches enough rows
// that the planner keeps the ordered walk and filters -- the proofs note
// measures exactly that at 0.39 ms -- while a grant naming a small repository
// is the case that reaches an index condition. Asking the data avoids pinning
// whichever one an id happened to land on.
func grantPlanRepoIDs(ctx context.Context, t *testing.T, db *sql.DB, n int) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
		SELECT repo_id
		FROM content_entities
		GROUP BY repo_id
		ORDER BY count(*) ASC, repo_id ASC
		LIMIT $1
	`, n)
	if err != nil {
		t.Fatalf("select the %d smallest seeded repositories: %v", n, err)
	}
	defer func() { _ = rows.Close() }()

	ids := make([]string, 0, n)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan seeded repository id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate seeded repository ids: %v", err)
	}
	if len(ids) != n {
		t.Fatalf("seeded corpus has %d repositories, want at least %d", len(ids), n)
	}
	return ids
}

// seedGrantPlanCorpus writes a content_entities corpus shaped like the one the
// proofs note measures: entities belong to files, a file belongs to one
// repository and one language, repository sizes are skewed, and entity_type is
// correlated with language. ANALYZE runs before any plan is read.
func seedGrantPlanCorpus(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO content_entities (
		    entity_id, repo_id, relative_path, entity_type, entity_name,
		    start_line, end_line, language, source_cache, metadata, indexed_at
		)
		SELECT
		    'ent_' || i,
		    'repo_' || lpad((floor($2 * power(((((i - 1) / 12)::bigint * 2654435761) % 1000003)::numeric / 1000003, 2.2))::int)::text, 4, '0'),
		    'src/unit_' || ((i - 1) / 12) || '.src',
		    CASE WHEN f.lang = 'yaml' THEN 'K8sResource' ELSE 'Function' END,
		    'handle' || (i % 25000),
		    1 + (i % 900),
		    12 + (i % 900),
		    f.lang,
		    'func x() { // ' || md5(i::text) || ' }',
		    '{"kind":"decl"}'::jsonb,
		    now()
		FROM generate_series(1, $1) AS i
		CROSS JOIN LATERAL (
		    SELECT (ARRAY['go','typescript','python','java','yaml','hcl'])[
		        1 + ((((i - 1) / 12)::bigint * 48271) % 6)::int] AS lang
		) AS f
	`, grantPlanSeedRows, grantPlanSeedRepos); err != nil {
		t.Fatalf("seed content_entities: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE content_entities"); err != nil {
		t.Fatalf("ANALYZE content_entities: %v", err)
	}
}
