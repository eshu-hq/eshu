// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_postgres_language_zero_match_plan

// The plan-shape guard for the defect #6540 fixes: a language/entity-type
// filter that matches NO rows must not walk content_entities_path_idx to the
// end before answering.
//
// Why this file exists beside language_query_grant_plan_shape_live_test.go:
// that test seeds every non-yaml language as `Function`, so its
// (language, entity_type) pairs all match something. The empty pair is the
// case this change re-measured at 2,590 ms and 2,013,451 buffers for an empty
// answer, and no test covered it. (Issue #6540 reported 504 ms / 239,920
// buffers on a seed whose relative_path was correlated with physical order;
// this seed md5-scatters it, so the same ordered walk pays one random heap
// fetch per row. Same defect, more pessimistic seed.)
//
// What this pins is the SHAPE the measurement depends on, not the numbers,
// which move with the machine:
//
//   - a filter matching nothing reaches an Index Cond on (language,
//     entity_type), so the read is a btree descent rather than an ordered
//     walk; and
//   - the statement's ORDER BY is served by the index, so no Sort node sits
//     above the scan.
//
// The second assertion is the one that would catch a truncated index.
// This change measured a plain (language, entity_type) index -- #6540 listed it
// as a theory to test, it did not test it -- and the planner did NOT take it:
// the plan stayed an ordered walk, because a two-column index
// does not serve ORDER BY relative_path, start_line, entity_name. An index
// that keeps the name but loses the trailing sort columns therefore
// reintroduces the defect while still satisfying an Index-Cond-only check.
//
// The statement under test is CAPTURED from the production path, not written
// here: the recording driver records exactly what
// SearchEntitiesByLanguageAndTypeForAccess sent, and that text is what gets
// EXPLAINed, so a builder change changes what this test measures instead of
// leaving it measuring a stale copy.
//
// CI compiles this tag but never executes it. `verify-tagged-builds.sh --all`
// discovers every directory under `go/` holding a `//go:build` file and runs
// one `go vet` per distinct constraint, and the static-contract workflow runs
// that sweep on any `go/**` change -- so a compile break here is caught, and a
// behavioral break is not. Nothing in CI has a PostgreSQL to point this at.
// Run it yourself against a disposable PostgreSQL 16:
//
//	ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN=... \
//	ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=yes \
//	go test ./internal/query -tags live_postgres_language_zero_match_plan \
//	  -run TestLivePostgresLanguageQueryZeroMatchPlanShape -count=1
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
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	// Enough rows that an ordered walk is expensive enough for the planner to
	// have a real choice, and small enough to seed in seconds. The defect was
	// measured at 2,000,000 rows by this change; the shapes asserted here are
	// what that plan reduces to at this size, not a claim measured above it.
	zeroMatchSeedRows  = 300000
	zeroMatchSeedRepos = 600
)

// TestLivePostgresLanguageQueryZeroMatchPlanShape is the plan-shape half of the
// #6540 evidence. See the file header for what it pins and why the statement is
// captured rather than written.
func TestLivePostgresLanguageQueryZeroMatchPlanShape(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		5*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedZeroMatchCorpus(ctx, t, db)

	// hcl carries no Function rows in the seed, for the same reason it carries
	// none in a real corpus: HCL has no function declarations. A positive
	// control guards the premise, because a filter that quietly started
	// matching rows would make the assertions below pass for the wrong reason.
	assertZeroMatchPremise(ctx, t, db)

	statement, args := captureShippedZeroMatchStatement(ctx, t)
	if !strings.Contains(statement, "language = $") {
		t.Fatalf("captured statement carries no language predicate, so this test is not judging the language filter:\n%s", statement)
	}
	plan := explainZeroMatchPlanJSON(ctx, t, db, statement, args)
	t.Logf("zero-match plan:\n%s", plan)

	if strings.Contains(plan, `"Node Type": "Seq Scan"`) {
		t.Fatalf("the zero-match read falls back to a Seq Scan over content_entities:\n%s", plan)
	}
	if !planReachesIndexCond(plan, "language") {
		t.Fatalf("the zero-match filter does not reach an Index Cond on language, so it is filtering rows an ordered scan already read -- this is the #6540 defect (2,590 ms / 2,013,451 buffers for an empty answer):\n%s", plan)
	}
	// A Sort above the scan means the index no longer carries the ORDER BY
	// key, which is exactly the truncated-index regression described above.
	if strings.Contains(plan, `"Node Type": "Sort"`) || strings.Contains(plan, `"Node Type": "Incremental Sort"`) {
		t.Fatalf("a Sort node sits above the scan, so the index no longer serves ORDER BY relative_path, start_line, entity_name; a (language, entity_type) prefix alone is not taken by the planner under ORDER BY ... LIMIT (#6540):\n%s", plan)
	}
}

