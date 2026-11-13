// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason 05a scoped snippet+line proof: exact snippet/line vs an independent oracle on a fixture corpus with edge rows (offset-1, EOF, CRLF, rune boundary, İ/Σ/ß folds, escaped wildcards), pos=0 omission, the 1M line fallback, fold-length parity with the deployed collation, and the shipped plan shape; one local PostgreSQL 18 run took about 30 s
//eshu:readiness:env ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE ESHU_SCOPED_SEARCH_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	snippetRepo  = "snip-repo"
	snippetRepoB = "snip-repo-b"
)

// snippetEdgeRow is one hand-planted corpus row. oracleOK is false for rows
// whose fold behavior Go cannot reproduce: Go strings.ToLower applies full
// case folding (İ gains a combining dot, length changes) while Postgres
// lower() folds per codepoint (İ becomes i, length preserved — the 05a shim
// proved max length drift 0 corpus-wide). Fold rows carry hand-pinned
// expectations and never pass through snippetOracle.
type snippetEdgeRow struct {
	repo     string
	path     string
	content  string
	patterns []string
	oracleOK bool
	// pinned, for oracleOK=false rows: expected snippet/line per pattern.
	pinned map[string]struct {
		snippet string
		line    int
	}
}

func snippetEdgeCorpus() []snippetEdgeRow {
	bigOK := strings.Repeat("ab", 450000) + " needle tail"
	bigFallback := strings.Repeat("ab", 600000) + " needle tail"
	return []snippetEdgeRow{
		{repo: snippetRepo, path: "offset1.go", content: "needle at the very start\nsecond line\n", patterns: []string{"needle"}, oracleOK: true},
		{repo: snippetRepo, path: "eof.go", content: "first line\nsecond line\ntail needle", patterns: []string{"needle"}, oracleOK: true},
		{repo: snippetRepo, path: "crlf.go", content: "alpha\r\nbeta needle gamma\r\nthird\r\n", patterns: []string{"needle"}, oracleOK: true},
		{repo: snippetRepo, path: "notrail.go", content: "one\ntwo needle", patterns: []string{"needle"}, oracleOK: true},
		{repo: snippetRepo, path: "runeboundary.go", content: "héllo wörld needle tail", patterns: []string{"needle"}, oracleOK: true},
		{repo: snippetRepo, path: "multiline.go", content: "l1\nl2\nl3\nl4 needle here\nl5\n", patterns: []string{"needle"}, oracleOK: true},
		{repo: snippetRepo, path: "wild-percent.go", content: "pay 100% coverage now", patterns: []string{"100%"}, oracleOK: true},
		{repo: snippetRepo, path: "wild-underscore.go", content: "use a_b literal here", patterns: []string{"a_b"}, oracleOK: true},
		{repo: snippetRepo, path: "wild-backslash.go", content: `path c\d end`, patterns: []string{`c\d`}, oracleOK: true},
		{repo: snippetRepo, path: "wild-mix.go", content: `x%y_z\w tail`, patterns: []string{`x%y_z\w`}, oracleOK: true},
		{repo: snippetRepo, path: "bigline.go", content: bigOK, patterns: []string{"needle"}, oracleOK: true},
		{repo: snippetRepo, path: "bigfallback.go", content: bigFallback, patterns: []string{"needle"}, oracleOK: true},
		{
			repo: snippetRepo, path: "ifold-aib.go", content: "aİb", patterns: []string{"aib"}, oracleOK: false,
			pinned: map[string]struct {
				snippet string
				line    int
			}{"aib": {snippet: "aİb", line: 1}},
		},
		{
			repo: snippetRepo, path: "ifold-sigma.go", content: "prefix ΣΊΣΥΦΟΣ suffix", patterns: []string{"σίσυφοσ"}, oracleOK: false,
			pinned: map[string]struct {
				snippet string
				line    int
			}{"σίσυφοσ": {snippet: "prefix ΣΊΣΥΦΟΣ suffix", line: 1}},
		},
		{
			// Kelvin sign (U+212A) folds to k under lower(): a genuine
			// length-preserving non-ASCII fold, unlike ß (lower keeps ß,
			// so only the literal ß matches).
			repo: snippetRepo, path: "ifold-kelvin.go", content: "100K resistor", patterns: []string{"100k"}, oracleOK: false,
			pinned: map[string]struct {
				snippet string
				line    int
			}{"100k": {snippet: "100K resistor", line: 1}},
		},
		{
			repo: snippetRepo, path: "ifold-eszett.go", content: "Straße data", patterns: []string{"straße"}, oracleOK: false,
			pinned: map[string]struct {
				snippet string
				line    int
			}{"straße": {snippet: "Straße data", line: 1}},
		},
		{repo: snippetRepoB, path: "other.go", content: "unrelated needle row in second repo", patterns: []string{"needle"}, oracleOK: true},
	}
}

