// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

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

// The proof in this file runs against a disposable PostgreSQL with pg_trgm
// (ESHU_CONTENT_SEARCH_PROOF_DSN and ESHU_CONTENT_SEARCH_PROOF_DISPOSABLE=1)
// and skips without it. The fixture is synthetic: 3,000 files of about 8 KB
// plus one document of about 16 MB that sorts at key position 1,001 and is
// first in heap order, so both the key-ordered walk and the trigram tail meet
// it. The tail rechecks about 40 MB and a window with the document about
// 20 MB, so the tail cannot finish inside a timeout the window fits in, and
// only a key-ordered window can carry the walk past the document.

const (
	largeDocLiveRows = 3000
	// largeDocLiveToken has no trigram, so the tail rechecks every row,
	// the large document included.
	largeDocLiveToken = "zz"
	// largeDocLiveWords is the number of 33-byte words in the large document.
	largeDocLiveWords = 500_000
	// largeDocLiveRepo and largeDocLivePath sort between file 1,000 and
	// file 1,001.
	largeDocLiveRepo = "repo-005"
	largeDocLivePath = "src/file-001000x-large.go"
)

func openLargeDocumentFixture(t *testing.T) (context.Context, *sql.DB) {
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
		// The large document goes in first, so it is first in heap order: the
		// trigram tail's bitmap heap scan rechecks it early.
		fmt.Sprintf(`INSERT INTO content_files
			(repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
			SELECT '%s', '%s', 'sha',
			       (SELECT string_agg(md5(k::text), ' ') FROM generate_series(1, %d) k),
			       'large', 1, 'go', 'source'`, largeDocLiveRepo, largeDocLivePath, largeDocLiveWords),
		fmt.Sprintf(`INSERT INTO content_files
			(repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
			SELECT 'repo-' || lpad((g / 200)::text, 3, '0'),
			       'src/file-' || lpad(g::text, 6, '0') || '.go',
			       'sha',
			       (SELECT string_agg(md5(g::text || ':' || k::text), ' ') FROM generate_series(1, 256) k)
			         || CASE WHEN g %% 97 = 5 THEN ' %s' ELSE '' END,
			       md5(g::text), 1, 'go', 'source'
			FROM generate_series(1, %d) g`, largeDocLiveToken, largeDocLiveRows),
		`CREATE INDEX content_files_content_trgm_idx ON content_files USING gin (content gin_trgm_ops)`,
		`VACUUM ANALYZE content_files`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed large-document fixture: %v\n%s", err, statement)
		}
	}
	return ctx, db
}

// largeWindowCost times the shipped step statement over the 500-row window
// that holds the large document five times and returns the fastest run: the
// warm cost, least disturbed by other load on the host.
func largeWindowCost(t *testing.T, ctx context.Context, db *sql.DB) time.Duration {
	t.Helper()
	fastest := time.Duration(1<<63 - 1)
	for range 5 {
		start := time.Now()
		rows, err := db.QueryContext(ctx, unscoped.Statements().StepAfter,
			largeDocLiveToken, int64(500), int64(1000), "repo-004", "src/file-000900.go")
		if err != nil {
			t.Fatalf("time the large window: %v", err)
		}
		for rows.Next() {
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("time the large window: %v", err)
		}
		_ = rows.Close()
		fastest = min(fastest, time.Since(start))
	}
	return fastest
}

