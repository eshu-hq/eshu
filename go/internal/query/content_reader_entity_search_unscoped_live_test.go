// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
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
// and skip without it. The fixture is synthetic: 12,000 entities in 60
// repositories (three entities per file, so window edges can fall mid-file)
// whose source is hex words, with a few planted tokens. It has the shape that
// matters to the walk (a walk-order btree, a trigram GIN index, a dense
// token, a medium token, a selective literal, a zero-match token and a token
// with no extractable trigram) and none of any real corpus. The edge token is
// not seeded: the edge-row proof plants it on the window edges of the figures
// the shipped walk reports (plantEntityEdgeRows).

const (
	liveEntityFixtureRows = 12000
	liveEntityDenseToken  = "handler"
	liveEntityMediumToken = "widget"
	liveEntityRareToken   = "zq_rare_literal_k"
	liveEntityNoTrigram   = "zz"
	// liveEntityEdgeToken sits on the rows a window statement ends on and on
	// the row after each, so a duplicated or skipped boundary row changes the
	// answer. Window edges fall on walk-order row probeRows + stepRows*k when
	// the walk starts at the first row. The figures are read back from the
	// shipped walk by observeEntityWindowFigures, so a change to the entity
	// shape's figures moves the planted rows with it.
	liveEntityEdgeToken = "edgemark"
)

func openUnscopedEntityLiveFixture(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_CONTENT_SEARCH_PROOF_DSN"),
		os.Getenv("ESHU_CONTENT_SEARCH_PROOF_DISPOSABLE"),
		3*time.Minute,
	)
	postgresproof.InstallTrigramExtension(ctx, t, db)
	statements := []string{
		`CREATE TABLE content_entities (
			entity_id text PRIMARY KEY,
			repo_id text NOT NULL,
			relative_path text NOT NULL,
			entity_type text NOT NULL,
			entity_name text NOT NULL,
			start_line integer NOT NULL,
			end_line integer NOT NULL,
			language text,
			source_cache text,
			metadata jsonb NOT NULL
		)`,
		`CREATE FUNCTION eshu_require_content_substring_indexes_ready() RETURNS boolean
			LANGUAGE sql STABLE AS 'SELECT true'`,
		fmt.Sprintf(`INSERT INTO content_entities
			(entity_id, repo_id, relative_path, entity_type, entity_name,
			 start_line, end_line, language, source_cache, metadata)
			SELECT 'entity-' || lpad(g::text, 6, '0'),
			       'repo-' || lpad((((g - 1) / 3) / 67)::text, 3, '0'),
			       'src/file-' || lpad(((g - 1) / 3)::text, 6, '0') || '.go',
			       'Function',
			       'name-' || lpad(g::text, 6, '0'),
			       10 + 10 * ((g - 1) %% 3),
			       18 + 10 * ((g - 1) %% 3),
			       'go',
			       (SELECT string_agg(md5(g::text || ':' || k::text), ' ') FROM generate_series(1, CASE WHEN g %% 20 = 0 THEN 800 WHEN g %% 2 = 1 THEN 5 ELSE 50 END) k)
			         || CASE WHEN g %% 4 = 0 THEN ' %[2]s' ELSE '' END
			         || CASE WHEN g %% 50 = 7 THEN ' %[3]s' ELSE '' END
			         || CASE WHEN g IN (9001, 11000, 11777) THEN ' %[4]s' ELSE '' END
			         || CASE WHEN g IN (3, 10001, 11999) OR g %% 500 IN (200, 201) THEN ' %[5]s' ELSE '' END,
			       '{}'
			FROM generate_series(1, %[1]d) g`, liveEntityFixtureRows, liveEntityDenseToken, liveEntityMediumToken, liveEntityRareToken, liveEntityNoTrigram),
		`CREATE INDEX content_entities_repo_path_start_idx
			ON content_entities (repo_id, relative_path, start_line, entity_id)`,
		`CREATE INDEX content_entities_source_trgm_idx
			ON content_entities USING gin (source_cache gin_trgm_ops)`,
		`VACUUM ANALYZE content_entities`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed unscoped entity search fixture: %v\n%s", err, statement)
		}
	}
	return ctx, db
}

