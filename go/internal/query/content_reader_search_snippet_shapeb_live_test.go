// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason 05a shape-B proof: keyed snippet re-read enriches walk hits (exact snippet+line vs the oracle), tolerates purged and content-changed keys by omitting fields, keeps every row when its own statement timeout trips, and the full unscoped path ships snippets on complete pages; multi-outlier limit-200 scoped page stays within 2 s
//eshu:readiness:env ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE ESHU_SCOPED_SEARCH_PROOF_DSN

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestSnippetReReadEnrichesExactHits proves the shape-B keyed re-read fills
// the exact snippet and line for walk hits keyed by
// (repo_id, relative_path, content_hash): no LIKE predicate, the raw
// pattern anchors position() literally, so escaped wildcards stay literal.
func TestSnippetReReadEnrichesExactHits(t *testing.T) {
	ctx, db := openSnippetFixture(t)
	reader := NewContentReader(db)

	files := []querycontract.FileContent{
		{RepoID: snippetRepo, RelativePath: "eof.go", ContentHash: "hash-eof.go", LineCount: 3},
		{RepoID: snippetRepo, RelativePath: "wild-percent.go", ContentHash: "hash-wild-percent.go", LineCount: 1},
		{RepoID: snippetRepo, RelativePath: "crlf.go", ContentHash: "hash-crlf.go", LineCount: 3},
	}
	reader.enrichFileSnippets(ctx, "needle", files)

	got := snippetRowsByPath(files)["snip-repo\x1feof.go"]
	wantSnippet, wantLine := snippetOracle("first line\nsecond line\ntail needle", "needle")
	if got.MatchSnippet != wantSnippet || got.MatchLine != wantLine {
		t.Errorf("eof.go: (%q,%d), want (%q,%d)", got.MatchSnippet, got.MatchLine, wantSnippet, wantLine)
	}
	// The re-read must not touch Content: rows stay handles.
	if got.Content != "" {
		t.Errorf("eof.go: Content = %q, want empty (re-read must not fill bodies)", got.Content)
	}
}

// TestSnippetReReadOmitsStaleKeys proves a row whose content was purged or
// changed between the walk and the re-read keeps its place with both fields
// omitted: no error, no wrong snippet.
func TestSnippetReReadOmitsStaleKeys(t *testing.T) {
	ctx, db := openSnippetFixture(t)
	reader := NewContentReader(db)

	files := []querycontract.FileContent{
		{RepoID: snippetRepo, RelativePath: "eof.go", ContentHash: "hash-eof.go", LineCount: 3},
		{RepoID: snippetRepo, RelativePath: "does-not-exist.go", ContentHash: "hash-missing", LineCount: 1},
		{RepoID: snippetRepo, RelativePath: "crlf.go", ContentHash: "hash-changed", LineCount: 3},
	}
	reader.enrichFileSnippets(ctx, "needle", files)

	if files[0].MatchSnippet == "" || files[0].MatchLine == 0 {
		t.Errorf("live key: snippet/line = (%q,%d), want enriched", files[0].MatchSnippet, files[0].MatchLine)
	}
	for i, name := range []string{"purged", "changed"} {
		if files[i+1].MatchSnippet != "" || files[i+1].MatchLine != 0 {
			t.Errorf("%s key: snippet/line = (%q,%d), want both omitted", name, files[i+1].MatchSnippet, files[i+1].MatchLine)
		}
	}
}