// openSnippetFixture seeds the edge corpus into a disposable PostgreSQL with
// the production content_files shape (generated content_lower, PK, both
// trigram indexes) and skips without the proof DSN.
func openSnippetFixture(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_SCOPED_SEARCH_PROOF_DSN"),
		os.Getenv("ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE"),
		3*time.Minute,
	)
	postgresproof.InstallTrigramExtension(ctx, t, db)
	ddl := []string{
		`CREATE TABLE content_files (
			repo_id text NOT NULL,
			relative_path text NOT NULL,
			commit_sha text,
			content text NOT NULL,
			content_lower text GENERATED ALWAYS AS (lower(content)) STORED,
			content_hash text NOT NULL,
			line_count integer NOT NULL,
			language text,
			artifact_type text,
			PRIMARY KEY (repo_id, relative_path)
		)`,
		`CREATE INDEX content_files_content_trgm_idx ON content_files USING gin (content gin_trgm_ops)`,
		`CREATE INDEX content_files_content_lower_trgm_idx ON content_files USING gin (content_lower gin_trgm_ops)`,
	}
	for _, statement := range ddl {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed snippet fixture DDL: %v\n%s", err, statement)
		}
	}
	for _, row := range snippetEdgeCorpus() {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO content_files (repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
			 VALUES ($1, $2, 'sha', $3, $4, $5, 'go', 'source')`,
			row.repo, row.path, row.content, "hash-"+row.path,
			1+strings.Count(row.content, "\n")); err != nil {
			t.Fatalf("seed snippet row %s: %v", row.path, err)
		}
	}
	if _, err := db.ExecContext(ctx, `VACUUM ANALYZE content_files`); err != nil {
		t.Fatalf("analyze snippet fixture: %v", err)
	}
	return ctx, db
}

// snippetOracle recomputes the expected first-match snippet and line from
// raw content in Go. It is valid only for rows where Go strings.ToLower is
// length-preserving and agrees with Postgres lower() (ASCII plus Latin-1
// accents): the ASCII bytes of the pattern can never match inside a
// multi-byte rune, so the byte match start is always a rune boundary and the
// character offset is exact.
func snippetOracle(content, pattern string) (snippet string, line int) {
	lc := strings.ToLower(content)
	lp := strings.ToLower(pattern)
	idx := strings.Index(lc, lp)
	if idx < 0 {
		return "", 0
	}
	pos := utf8.RuneCountInString(lc[:idx]) + 1
	runes := []rune(content)
	start := pos - 40
	if start < 1 {
		start = 1
	}
	end := start - 1 + 120
	if end > len(runes) {
		end = len(runes)
	}
	snippet = string(runes[start-1 : end])
	line = 1 + strings.Count(string(runes[:pos-1]), "\n")
	return snippet, line
}

// snippetRowsByPath indexes one page of reader rows by path.
func snippetRowsByPath(files []FileContent) map[string]FileContent {
	out := make(map[string]FileContent, len(files))
	for _, file := range files {
		out[file.RepoID+"\x1f"+file.RelativePath] = file
	}
	return out
}

// TestScopedSnippetMatchesOracle proves every scoped file-search row carries
// the exact first-match snippet and line: single- and multi-scope, ASCII
// edge rows against the independent Go oracle, fold rows against hand-pinned
// expectations, wildcard literals anchoring literally, Content still empty,
// and per-row values stable across pages.
func TestScopedSnippetMatchesOracle(t *testing.T) {
	ctx, db := openSnippetFixture(t)
	reader := NewContentReader(db)

	oraclePatterns := []string{"needle", "100%", "a_b", `c\d`, `x%y_z\w`}
	for _, pattern := range oraclePatterns {
		files, err := reader.searchFileContentPage(ctx, snippetRepo, pattern, 200, 0)
		if err != nil {
			t.Fatalf("pattern %q: reader error = %v", pattern, err)
		}
		if len(files) == 0 {
			t.Fatalf("pattern %q: no rows, want oracle rows", pattern)
		}
		for _, file := range files {
			if file.Content != "" {
				t.Errorf("pattern %q row %s: Content = %q, want empty (handle contract)", pattern, file.RelativePath, file.Content)
			}
			if file.RelativePath == "bigfallback.go" {
				continue // line omitted by the 1M fallback; covered by TestScopedSnippetLineFallbackBoundary
			}
			wantSnippet, wantLine := snippetOracle(snippetCorpusContent(t, file.RepoID, file.RelativePath), pattern)
			if wantSnippet == "" {
				t.Fatalf("pattern %q row %s: oracle found no match", pattern, file.RelativePath)
			}
			if file.MatchSnippet != wantSnippet {
				t.Errorf("pattern %q row %s: MatchSnippet = %q, want %q", pattern, file.RelativePath, file.MatchSnippet, wantSnippet)
			}
			if file.MatchLine != wantLine {
				t.Errorf("pattern %q row %s: MatchLine = %d, want %d", pattern, file.RelativePath, file.MatchLine, wantLine)
			}
			if file.MatchLine < 1 {
				t.Errorf("pattern %q row %s: MatchLine = %d, want >= 1 (exact or absent, never approximate)", pattern, file.RelativePath, file.MatchLine)
			}
		}
	}

	// Multi-scope carries the same derivations.
	multi, err := reader.searchFileContentInRepos(ctx, []string{snippetRepo, snippetRepoB}, "needle", 200, 0)
	if err != nil {
		t.Fatalf("multi-scope: reader error = %v", err)
	}
	byPath := snippetRowsByPath(multi)
	for _, row := range snippetEdgeCorpus() {
		if len(row.patterns) == 0 || row.patterns[0] != "needle" || !row.oracleOK {
			continue
		}
		if row.path == "bigfallback.go" {
			continue // line omitted by the 1M fallback; covered by TestScopedSnippetLineFallbackBoundary
		}
		got, ok := byPath[row.repo+"\x1f"+row.path]
		if !ok {
			t.Errorf("multi-scope: missing row %s/%s", row.repo, row.path)
			continue
		}
		wantSnippet, wantLine := snippetOracle(row.content, "needle")
		if got.MatchSnippet != wantSnippet || got.MatchLine != wantLine {
			t.Errorf("multi-scope row %s: snippet/line = (%q,%d), want (%q,%d)", row.path, got.MatchSnippet, got.MatchLine, wantSnippet, wantLine)
		}
	}

	// Paging-stable by construction: the same row on two pages carries the
	// same snippet and line.
	first, err := reader.searchFileContentPage(ctx, snippetRepo, "needle", 3, 0)
	if err != nil {
		t.Fatalf("page one: reader error = %v", err)
	}
	second, err := reader.searchFileContentPage(ctx, snippetRepo, "needle", 3, 3)
	if err != nil {
		t.Fatalf("page two: reader error = %v", err)
	}
	overlap := snippetRowsByPath(append(append([]FileContent{}, first...), second...))
	full, err := reader.searchFileContentPage(ctx, snippetRepo, "needle", 200, 0)
	if err != nil {
		t.Fatalf("full page: reader error = %v", err)
	}
	for _, file := range full {
		key := file.RepoID + "\x1f" + file.RelativePath
		if paged, ok := overlap[key]; ok {
			if paged.MatchSnippet != file.MatchSnippet || paged.MatchLine != file.MatchLine {
				t.Errorf("row %s: paged (%q,%d) != full-page (%q,%d)", key, paged.MatchSnippet, paged.MatchLine, file.MatchSnippet, file.MatchLine)
			}
		}
	}
}

// snippetCorpusContent returns the planted content for a corpus row.
func snippetCorpusContent(t *testing.T, repo, path string) string {
	t.Helper()
	for _, row := range snippetEdgeCorpus() {
		if row.repo == repo && row.path == path {
			return row.content
		}
	}
	t.Fatalf("unknown corpus row %s/%s", repo, path)
	return ""
}

// TestScopedSnippetFoldRowsExact pins the fold-sensitive rows by hand: Go
// full case folding changes their length, so only hand derivation (checked
// against the 05a shim's length-parity proof) is an independent oracle here.
func TestScopedSnippetFoldRowsExact(t *testing.T) {
	ctx, db := openSnippetFixture(t)
	reader := NewContentReader(db)

	for _, row := range snippetEdgeCorpus() {
		if row.oracleOK {
			continue
		}
		for pattern, want := range row.pinned {
			files, err := reader.searchFileContentPage(ctx, row.repo, pattern, 10, 0)
			if err != nil {
				t.Fatalf("row %s pattern %q: reader error = %v", row.path, pattern, err)
			}
			if len(files) != 1 {
				t.Fatalf("row %s pattern %q: %d rows, want exactly this row", row.path, pattern, len(files))
			}
			got := files[0]
			if got.MatchSnippet != want.snippet {
				t.Errorf("row %s: MatchSnippet = %q, want %q", row.path, got.MatchSnippet, want.snippet)
			}
			if got.MatchLine != want.line {
				t.Errorf("row %s: MatchLine = %d, want %d", row.path, got.MatchLine, want.line)
			}
		}
	}
}

// TestScopedSnippetPosZeroOmitsWithoutError drives the shipped builder with
// a predicate that matches a row the raw pattern never occurs in, proving
// the CASE guards return NULLs (fields omitted) instead of erroring on the
// negative-length substring the bare expression would build.
func TestScopedSnippetPosZeroOmitsWithoutError(t *testing.T) {
	ctx, db := openSnippetFixture(t)

	const rawAbsent = "zzznomatchpresent"
	// length($2) >= 0 is always true; it exists only to give the unused
	// predicate bind a type, since an unreferenced bind has none.
	where := "repo_id = $1 AND length($2) >= 0"
	query := fileContentScopedQuery(where, 3, 4, 5)
	rows, err := db.QueryContext(ctx, query, snippetRepo, querycontract.EscapeLikeLiteral("needle"), rawAbsent, 10, 0)
	if err != nil {
		t.Fatalf("pos=0 statement error = %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var file FileContent
		var snippet sql.NullString
		var line sql.NullInt64
		if err := rows.Scan(&file.RepoID, &file.RelativePath, &file.CommitSHA, &file.Content,
			&file.ContentHash, &file.LineCount, &file.Language, &file.ArtifactType, &snippet, &line); err != nil {
			t.Fatalf("pos=0 scan error = %v", err)
		}
		seen++
		if snippet.Valid || line.Valid {
			t.Errorf("row %s: pos=0 must omit both fields, got (%q,%v)", file.RelativePath, snippet.String, line.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("pos=0 rows error = %v", err)
	}
	if seen == 0 {
		t.Fatalf("pos=0 probe matched no rows, want the corpus rows")
	}
}

// TestScopedSnippetLineFallbackBoundary proves the pre-approved 1M fallback:
// a match starting within 1M characters keeps its exact line, a match past
// 1M characters still ships its snippet but omits the line.
func TestScopedSnippetLineFallbackBoundary(t *testing.T) {
	ctx, db := openSnippetFixture(t)
	reader := NewContentReader(db)

	files, err := reader.searchFileContentPage(ctx, snippetRepo, "needle", 200, 0)
	if err != nil {
		t.Fatalf("reader error = %v", err)
	}
	byPath := snippetRowsByPath(files)
	within, ok := byPath[snippetRepo+"\x1f"+"bigline.go"]
	if !ok {
		t.Fatalf("bigline.go missing from page")
	}
	if within.MatchLine != 1 {
		t.Errorf("bigline.go (pos-1 = 900000): MatchLine = %d, want 1 (under the 1M bound)", within.MatchLine)
	}
	if within.MatchSnippet == "" {
		t.Errorf("bigline.go: MatchSnippet empty, want the anchored excerpt")
	}
	past, ok := byPath[snippetRepo+"\x1f"+"bigfallback.go"]
	if !ok {
		t.Fatalf("bigfallback.go missing from page")
	}
	if past.MatchLine != 0 {
		t.Errorf("bigfallback.go (pos-1 = 1200000): MatchLine = %d, want omitted (over the 1M bound)", past.MatchLine)
	}
	if past.MatchSnippet == "" {
		t.Errorf("bigfallback.go: MatchSnippet empty, want the anchored excerpt even when the line is omitted")
	}
	if !strings.HasSuffix(past.MatchSnippet, " needle tail") {
		t.Errorf("bigfallback.go: MatchSnippet = %q, want the EOF-anchored excerpt", past.MatchSnippet)
	}
}

// TestScopedSnippetFoldLengthMatchesDeployedCollation asserts the shim's
// length-parity premise on the database the tests actually run against: no
// row's lowercase copy differs in character length, so character offsets
// anchor identically in content and content_lower under this collation.
func TestScopedSnippetFoldLengthMatchesDeployedCollation(t *testing.T) {
	ctx, db := openSnippetFixture(t)

	var collate, ctype, encoding string
	if err := db.QueryRowContext(ctx,
		`SELECT datcollate, datctype, pg_encoding_to_char(encoding) FROM pg_database WHERE datname = current_database()`,
	).Scan(&collate, &ctype, &encoding); err != nil {
		t.Fatalf("read fixture collation: %v", err)
	}
	t.Logf("snippet fixture collation datcollate=%s datctype=%s encoding=%s", collate, ctype, encoding)
	if encoding != "UTF8" {
		t.Fatalf("fixture encoding = %s, want UTF8", encoding)
	}
	var maxDiff, minDiff int
	if err := db.QueryRowContext(ctx,
		`SELECT max(length(content_lower) - length(content)), min(length(content_lower) - length(content)) FROM content_files`,
	).Scan(&maxDiff, &minDiff); err != nil {
		t.Fatalf("fold-length parity: %v", err)
	}
	if maxDiff != 0 || minDiff != 0 {
		t.Fatalf("fold-length drift max=%d min=%d, want 0 (offsets would misalign)", maxDiff, minDiff)
	}
}

// TestScopedSearchSnippetPlanShape derives the shipped statement from the
// production builder and proves it plans a bounded index path: the snippet
// lateral must not introduce a sequential scan. The bulk rows live in this
// fixture's own table (the shared #7930 workbound fixture predates
// content_lower and must keep its work-bound shape untouched).
func TestScopedSearchSnippetPlanShape(t *testing.T) {
	ctx, db := openSnippetFixture(t)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO content_files
			(repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
			SELECT 'snip-repo',
			       'src/plan-' || lpad(g::text, 6, '0') || '.go',
			       'sha',
			       (SELECT string_agg(md5(g::text || ':' || k::text), ' ') FROM generate_series(1, 30) k)
			         || CASE WHEN g IN (101, 9999, 19999) THEN ' planrare' ELSE '' END,
			       md5(g::text), 1, 'go', 'source'
			FROM generate_series(1, 20000) g`); err != nil {
		t.Fatalf("seed plan bulk rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, `VACUUM ANALYZE content_files`); err != nil {
		t.Fatalf("analyze plan fixture: %v", err)
	}

	single := fileContentSingleScopeWhere(1, 2)
	query, args := fileContentScopedQuery(single, 3, 4, 5),
		[]any{snippetRepo, querycontract.EscapeLikeLiteral("planrare"), "planrare", int64(10), int64(0)}
	rows, err := db.QueryContext(ctx, "EXPLAIN (FORMAT JSON) "+query, args...)
	if err != nil {
		t.Fatalf("explain JSON: %v", err)
	}
	defer func() { _ = rows.Close() }()
	raw := ""
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		raw += line
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	if strings.Contains(raw, "Seq Scan") {
		t.Errorf("shipped snippet statement plans a Seq Scan on the selective token:\n%s", raw)
	}
	if !strings.Contains(raw, "content_files") {
		t.Errorf("shipped snippet statement lost the content_files scan:\n%s", raw)
	}
	reader := NewContentReader(db)
	files, err := reader.searchFileContentPage(ctx, snippetRepo, "planrare", 10, 0)
	if err != nil {
		t.Fatalf("execute shipped snippet statement: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("planrare page holds %d rows, want the 3 planted hits", len(files))
	}
}