// TestResumeChainCrossesLargeDocumentLive plants one document whose window
// costs more than the flat 0.375 x budget timeout the walk used before #7730
// decision 3.2, at a budget twice that window's measured cost (about 40 ms for
// the 16 MB document on a laptop PostgreSQL 18, so a budget near 80 ms). The probe's
// timeout is now the remaining budget and a step's the room left in its phase,
// so a resume chain from the first row must cross the document: every call
// advances its cursor, and the gathered rows equal the old single statement's.
// With the flat timeout the probe that meets the document is cancelled after
// paying for it and the chain stops with progressed=false.
func TestResumeChainCrossesLargeDocumentLive(t *testing.T) {
	ctx, db := openLargeDocumentFixture(t)

	var want []string
	rows, err := db.QueryContext(ctx, `SELECT repo_id || '/' || relative_path FROM content_files
		WHERE content ILIKE '%' || $1 || '%' ORDER BY repo_id, relative_path`, largeDocLiveToken)
	if err != nil {
		t.Fatalf("oracle: %v", err)
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("oracle scan: %v", err)
		}
		want = append(want, key)
	}
	_ = rows.Close()
	if len(want) < 20 {
		t.Fatalf("fixture has %d matches, want at least 20", len(want))
	}

	// The budget is twice the window's measured cost, so the window costs
	// 0.5 x budget: over the old flat 0.375 x budget timeout, under the 0.55 x
	// budget phase room, and half the remaining budget a probe now gets. The
	// tail, about twice the window's work, cannot finish in its 0.5 x budget.
	// Deriving it from the measurement keeps those ratios on a fast or a slow
	// host; a fixed budget would not.
	cost := largeWindowCost(t, ctx, db)
	if cost < 10*time.Millisecond {
		t.Fatalf("the large window costs %v: too cheap to separate the timeouts from round trips", cost)
	}
	budget := 2 * cost
	t.Logf("large window cost %.1f ms (fastest of 5); budget %d ms; the flat timeout before 3.2 would be %d ms",
		float64(cost.Microseconds())/1000, budget.Milliseconds(), budget.Milliseconds()*300/800)

	spans := tracetest.NewSpanRecorder()
	searcher := &unscoped.Searcher{
		Store:  postgres.NewSQLReadStore(db),
		Budget: budget,
		Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	var gathered []string
	cursor := querycontract.SearchCursor{}
	crossed := false
	for call := 1; ; call++ {
		before := !pastLargeDocument(cursor)
		if call > 12 {
			t.Fatalf("no complete answer after 12 calls; gathered %d of %d", len(gathered), len(want))
		}
		page, err := searcher.Search(ctx, largeDocLiveToken, 100, 0, cursor)
		if err != nil {
			t.Fatalf("call %d: Search() error = %v", call, err)
		}
		attrs := lastSpanAttrs(t, spans)
		t.Logf("call %d: rows=%d partial=%v ended_in=%s steps=%d steps_cancelled=%d tail_ran=%v tail_cancelled=%v",
			call, len(page.Files), page.Partial != nil, attrs["search.ended_in"].AsString(),
			attrs["search.continuation_steps"].AsInt64(), attrs["search.steps_cancelled"].AsInt64(),
			attrs["search.tail_ran"].AsBool(), attrs["search.tail_cancelled"].AsBool())
		gathered = append(gathered, keys(page.Files)...)
		if page.Partial == nil {
			if before && attrs["search.tail_ran"].AsBool() && !attrs["search.tail_cancelled"].AsBool() {
				t.Fatalf("call %d: the tail answered across the large document; the fixture must make the walk cross it", call)
			}
			crossed = crossed || before
			break
		}
		if !page.Partial.Progressed {
			t.Fatalf("call %d: partial made no progress at the large document: %+v", call, page.Partial)
		}
		cursor = page.Partial.Cursor
		// Only a completed key-ordered window moves the cursor.
		crossed = crossed || (before && pastLargeDocument(cursor))
	}
	if strings.Join(gathered, ",") != strings.Join(want, ",") {
		t.Fatalf("gathered %d rows != old statement's %d rows\nwalk: %v\nold:  %v", len(gathered), len(want), gathered, want)
	}
	if !crossed {
		t.Fatal("no key-ordered window carried the walk past the large document")
	}
}

// pastLargeDocument reports whether a cursor sits at or after the large
// document's key, so the walk has already read it.
func pastLargeDocument(cursor querycontract.SearchCursor) bool {
	return cursor.RepoID > largeDocLiveRepo ||
		(cursor.RepoID == largeDocLiveRepo && cursor.RelativePath >= largeDocLivePath)
}

// lastSpanAttrs returns the attributes of the most recently ended span.
func lastSpanAttrs(t *testing.T, spans *tracetest.SpanRecorder) map[string]attribute.Value {
	t.Helper()
	ended := spans.Ended()
	if len(ended) == 0 {
		t.Fatal("no span ended")
	}
	out := map[string]attribute.Value{}
	for _, kv := range ended[len(ended)-1].Attributes() {
		out[string(kv.Key)] = kv.Value
	}
	return out
}
