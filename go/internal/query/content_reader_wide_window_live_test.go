// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
)

// The proofs in this file run against a disposable PostgreSQL
// (ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN and
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1) and skip without it. They pin
// the statement shapes of the lifted config-artifact cap (#7951) on a
// repository that fills the 5,000-file window: the blank check is sent in
// chunks of BlankCheckChunkSize paths and each chunk keeps an index-scan plan,
// and the wide reads return exactly what single-file reads return.

const wideWindowRepo = "repo-wide"

// wideWindowFixture seeds the 5,000-file repository: 4,950 Terraform var files
// (the blank-checked class; every seventh one blank) and 50 Terraform source
// files (the read class), among 300 filler repositories of 40 rows. It
// returns the seeded contents by path.
func wideWindowFixture(t *testing.T) (context.Context, *sql.DB, map[string]string) {
	t.Helper()
	ctx, db := openNonBlankLiveFixture(t)
	if _, err := db.ExecContext(ctx, `CREATE INDEX content_files_repo_path_pattern_idx
		ON content_files (repo_id, relative_path text_pattern_ops)`); err != nil {
		t.Fatalf("create pattern index: %v", err)
	}
	for r := 0; r < 300; r++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO content_files
			(repo_id, relative_path, content, content_hash, line_count)
			SELECT $1, 'dir/file-' || g || '.yaml', repeat('k: v' || g || E'\n', 40), 'h', 40
			FROM generate_series(1, 40) g`, fmt.Sprintf("repo-fill-%03d", r)); err != nil {
			t.Fatalf("seed filler: %v", err)
		}
	}
	// Var files carry about 6 KB of distinct text so the large ones are
	// stored out of line, as real JSON and YAML configuration is.
	if _, err := db.ExecContext(ctx, `INSERT INTO content_files
		(repo_id, relative_path, content, content_hash, line_count)
		SELECT $1, 'env/e' || lpad(g::text, 5, '0') || '.tfvars',
		       CASE WHEN g % 7 = 0 THEN E' \n\t'
		            ELSE (SELECT string_agg(md5(g::text || ':' || i), E'\n') FROM generate_series(1, 180) i) END,
		       'h', 180
		FROM generate_series(0, 4949) g`, wideWindowRepo); err != nil {
		t.Fatalf("seed var files: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO content_files
		(repo_id, relative_path, content, content_hash, line_count)
		SELECT $1, 'infra/m' || lpad(g::text, 3, '0') || '/main.tf',
		       'module "m' || g || E'" {\n  source = "./mod' || g || E'"\n}\n', 'h', 3
		FROM generate_series(0, 49) g`, wideWindowRepo); err != nil {
		t.Fatalf("seed Terraform files: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE content_files`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	contents := make(map[string]string, 5000)
	rows, err := db.QueryContext(ctx, `SELECT relative_path, content FROM content_files WHERE repo_id = $1`, wideWindowRepo)
	if err != nil {
		t.Fatalf("read seeded rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var relativePath, content string
		if err := rows.Scan(&relativePath, &content); err != nil {
			t.Fatalf("scan seeded row: %v", err)
		}
		contents[relativePath] = content
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("seeded rows: %v", err)
	}
	if len(contents) != contentartifacts.ListLimit {
		t.Fatalf("seeded %d files, want %d", len(contents), contentartifacts.ListLimit)
	}
	return ctx, db, contents
}

// explainPrepared explains statement prepared with types under plan mode, for
// the literal arguments args (EXPLAIN takes no bind values).
func explainPrepared(ctx context.Context, t *testing.T, db *sql.DB, mode, types, statement string, args ...string) string {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+mode); err != nil {
		t.Fatalf("set plan_cache_mode: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `PREPARE wide(`+types+`) AS `+statement); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.Contains(arg, "'") {
			t.Fatalf("argument %d holds a quote", i)
		}
		quoted[i] = "'" + arg + "'"
	}
	rows, err := conn.QueryContext(ctx, `EXPLAIN (COSTS OFF) EXECUTE wide(`+strings.Join(quoted, ", ")+`)`)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, line)
	}
	_ = rows.Close()
	// The pooled session outlives this call: leave it as found.
	if _, err := conn.ExecContext(ctx, `DEALLOCATE wide`); err != nil {
		t.Fatalf("deallocate: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `RESET plan_cache_mode`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	return strings.Join(plan, "\n")
}

func pathArrayLiteral(paths []string) string {
	quoted := make([]string, len(paths))
	for i, path := range paths {
		quoted[i] = `"` + path + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

func sortedPaths(contents map[string]string, suffix string) []string {
	paths := make([]string, 0, len(contents))
	for path := range contents {
		if strings.HasSuffix(path, suffix) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

// TestWideWindowChunkPlansUseAnIndexLive explains the two statements at the
// sizes production sends them on a full 5,000-file window: the blank check at
// BlankCheckChunkSize paths and the content read at ReadChunkSize paths, with
// a custom and a generic plan. Each must be an index scan on the repository
// and path key: no sequential scan and no bitmap heap scan. The plans at 1,171
// and 4,950 paths, which production no longer sends, are logged for the
// record.
func TestWideWindowChunkPlansUseAnIndexLive(t *testing.T) {
	ctx, db, contents := wideWindowFixture(t)
	varFiles := sortedPaths(contents, ".tfvars")
	tfFiles := sortedPaths(contents, ".tf")

	type planCase struct {
		name      string
		statement string
		types     string
		paths     []string
		require   bool
	}
	blank := func(n int) []string { return varFiles[:n] }
	cases := []planCase{
		{"blank check at the chunk size", contentartifacts.NonBlankPathsStatement, "text, text[], text", blank(contentartifacts.BlankCheckChunkSize), true},
		{"content read at the chunk size", contentartifacts.GetFilesStatement, "text, text[]", tfFiles[:contentartifacts.ReadChunkSize], true},
		{"blank check at 1,171 (not sent)", contentartifacts.NonBlankPathsStatement, "text, text[], text", blank(1171), false},
		{"blank check at 4,950 (not sent)", contentartifacts.NonBlankPathsStatement, "text, text[], text", blank(4950), false},
	}
	for _, pc := range cases {
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			args := []string{wideWindowRepo, pathArrayLiteral(pc.paths)}
			if strings.Contains(pc.types, "text, text[], text") {
				args = append(args, contentartifacts.SpaceCharacters())
			}
			plan := explainPrepared(ctx, t, db, mode, pc.types, pc.statement, args...)
			t.Logf("%s, %s, %d paths:\n%s", pc.name, mode, len(pc.paths), plan)
			if !pc.require {
				continue
			}
			if strings.Contains(plan, "Seq Scan") || strings.Contains(plan, "Bitmap Heap Scan") {
				t.Fatalf("%s %s: plan is not an index scan:\n%s", pc.name, mode, plan)
			}
			if !strings.Contains(plan, "Index Scan") {
				t.Fatalf("%s %s: no index scan in plan:\n%s", pc.name, mode, plan)
			}
			if !strings.Contains(plan, "content_files_repo_path_pattern_idx") && !strings.Contains(plan, "content_files_pkey") {
				t.Fatalf("%s %s: plan names neither the pattern index nor the primary key:\n%s", pc.name, mode, plan)
			}
		}
	}
}

// liveArtifactReader adapts the content reader to the neutral package's
// Reader, as the query package's loader adapter does, and records the size of
// every blank-check statement.
type liveArtifactReader struct {
	reader     *ContentReader
	blankSizes []int
}

func storedFileFromContent(file FileContent) contentartifacts.File {
	return contentartifacts.File{
		RepoID: file.RepoID, RelativePath: file.RelativePath, CommitSHA: file.CommitSHA,
		Content: file.Content, ContentHash: file.ContentHash, LineCount: file.LineCount,
		Language: file.Language, ArtifactType: file.ArtifactType,
	}
}

func (r *liveArtifactReader) ListFiles(ctx context.Context, repoID string, limit int) ([]contentartifacts.File, error) {
	rows, err := r.reader.ListRepoFiles(ctx, repoID, limit)
	files := make([]contentartifacts.File, len(rows))
	for i, row := range rows {
		files[i] = storedFileFromContent(row)
	}
	return files, err
}

func (r *liveArtifactReader) ReadFiles(ctx context.Context, repoID string, paths []string) ([]contentartifacts.File, error) {
	rows, err := r.reader.GetFileContents(ctx, repoID, paths)
	files := make([]contentartifacts.File, len(rows))
	for i, row := range rows {
		files[i] = storedFileFromContent(row)
	}
	return files, err
}

func (r *liveArtifactReader) NonBlankPaths(ctx context.Context, repoID string, paths []string) ([]string, error) {
	r.blankSizes = append(r.blankSizes, len(paths))
	return r.reader.NonBlankRepoFilePaths(ctx, repoID, paths)
}

// TestWideWindowReadsMatchSingleReadsLive proves the wide reads are exact: the
// batched read of 500 and 4,950 paths returns the rows of single-file reads,
// the non-blank statement at 500 and 4,950 paths agrees with
// strings.TrimSpace, and the whole-window load over the full 5,000-file
// repository issues ten blank-check chunks of at most 500 paths and returns
// the rows of the extractors over every non-blank file.
func TestWideWindowReadsMatchSingleReadsLive(t *testing.T) {
	ctx, db, contents := wideWindowFixture(t)
	reader := NewContentReader(db)
	all := sortedPaths(contents, "")

	for _, n := range []int{500, 4950} {
		paths := all[:n]
		got, err := reader.GetFileContents(ctx, wideWindowRepo, paths)
		if err != nil {
			t.Fatalf("GetFileContents(%d) error = %v", n, err)
		}
		want := make([]FileContent, 0, n)
		for _, path := range paths {
			single, err := reader.GetFileContent(ctx, wideWindowRepo, path)
			if err != nil {
				t.Fatalf("GetFileContent(%q) error = %v", path, err)
			}
			want = append(want, *single)
		}
		if !reflect.DeepEqual(sortedFiles(got), sortedFiles(want)) {
			t.Fatalf("%d paths: batched rows differ from single reads (got %d, want %d)", n, len(got), len(want))
		}

		nonBlank, err := reader.NonBlankRepoFilePaths(ctx, wideWindowRepo, paths)
		if err != nil {
			t.Fatalf("NonBlankRepoFilePaths(%d) error = %v", n, err)
		}
		sort.Strings(nonBlank)
		wantNonBlank := make([]string, 0, n)
		for _, path := range paths {
			if strings.TrimSpace(contents[path]) != "" {
				wantNonBlank = append(wantNonBlank, path)
			}
		}
		if !reflect.DeepEqual(nonBlank, wantNonBlank) {
			t.Fatalf("%d paths: non-blank set of %d differs from strings.TrimSpace (%d)", n, len(nonBlank), len(wantNonBlank))
		}
	}

	live := &liveArtifactReader{reader: reader}
	rows, stats, err := contentartifacts.LoadConfigPaths(ctx, live,
		contentartifacts.Repository{ID: wideWindowRepo, Name: "svc"}, nil, false, contentartifacts.AllowanceWholeWindow)
	if err != nil {
		t.Fatalf("whole-window load error = %v", err)
	}
	if stats.ListTruncated || stats.CandidateCount != 5000 || stats.CandidatesHydrated != 5000 {
		t.Fatalf("stats = truncated %v, candidates %d, hydrated %d; want a complete window of 5,000", stats.ListTruncated, stats.CandidateCount, stats.CandidatesHydrated)
	}
	if want := []int{500, 500, 500, 500, 500, 500, 500, 500, 500, 450}; !reflect.DeepEqual(live.blankSizes, want) || stats.BlankCheckStatements != len(want) {
		t.Fatalf("blank-check statements = %v (stats %d), want %v", live.blankSizes, stats.BlankCheckStatements, want)
	}
	nonBlankFiles := make([]contentartifacts.File, 0, len(all))
	for _, path := range all {
		if strings.TrimSpace(contents[path]) != "" {
			nonBlankFiles = append(nonBlankFiles, contentartifacts.File{RepoID: wideWindowRepo, RelativePath: path, Content: contents[path]})
		}
	}
	want := contentartifacts.ConfigPathRows(contentartifacts.BuildConfigPaths("svc", nonBlankFiles))
	if len(want) < 4000 {
		t.Fatalf("expected rows = %d; the fixture does not exercise the var-file rows", len(want))
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("whole-window load rows (%d) differ from the extractors over every non-blank file (%d)", len(rows), len(want))
	}
}