// assertZeroMatchPremise fails if the seed's supposedly-empty pair matches
// rows. Without it, a seed change could make every assertion below vacuous.
func assertZeroMatchPremise(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	var zeroMatch, hclRows, functionRows int
	if err := db.QueryRowContext(ctx, `
		SELECT
		    count(*) FILTER (WHERE language = 'hcl' AND entity_type = 'Function'),
		    count(*) FILTER (WHERE language = 'hcl'),
		    count(*) FILTER (WHERE entity_type = 'Function')
		FROM content_entities
	`).Scan(&zeroMatch, &hclRows, &functionRows); err != nil {
		t.Fatalf("count the zero-match premise: %v", err)
	}
	if zeroMatch != 0 {
		t.Fatalf("seed has %d (hcl, Function) rows, want 0; this test measures the EMPTY filter", zeroMatch)
	}
	// Both halves must be populated, or the planner is choosing between
	// predicates that are individually empty and the test proves nothing.
	if hclRows == 0 || functionRows == 0 {
		t.Fatalf("seed has %d hcl rows and %d Function rows, want both non-zero; the zero match must come from the COMBINATION, not from an absent language or type",
			hclRows, functionRows)
	}
}

// captureShippedZeroMatchStatement runs the production content read against a
// recording driver and returns the statement it actually sent.
func captureShippedZeroMatchStatement(ctx context.Context, t *testing.T) (string, []any) {
	t.Helper()

	recordingDB, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{
			"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
			"start_line", "end_line", "language", "source_cache", "metadata",
		},
	}})
	reader := NewContentReader(recordingDB)
	if _, err := reader.SearchEntitiesByLanguageAndTypeForAccess(ctx, languageEntitySearch{
		Language:   "hcl",
		EntityType: "Function",
		Limit:      50,
	}); err != nil {
		t.Fatalf("SearchEntitiesByLanguageAndTypeForAccess(): %v", err)
	}
	for i, query := range recorder.queries {
		if !strings.Contains(query, "FROM content_entities") {
			continue
		}
		// The recorder stores driver.Value; a *sql.DB call takes []any. This
		// read binds no grant array, so every argument is a plain scalar and
		// needs no re-wrapping -- unlike the grant-plan test, which has to
		// rebuild its text[] through pgarray.Array.
		args := make([]any, 0, len(recorder.args[i]))
		for _, recorded := range recorder.args[i] {
			args = append(args, recorded)
		}
		return query, args
	}
	t.Fatalf("the production read issued no content_entities query: %#v", recorder.queries)
	return "", nil
}

// seedZeroMatchCorpus writes a corpus whose (language, entity_type) pairs are
// realistic: hcl carries Resource/Variable/Module rows and no Function rows,
// while go and python carry Function rows. relative_path is scattered so an
// ordered walk of content_entities_path_idx is a random heap traversal, which
// is what makes the defect expensive.
func seedZeroMatchCorpus(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO content_entities (
		    entity_id, repo_id, relative_path, entity_type, entity_name,
		    start_line, end_line, language, source_cache, metadata, indexed_at
		)
		SELECT
		    'ent_' || i,
		    'repo_' || lpad((((i - 1) % $2) + 1)::text, 4, '0'),
		    'src/' || substr(md5(i::text), 1, 2) || '/' || substr(md5((i * 7)::text), 1, 10) || '.src',
		    f.entity_type,
		    'sym' || (i % 25000),
		    1 + (i % 900),
		    12 + (i % 900),
		    f.lang,
		    'body ' || md5(i::text),
		    '{}'::jsonb,
		    now()
		FROM generate_series(1, $1) AS i
		CROSS JOIN LATERAL (
		    SELECT
		        CASE WHEN i % 5 = 0 THEN 'hcl' WHEN i % 5 = 1 THEN 'python' ELSE 'go' END AS lang,
		        CASE
		            WHEN i % 5 = 0 THEN (ARRAY['Resource','Variable','Module','Output'])[1 + (i % 4)]
		            ELSE (ARRAY['Function','Method','Struct','Variable'])[1 + (i % 4)]
		        END AS entity_type
		) AS f
	`, zeroMatchSeedRows, zeroMatchSeedRepos); err != nil {
		t.Fatalf("seed content_entities: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE content_entities"); err != nil {
		t.Fatalf("ANALYZE content_entities: %v", err)
	}
}

// explainZeroMatchPlanJSON returns the EXPLAIN plan for statement as JSON text.
// ANALYZE is deliberately absent: this asserts the plan the planner CHOOSES,
// and running it would add timings that make the guard flaky without making it
// stricter.
//
// It duplicates the grant-plan file's helper rather than sharing one, because
// that file sits behind a different build tag and neither is compiled when the
// other's tag is selected.
func explainZeroMatchPlanJSON(ctx context.Context, t *testing.T, db *sql.DB, statement string, args []any) string {
	t.Helper()

	var plan string
	row := db.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON, BUFFERS false) "+statement, args...)
	if err := row.Scan(&plan); err != nil {
		t.Fatalf("EXPLAIN the shipped statement: %v\nstatement:\n%s", err, statement)
	}
	return plan
}

// planReachesIndexCond reports whether column appears in an Index Cond (or a
// bitmap Recheck Cond) anywhere in the plan, meaning the predicate is narrowing
// the scan itself rather than filtering rows the scan already read.
func planReachesIndexCond(plan string, column string) bool {
	var nodes []map[string]any
	if err := json.Unmarshal([]byte(plan), &nodes); err != nil {
		return false
	}
	found := false
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if cond, ok := typed["Index Cond"].(string); ok && strings.Contains(cond, column) {
				found = true
			}
			if cond, ok := typed["Recheck Cond"].(string); ok && strings.Contains(cond, column) {
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
