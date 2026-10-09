// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// The proofs in this file run against a disposable PostgreSQL with pg_trgm
// (ESHU_CONTENT_SEARCH_PROOF_DSN and ESHU_CONTENT_SEARCH_PROOF_DISPOSABLE=1)
// and skip without it. The fixture is synthetic: 12,000 files in 60
// repositories whose content is hex words, with a few planted tokens. It has
// the shape that matters to the walk (a primary-key order, a trigram GIN
// index, a dense token, a medium token, a selective literal, a zero-match
// token and a token with no extractable trigram) and none of any real corpus.

const (
	liveFixtureRows   = 12000
	liveDenseToken    = "handler"
	liveMediumToken   = "widget"
	liveRareToken     = "zq_rare_literal_k"
	liveNoTrigramHits = "zz"
	// liveEdgeToken sits on the rows a window statement ends on and on the row
	// after each, so a duplicated or skipped boundary row changes the answer.
	// Window edges fall on g = 200 + 500k when the walk starts at the first row
	// (200-row probe, then 500-row steps), and the tail resumes after one.
	liveEdgeToken = "edgemark"
)

func openUnscopedLiveFixture(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_CONTENT_SEARCH_PROOF_DSN"),
		os.Getenv("ESHU_CONTENT_SEARCH_PROOF_DISPOSABLE"),
		3*time.Minute,
	)
	postgresproof.InstallTrigramExtension(ctx, t, db)
	statements := []string{
		`CREATE TABLE content_files (
			repo_id text NOT NULL,
			relative_path text NOT NULL,
			commit_sha text,
			content text NOT NULL,
			content_hash text NOT NULL,
			line_count integer NOT NULL,
			language text,
			artifact_type text,
			PRIMARY KEY (repo_id, relative_path)
		)`,
		`CREATE FUNCTION eshu_require_content_substring_indexes_ready() RETURNS boolean
			LANGUAGE sql STABLE AS 'SELECT true'`,
		fmt.Sprintf(`INSERT INTO content_files
			(repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
			SELECT 'repo-' || lpad((g / 200)::text, 3, '0'),
			       'src/file-' || lpad(g::text, 6, '0') || '.go',
			       'sha',
			       (SELECT string_agg(md5(g::text || ':' || k::text), ' ') FROM generate_series(1, CASE WHEN g %% 20 = 0 THEN 800 WHEN g %% 2 = 1 THEN 5 ELSE 50 END) k)
			         || CASE WHEN g %% 4 = 0 THEN ' %[2]s' ELSE '' END
			         || CASE WHEN g %% 50 = 7 THEN ' %[3]s' ELSE '' END
			         || CASE WHEN g IN (9001, 11000, 11777) THEN ' %[4]s' ELSE '' END
			         || CASE WHEN g IN (3, 10001, 11999) OR g %% 500 IN (200, 201) THEN ' %[5]s' ELSE '' END
			         || CASE WHEN g %% 500 IN (200, 201) THEN ' %[6]s' ELSE '' END,
			       md5(g::text), 1, 'go', 'source'
			FROM generate_series(1, %[1]d) g`, liveFixtureRows, liveDenseToken, liveMediumToken, liveRareToken, liveNoTrigramHits, liveEdgeToken),
		`CREATE INDEX content_files_content_trgm_idx ON content_files USING gin (content gin_trgm_ops)`,
		`VACUUM ANALYZE content_files`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed unscoped search fixture: %v\n%s", err, statement)
		}
	}
	return ctx, db
}

// oracleOffsetSQL derives the old single-statement page query from the
// shipped tail statement: the same WHERE and ORDER BY, with the old OFFSET.
// The hermetic TestOracleStatementIsDerivedFromShippedTailText pins the
// derivation; no copy of the statement exists in the tests.
func oracleOffsetSQL(t *testing.T) string {
	t.Helper()
	shipped := unscoped.Statements().TailFirst
	const marker = "LIMIT $2::bigint"
	if !strings.HasSuffix(strings.TrimSpace(shipped), marker) {
		t.Fatalf("shipped tail statement no longer ends with %q: %s", marker, shipped)
	}
	return strings.TrimSpace(shipped) + " OFFSET $3::bigint"
}