// TestSnippetReReadTimeoutKeepsRows proves the re-read's own statement
// timeout degrades to omission: with a 1 ms budget against an 8 MB
// incompressible row the statement trips, the row is kept, and no field is
// filled. The same row then enriches under the production timeout, proving
// the trip was the timeout and not a broken statement.
func TestSnippetReReadTimeoutKeepsRows(t *testing.T) {
	ctx, db := openSnippetFixture(t)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO content_files (repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
		 VALUES ('snip-repo', 'timeout-blob.go', 'sha',
		         (SELECT string_agg(md5(g::text), '') FROM generate_series(1, 250000) g) || ' timeouttailtoken',
		         'hash-timeout', 1, 'go', 'source')`); err != nil {
		t.Fatalf("seed timeout blob: %v", err)
	}
	reader := NewContentReader(db)

	files := []querycontract.FileContent{
		{RepoID: snippetRepo, RelativePath: "timeout-blob.go", ContentHash: "hash-timeout", LineCount: 1},
	}
	reader.enrichFileSnippetsWithTimeout(ctx, "timeouttailtoken", files, time.Millisecond)

	if len(files) != 1 || files[0].RelativePath != "timeout-blob.go" {
		t.Fatalf("timeout must keep the row, got %+v", files)
	}
	if files[0].MatchSnippet != "" || files[0].MatchLine != 0 {
		t.Errorf("timed-out re-read: snippet/line = (%q,%d), want both omitted", files[0].MatchSnippet, files[0].MatchLine)
	}

	reader.enrichFileSnippets(ctx, "timeouttailtoken", files)
	if files[0].MatchSnippet == "" {
		t.Errorf("production-timeout re-read left the snippet empty, want the anchored excerpt (the 1 ms trip was the timeout, not a broken statement)")
	}
	if !strings.HasSuffix(files[0].MatchSnippet, " timeouttailtoken") {
		t.Errorf("re-read snippet = %q, want the EOF-anchored excerpt", files[0].MatchSnippet)
	}
}

// TestScopedSnippetMultiOutlierLimit200Within2s is proof item 2: a limit-200
// scoped page over a corpus with several multi-megabyte EOF-match outliers
// completes within 2 s absolute, every row carries its snippet, small rows
// carry exact lines, and outlier rows (match past 1M chars) omit the line
// per the fallback instead of eating the page budget.
func TestScopedSnippetMultiOutlierLimit200Within2s(t *testing.T) {
	ctx, db := openSnippetFixture(t)
	for i := 0; i < 4; i++ {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO content_files (repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
			 VALUES ('snip-repo', $1, 'sha', repeat('q', 8000000) || ' outlierneedle', $2, 1, 'go', 'source')`,
			"outlier-"+string(rune('a'+i))+".go", "hash-outlier-"+string(rune('a'+i))); err != nil {
			t.Fatalf("seed outlier %d: %v", i, err)
		}
	}
	if _, err := db.ExecContext(ctx, `VACUUM ANALYZE content_files`); err != nil {
		t.Fatalf("analyze outliers: %v", err)
	}
	reader := NewContentReader(db)

	start := time.Now()
	files, err := reader.searchFileContentPage(ctx, snippetRepo, "needle", 200, 0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("outlier page: reader error = %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("limit-200 multi-outlier page took %v, want <= 2s", elapsed)
	}
	t.Logf("limit-200 multi-outlier page: %d rows in %v", len(files), elapsed)
	outliers := 0
	for _, file := range files {
		if file.MatchSnippet == "" {
			t.Errorf("row %s: MatchSnippet empty on the outlier page", file.RelativePath)
		}
		if strings.HasPrefix(file.RelativePath, "outlier-") {
			outliers++
			if file.MatchLine != 0 {
				t.Errorf("row %s: MatchLine = %d, want omitted (match past 1M chars)", file.RelativePath, file.MatchLine)
			}
			continue
		}
		if file.RelativePath == "bigfallback.go" {
			continue
		}
		if file.MatchLine < 1 {
			t.Errorf("row %s: MatchLine = %d, want >= 1", file.RelativePath, file.MatchLine)
		}
	}
	if outliers != 4 {
		t.Errorf("outlier rows on page = %d, want 4", outliers)
	}
}

// TestUnscopedSnippetEndToEnd proves the full unscoped path ships snippets:
// on a complete page every row carries the exact first-match snippet and a
// positive line, and the walk outcome stays complete (Partial nil).
func TestUnscopedSnippetEndToEnd(t *testing.T) {
	ctx, db := openUnscopedLiveFixture(t)
	reader := NewContentReader(db)

	page, err := reader.SearchFilesUnscoped(ctx, liveRareToken, 10, 0, "", "", 0)
	if err != nil {
		t.Fatalf("unscoped rare-token search: %v", err)
	}
	if page.Partial != nil {
		t.Fatalf("rare-token page is partial (%+v), want the complete answer", page.Partial)
	}
	if len(page.Files) == 0 {
		t.Fatalf("rare-token page is empty, want the planted rows")
	}
	for _, file := range page.Files {
		if file.Content != "" {
			t.Errorf("row %s: Content = %q, want empty (handle contract)", file.RelativePath, file.Content)
		}
		if file.MatchSnippet == "" {
			t.Errorf("row %s: MatchSnippet empty on a complete unscoped page", file.RelativePath)
		}
		if file.MatchLine < 1 {
			t.Errorf("row %s: MatchLine = %d, want >= 1", file.RelativePath, file.MatchLine)
		}
		if !strings.Contains(strings.ToLower(file.MatchSnippet), strings.ToLower(liveRareToken)) {
			t.Errorf("row %s: MatchSnippet %q does not contain the pattern", file.RelativePath, file.MatchSnippet)
		}
	}
}