// entityOracleOffsetSQL derives the old single-statement entity page query
// from the shipped entity tail statement: the same WHERE and ORDER BY, with
// the old OFFSET. The hermetic TestEntityOracleStatementIsDerivedFromShippedTailText
// pins the derivation; no copy of the statement exists in the tests.
func entityOracleOffsetSQL(t *testing.T) string {
	t.Helper()
	shipped := unscoped.EntityStatements().TailFirst
	const marker = "LIMIT $2::bigint"
	if !strings.HasSuffix(strings.TrimSpace(shipped), marker) {
		t.Fatalf("shipped entity tail statement no longer ends with %q: %s", marker, shipped)
	}
	return strings.TrimSpace(shipped) + " OFFSET $3::bigint"
}

func entityOracleKeys(t *testing.T, ctx context.Context, db *sql.DB, pattern string, limit, offset int) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, entityOracleOffsetSQL(t), pattern, limit, offset)
	if err != nil {
		t.Fatalf("entity oracle query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var kind, id, repo, path, typ, name, lang, source string
		var start, end int
		var metadata []byte
		if err := rows.Scan(&kind, &id, &repo, &path, &typ, &name, &start, &end, &lang, &source, &metadata); err != nil {
			t.Fatalf("scan entity oracle: %v", err)
		}
		keys = append(keys, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("entity oracle rows: %v", err)
	}
	return keys
}

func entityPageKeys(hits []querycontract.EntitySearchHit) []string {
	keys := make([]string, 0, len(hits))
	for _, h := range hits {
		keys = append(keys, h.Entity.EntityID)
	}
	return keys
}

func entityContentKeys(entities []querycontract.EntityContent) []string {
	keys := make([]string, 0, len(entities))
	for _, e := range entities {
		keys = append(keys, e.EntityID)
	}
	return keys
}

// TestSearchEntitiesUnscopedMatchesOldStatementLive proves the walk returns
// the same ordered pages as the old single statement for every class that is
// exact inside the budget, including ILIKE wildcards and a pattern whose case
// differs from the stored text, and that its More flag agrees with the old
// look-ahead row.
func TestSearchEntitiesUnscopedMatchesOldStatementLive(t *testing.T) {
	ctx, db := openUnscopedEntityLiveFixture(t)
	reader := NewContentReader(db).WithUnscopedSearch(10*time.Second, nil)

	classes := map[string]string{
		"dense":      liveEntityDenseToken,
		"medium":     liveEntityMediumToken,
		"selective":  liveEntityRareToken,
		"zero match": "absent-token-xyz",
		"wildcard":   "han_ler",
		"upper case": strings.ToUpper(liveEntityMediumToken),
	}
	for name, pattern := range classes {
		for _, page := range []struct{ limit, offset int }{{10, 0}, {10, 10}, {10, 20}, {50, 0}, {2, 0}, {3, 0}} {
			oldStart := time.Now()
			want := entityOracleKeys(t, ctx, db, pattern, page.limit+1, page.offset)
			oldWall := time.Since(oldStart)
			newStart := time.Now()
			got, err := reader.SearchEntitiesUnscoped(ctx, pattern, page.limit, page.offset, querycontract.SearchCursor{})
			newWall := time.Since(newStart)
			if page.offset == 0 && page.limit == 10 {
				t.Logf("timing class=%q old=%.1fms new=%.1fms", name, float64(oldWall.Microseconds())/1000, float64(newWall.Microseconds())/1000)
			}
			if err != nil {
				t.Fatalf("%s %+v: SearchEntitiesUnscoped() error = %v", name, page, err)
			}
			if got.Partial != nil {
				t.Fatalf("%s %+v: partial inside a 10 s budget: %+v", name, page, got.Partial)
			}
			wantPage := want
			if len(wantPage) > page.limit {
				wantPage = wantPage[:page.limit]
			}
			gotIDs := entityContentKeys(got.Entities)
			if strings.Join(gotIDs, ",") != strings.Join(wantPage, ",") {
				t.Fatalf("%s %+v: walk page %v != old page %v", name, page, gotIDs, wantPage)
			}
			if got.More != (len(want) > page.limit) {
				t.Fatalf("%s %+v: More = %v, old statement saw %d rows for limit %d", name, page, got.More, len(want), page.limit)
			}
		}
	}
}

// TestEntityUnscopedSearchPlanShapesLive pins the plan contract the cost bound
// rests on: the key-ordered window walks the walk-order index and never
// touches the trigram index, and the tail run with index scans disabled is
// the trigram bitmap with no walk-order scan and no sequential scan.
func TestEntityUnscopedSearchPlanShapesLive(t *testing.T) {
	ctx, db := openUnscopedEntityLiveFixture(t)
	statements := unscoped.EntityStatements()

	for name, c := range map[string]struct {
		statement string
		args      []any
	}{
		"first window": {statements.StepFirst, []any{liveEntityRareToken, int64(200), int64(11)}},
		"next window": {
			statements.StepAfter,
			[]any{liveEntityRareToken, int64(500), int64(11), "repo-029", "src/file-002000.go", int32(10), "entity-006001"},
		},
	} {
		plan := explainShipped(t, ctx, db, false, c.statement, c.args...)
		joined := strings.Join(planNodes(plan), " ")
		if strings.Contains(joined, "content_entities_source_trgm_idx") || strings.Contains(joined, "Bitmap") {
			t.Errorf("%s plan touches the trigram index: %s", name, joined)
		}
		if !strings.Contains(joined, "content_entities_repo_path_start_idx") {
			t.Errorf("%s plan does not walk the walk-order index: %s", name, joined)
		}
	}

	for name, c := range map[string]struct {
		statement string
		args      []any
	}{
		"tail from the start, selective": {statements.TailFirst, []any{liveEntityRareToken, int64(11)}},
		"tail from the start, medium":    {statements.TailFirst, []any{liveEntityMediumToken, int64(11)}},
		"tail after a cursor, selective": {
			statements.TailAfter,
			[]any{liveEntityRareToken, int64(11), "repo-029", "src/file-002000.go", int32(10), "entity-006001"},
		},
		"tail after a cursor, medium": {
			statements.TailAfter,
			[]any{liveEntityMediumToken, int64(11), "repo-029", "src/file-002000.go", int32(10), "entity-006001"},
		},
	} {
		plan := explainShipped(t, ctx, db, true, c.statement, c.args...)
		nodes := planNodes(plan)
		joined := strings.Join(nodes, " ")
		if !strings.Contains(joined, "Bitmap Index Scan/content_entities_source_trgm_idx") {
			t.Errorf("%s plan is not the trigram bitmap: %s", name, joined)
		}
		// The keyset range may enter as a bitmap on the walk-order index
		// (BitmapAnd); what must not appear is an ordered walk of it or a
		// sequential scan, so the ban matches whole plan nodes, not
		// substrings.
		for _, node := range nodes {
			if node == "Index Scan/content_entities_repo_path_start_idx" || strings.HasPrefix(node, "Seq Scan") {
				t.Errorf("%s plan contains %s: %s", name, node, joined)
			}
		}
	}
}

// entityLiveWalk is one live entity search with the span attributes that say
// which phases ran.
type entityLiveWalk struct {
	page   unscoped.EntityPage
	budget time.Duration
	steps  int64
	tail   bool
	cancel bool
	// probeRows is the probe window the walk completed and continuationRows
	// the rows its continuation steps covered, both as the walk's own span
	// reports them.
	probeRows        int64
	continuationRows int64
}

// searchEntityLive runs an entity search at the given budget and records its
// phases.
func searchEntityLive(t *testing.T, ctx context.Context, db *sql.DB, budget time.Duration, token string, limit int) entityLiveWalk {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	searcher := &unscoped.Searcher{
		Store:  postgres.NewSQLReadStore(db),
		Budget: budget,
		Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	page, err := searcher.SearchEntities(ctx, token, limit, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("SearchEntities(%d ms) error = %v", budget.Milliseconds(), err)
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	attrs := map[string]attribute.Value{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	return entityLiveWalk{
		page:   page,
		budget: budget,
		steps:  attrs["search.continuation_steps"].AsInt64(),
		tail:   attrs["search.tail_ran"].AsBool(),
		cancel: attrs["search.tail_cancelled"].AsBool(),

		probeRows:        attrs["search.probe_rows_visited"].AsInt64(),
		continuationRows: attrs["search.continuation_rows"].AsInt64(),
	}
}

// entityWindowFigures are the window sizes the shipped entity walk used for
// one search.
type entityWindowFigures struct {
	probeRows, stepRows int
}

// observeEntityWindowFigures reads the entity shape's probe and step window
// sizes back from the walk itself. A zero-match search over a table larger than
// the probe visits every window, so the span's probe_rows_visited is the probe
// size and continuation_rows over continuation_steps is the step size. The
// figures belong to the unexported shape; reading them from the walk's own
// telemetry adds no exported surface and cannot drift from what production
// runs.
func observeEntityWindowFigures(t *testing.T, ctx context.Context, db *sql.DB) entityWindowFigures {
	t.Helper()
	walk := searchEntityLive(t, ctx, db, unscoped.MaxBudget, "absent-token-xyz-figures", 10)
	if walk.page.Partial != nil || walk.probeRows <= 0 || walk.steps <= 0 || walk.continuationRows%walk.steps != 0 {
		t.Fatalf("cannot read the entity window figures from a zero-match walk: probe=%d steps=%d continuation_rows=%d partial=%+v",
			walk.probeRows, walk.steps, walk.continuationRows, walk.page.Partial)
	}
	return entityWindowFigures{probeRows: int(walk.probeRows), stepRows: int(walk.continuationRows / walk.steps)}
}

// entityEdgeOrdinals returns the 1-based walk-order rows to plant the edge
// token on: the last row of every full window (probeRows + stepRows*k) and the
// row after it. totalRows bounds the table; an edge whose next row does not
// exist is not an edge.
func entityEdgeOrdinals(f entityWindowFigures, totalRows int) []int {
	var ordinals []int
	for edge := f.probeRows; edge+1 <= totalRows; edge += f.stepRows {
		ordinals = append(ordinals, edge, edge+1)
	}
	return ordinals
}

// plantEntityEdgeRows appends the edge token to the rows on and after every
// window edge of the shipped figures and returns the planted walk-order rows.
// The probe and at least two continuation steps must each end on a planted
// edge, so the fixture is too small for the figures if fewer than six rows
// would be planted; that fails the test instead of passing on a fixture the
// windows never reach.
func plantEntityEdgeRows(t *testing.T, ctx context.Context, db *sql.DB, f entityWindowFigures) []int {
	t.Helper()
	var total int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM content_entities`).Scan(&total); err != nil {
		t.Fatalf("count content_entities: %v", err)
	}
	ordinals := entityEdgeOrdinals(f, total)
	if len(ordinals) < 6 {
		t.Fatalf("fixture has %d rows, too few for probe=%d step=%d: want at least three window edges, planted rows %v",
			total, f.probeRows, f.stepRows, ordinals)
	}
	list := make([]string, 0, len(ordinals))
	for _, o := range ordinals {
		list = append(list, fmt.Sprintf("%d", o))
	}
	result, err := db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE content_entities SET source_cache = source_cache || ' %s'
		WHERE entity_id IN (
			SELECT entity_id FROM (
				SELECT entity_id, row_number() OVER (ORDER BY repo_id, relative_path, start_line, entity_id) AS n
				FROM content_entities) ordered
			WHERE n IN (%s))`, liveEntityEdgeToken, strings.Join(list, ", ")))
	if err != nil {
		t.Fatalf("plant edge token: %v", err)
	}
	if planted, err := result.RowsAffected(); err != nil || int(planted) != len(ordinals) {
		t.Fatalf("planted %d rows (err %v), want %d", planted, err, len(ordinals))
	}
	if _, err := db.ExecContext(ctx, `VACUUM ANALYZE content_entities`); err != nil {
		t.Fatalf("analyze after planting: %v", err)
	}
	return ordinals
}

// TestSearchEntitiesUnscopedEdgeRowsAreExactLive is the SQL-level exactness
// proof for the four-part keyset and the window edge. The edge token is
// planted on the last row of the probe window and of every continuation window
// of the figures the shipped walk reports, and on the row after each. The
// budget is chosen so the probe, at least two continuation steps and then the
// trigram tail all run, and the answer must equal the old statement's rows
// exactly. An inclusive keyset operator duplicates a boundary row; an edge
// offset past the window skips one; either changes the list. Both mutations
// fail this test on the shipped figures and on any other figures, because the
// plant positions follow the walk.
func TestSearchEntitiesUnscopedEdgeRowsAreExactLive(t *testing.T) {
	ctx, db := openUnscopedEntityLiveFixture(t)
	figures := observeEntityWindowFigures(t, ctx, db)
	planted := plantEntityEdgeRows(t, ctx, db, figures)
	t.Logf("entity window figures probe=%d step=%d; edge token planted on walk-order rows %v", figures.probeRows, figures.stepRows, planted)

	want := entityOracleKeys(t, ctx, db, liveEntityEdgeToken, 1000, 0)
	if len(want) != len(planted) {
		t.Fatalf("old statement returned %d edge rows, planted %d", len(want), len(planted))
	}
	searchEntityLiveUntil(t, ctx, db, liveEntityEdgeToken, 200,
		func(walk entityLiveWalk) {
			got := entityPageKeys(walk.page.Hits)
			if walk.page.Partial != nil {
				// A cut-short walk returns an ordered prefix of the exact answer.
				if len(got) > len(want) || strings.Join(got, ",") != strings.Join(want[:len(got)], ",") {
					t.Fatalf("budget %d ms: partial rows %v are not a prefix of the old rows", walk.budget.Milliseconds(), got)
				}
				return
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("budget %d ms: walk rows (%d) != old statement rows (%d)\nwalk: %v\nold:  %v",
					walk.budget.Milliseconds(), len(got), len(want), got, want)
			}
			if walk.page.More {
				t.Fatalf("budget %d ms: More=true but the old statement has no row past the %d it returned", walk.budget.Milliseconds(), len(want))
			}
		},
		func(walk entityLiveWalk) bool {
			return walk.page.Partial == nil && walk.steps >= 2 && walk.tail && !walk.cancel
		})
}

// searchEntityLiveUntil runs the entity search at a series of budgets until
// accept holds for the phases that ran. Which phases run depends on host
// speed: the probe and two 5,000-row steps must fit the continuation room
// (0.2 x budget) and the next step must not, so the viable budget band is
// narrow in absolute terms and moves with the host. The budgets therefore
// climb geometrically from the smallest accepted budget to the largest, so
// some budget lands in the band on a fast or a loaded host. Rows and the
// look-ahead flag must be exact at every budget; only the choice of the
// budget that exercises the wanted phases is adaptive, and failing to find
// one is a failure, never a skip.
func searchEntityLiveUntil(
	t *testing.T, ctx context.Context, db *sql.DB, token string, limit int,
	check func(walk entityLiveWalk),
	accept func(walk entityLiveWalk) bool,
) entityLiveWalk {
	t.Helper()
	var tried []string
	for budget := unscoped.MinBudget; budget <= unscoped.MaxBudget; budget = budget * 6 / 5 {
		walk := searchEntityLive(t, ctx, db, budget, token, limit)
		check(walk)
		if accept(walk) {
			return walk
		}
		tried = append(tried, fmt.Sprintf("%dms(steps=%d tail=%v cancelled=%v)", budget.Milliseconds(), walk.steps, walk.tail, walk.cancel))
	}
	t.Fatalf("no budget exercised the wanted phases; tried %s", strings.Join(tried, " "))
	return entityLiveWalk{}
}