func oracleKeys(t *testing.T, ctx context.Context, db *sql.DB, pattern string, limit, offset int) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, oracleOffsetSQL(t), pattern, limit, offset)
	if err != nil {
		t.Fatalf("oracle query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var kind, repo, path, sha, content, hash, language, artifact string
		var lines int
		if err := rows.Scan(&kind, &repo, &path, &sha, &content, &hash, &lines, &language, &artifact); err != nil {
			t.Fatalf("scan oracle: %v", err)
		}
		keys = append(keys, repo+"/"+path)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("oracle rows: %v", err)
	}
	return keys
}

func pageKeys(files []querycontract.FileContent) []string {
	keys := make([]string, 0, len(files))
	for _, f := range files {
		keys = append(keys, f.RepoID+"/"+f.RelativePath)
	}
	return keys
}

// TestSearchFilesUnscopedMatchesOldStatementLive proves the walk returns the
// same ordered pages as the old single statement for every class that is exact
// inside the budget, including ILIKE wildcards, and that its More flag agrees
// with the old look-ahead row.
func TestSearchFilesUnscopedMatchesOldStatementLive(t *testing.T) {
	ctx, db := openUnscopedLiveFixture(t)
	reader := NewContentReader(db).WithUnscopedSearch(10*time.Second, nil)

	classes := map[string]string{
		"dense":      liveDenseToken,
		"medium":     liveMediumToken,
		"selective":  liveRareToken,
		"zero match": "absent-token-xyz",
		"wildcard":   "han_ler",
		"upper case": strings.ToUpper(liveMediumToken),
	}
	for name, pattern := range classes {
		for _, page := range []struct{ limit, offset int }{{10, 0}, {10, 10}, {10, 20}, {50, 0}} {
			oldStart := time.Now()
			want := oracleKeys(t, ctx, db, pattern, page.limit+1, page.offset)
			oldWall := time.Since(oldStart)
			newStart := time.Now()
			got, err := reader.SearchFilesUnscoped(ctx, pattern, page.limit, page.offset, "", "")
			newWall := time.Since(newStart)
			if page.offset == 0 && page.limit == 10 {
				t.Logf("timing class=%q old=%.1fms new=%.1fms", name, float64(oldWall.Microseconds())/1000, float64(newWall.Microseconds())/1000)
			}
			if err != nil {
				t.Fatalf("%s %+v: SearchFilesUnscoped() error = %v", name, page, err)
			}
			if got.Partial != nil {
				t.Fatalf("%s %+v: partial inside a 10 s budget: %+v", name, page, got.Partial)
			}
			wantPage := want
			if len(wantPage) > page.limit {
				wantPage = wantPage[:page.limit]
			}
			gotKeys := pageKeys(got.Files)
			if strings.Join(gotKeys, ",") != strings.Join(wantPage, ",") {
				t.Fatalf("%s %+v: walk page %v != old page %v", name, page, gotKeys, wantPage)
			}
			if got.More != (len(want) > page.limit) {
				t.Fatalf("%s %+v: More = %v, old statement saw %d rows for limit %d", name, page, got.More, len(want), page.limit)
			}
		}
	}
}

// TestSearchFilesUnscopedCancelledTailResumesToExactAnswerLive runs the real
// cancel path: a two-character token has no trigram, so the tail rechecks
// every row and cannot finish inside a 100 ms budget. The server cancels it,
// the savepoint keeps the transaction usable, and resuming from each returned
// cursor gathers exactly the old statement's rows with no gap and no
// duplicate.
func TestSearchFilesUnscopedCancelledTailResumesToExactAnswerLive(t *testing.T) {
	ctx, db := openUnscopedLiveFixture(t)
	reader := NewContentReader(db).WithUnscopedSearch(unscoped.MinBudget, nil)
	want := oracleKeys(t, ctx, db, liveNoTrigramHits, 1000, 0)
	if len(want) < 3 {
		t.Fatalf("fixture has %d matches for the no-trigram token, want at least 3", len(want))
	}

	var gathered []string
	cursor := querycontract.SearchCursor{}
	offset := 0
	partials := 0
	for call := 0; call < 200; call++ {
		page, err := reader.SearchFilesUnscoped(ctx, liveNoTrigramHits, 100, offset, cursor.RepoID, cursor.RelativePath)
		if err != nil {
			t.Fatalf("call %d: %v", call, err)
		}
		gathered = append(gathered, pageKeys(page.Files)...)
		if page.Partial == nil {
			if strings.Join(gathered, ",") != strings.Join(want, ",") {
				t.Fatalf("gathered %v, want %v (after %d partial pages)", gathered, want, partials)
			}
			if partials == 0 {
				t.Fatal("no partial page: the tail finished inside the budget, so the cancel path was not exercised")
			}
			return
		}
		partials++
		if page.Partial.Cursor.IsZero() && partials > 1 {
			t.Fatalf("call %d: partial without a cursor after the first page: %+v", call, page.Partial)
		}
		cursor = page.Partial.Cursor
	}
	t.Fatalf("no convergence in 200 calls; gathered %v", gathered)
}

type explainNode struct {
	NodeType  string        `json:"Node Type"`
	IndexName string        `json:"Index Name"`
	Plans     []explainNode `json:"Plans"`
}

func (n explainNode) walk(visit func(explainNode)) {
	visit(n)
	for _, child := range n.Plans {
		child.walk(visit)
	}
}

func explainShipped(t *testing.T, ctx context.Context, db *sql.DB, indexScanOff bool, statement string, args ...any) explainNode {
	t.Helper()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if indexScanOff {
		if _, err := tx.ExecContext(ctx, unscoped.Statements().DisableIndexScan); err != nil {
			t.Fatalf("disable index scan: %v", err)
		}
	}
	var raw string
	if err := tx.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+statement, args...).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plans []struct {
		Plan explainNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v; %s", err, raw)
	}
	return plans[0].Plan
}

// TestUnscopedSearchPlanShapesLive pins the plan contract the cost bound rests
// on: the key-ordered window never touches the trigram index, and the tail
// run with index scans disabled is the trigram bitmap with no primary-key walk
// and no sequential scan. The fixture proves the selective class; the
// measured corpus-scale proof of the other classes is in the evidence note.
func TestUnscopedSearchPlanShapesLive(t *testing.T) {
	ctx, db := openUnscopedLiveFixture(t)
	statements := unscoped.Statements()

	for name, c := range map[string]struct {
		statement string
		args      []any
	}{
		"first window": {statements.StepFirst, []any{liveRareToken, int64(200), int64(11)}},
		"next window":  {statements.StepAfter, []any{liveRareToken, int64(500), int64(11), "repo-010", "src/file-002000.go"}},
	} {
		plan := explainShipped(t, ctx, db, false, c.statement, c.args...)
		var nodes []string
		plan.walk(func(n explainNode) { nodes = append(nodes, n.NodeType+"/"+n.IndexName) })
		joined := strings.Join(nodes, " ")
		if strings.Contains(joined, "content_files_content_trgm_idx") || strings.Contains(joined, "Bitmap") {
			t.Errorf("%s plan touches the trigram index: %s", name, joined)
		}
		if !strings.Contains(joined, "content_files_pkey") {
			t.Errorf("%s plan does not walk the primary key: %s", name, joined)
		}
	}

	// The precondition that gives the setting teeth: left to the planner, the
	// medium token's old single statement is an ordered primary-key walk, the
	// plan whose cost depends on where the matches sit in key order.
	natural := explainShipped(t, ctx, db, false, statements.TailFirst, liveMediumToken, int64(11))
	if !planHas(natural, "Index Scan/content_files_pkey") {
		t.Fatalf("fixture lost the plan lottery: the planner no longer picks the primary-key walk for the medium token: %v", planNodes(natural))
	}

	for name, c := range map[string]struct {
		statement string
		args      []any
	}{
		"tail from the start, selective": {statements.TailFirst, []any{liveRareToken, int64(11)}},
		"tail from the start, medium":    {statements.TailFirst, []any{liveMediumToken, int64(11)}},
		"tail after a cursor, selective": {statements.TailAfter, []any{liveRareToken, int64(11), "repo-010", "src/file-002000.go"}},
		"tail after a cursor, medium":    {statements.TailAfter, []any{liveMediumToken, int64(11), "repo-010", "src/file-002000.go"}},
	} {
		plan := explainShipped(t, ctx, db, true, c.statement, c.args...)
		joined := strings.Join(planNodes(plan), " ")
		if !strings.Contains(joined, "Bitmap Index Scan/content_files_content_trgm_idx") {
			t.Errorf("%s plan is not the trigram bitmap: %s", name, joined)
		}
		for _, banned := range []string{"Seq Scan", "Index Scan/content_files_pkey"} {
			if strings.Contains(joined, banned) {
				t.Errorf("%s plan contains %s: %s", name, banned, joined)
			}
		}
	}
}

func planNodes(plan explainNode) []string {
	var nodes []string
	plan.walk(func(n explainNode) { nodes = append(nodes, n.NodeType+"/"+n.IndexName) })
	return nodes
}

func planHas(plan explainNode, want string) bool {
	for _, node := range planNodes(plan) {
		if node == want {
			return true
		}
	}
	return false
}

// TestSearchFilesUnscopedEdgeRowsAreExactLive is the SQL-level exactness proof
// for the keyset and the window edge: the edge token is planted on every row a
// window ends on and on the row after it, the budget is small enough that the
// probe, at least two continuation steps and then the trigram tail all run, and
// the answer must equal the old statement's rows exactly. An inclusive keyset
// operator duplicates a boundary row; an edge offset past the window skips
// one; either changes the list.
func TestSearchFilesUnscopedEdgeRowsAreExactLive(t *testing.T) {
	ctx, db := openUnscopedLiveFixture(t)
	spans := tracetest.NewSpanRecorder()
	searcher := &unscoped.Searcher{
		Store:  postgres.NewSQLReadStore(db),
		Budget: 200 * time.Millisecond,
		Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	want := oracleKeys(t, ctx, db, liveEdgeToken, 1000, 0)
	if len(want) < 40 {
		t.Fatalf("fixture has %d edge matches, want a boundary-rich set", len(want))
	}
	page, err := searcher.Search(ctx, liveEdgeToken, 200, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if page.Partial != nil {
		t.Fatalf("partial result %+v, want the tail to complete the answer", page.Partial)
	}
	if got := pageKeys(page.Files); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("walk rows (%d) != old statement rows (%d)\nwalk: %v\nold:  %v", len(got), len(want), got, want)
	}

	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	attrs := map[string]attribute.Value{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	if steps := attrs["search.continuation_steps"].AsInt64(); steps < 2 {
		t.Fatalf("continuation steps = %d, want at least 2 so a step edge is the next cursor", steps)
	}
	if !attrs["search.tail_ran"].AsBool() || attrs["search.tail_cancelled"].AsBool() {
		t.Fatalf("tail_ran=%v tail_cancelled=%v, want a tail that ran and completed from a step-edge cursor",
			attrs["search.tail_ran"].AsBool(), attrs["search.tail_cancelled"].AsBool())
	}
}
