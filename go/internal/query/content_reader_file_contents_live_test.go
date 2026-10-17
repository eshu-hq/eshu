// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7126 disposable PostgreSQL 18 proof that the batched file-content statement returns the rows N single-file reads return (ASCII, multibyte, empty, blank, 2 MB, paths with spaces, quotes, percent, underscore, backslash and non-ASCII, absent, duplicated and cross-repository paths, array sizes 0, 1, 10 and all stored) and that its plan uses an index scan on a small and a 50-file repository under a generic and a custom plan; one local PostgreSQL 18 run took about 1.5 s
//eshu:readiness:env ESHU_SCOPED_SEARCH_PROOF_DISPOSABLE ESHU_SCOPED_SEARCH_PROOF_DSN

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
// that the batched read of #7126 returns exactly the rows N GetFileContent
// calls return, and that its plan uses an index on a small and a 50-file
// repository.

const fileContentsLiveRepo = "repo-batched"

type fileContentsLiveRow struct {
	relativePath string
	content      string
	commitSHA    any
	language     any
	artifactType any
}

func insertFileContentsRow(ctx context.Context, t *testing.T, db *sql.DB, repoID string, row fileContentsLiveRow) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO content_files
		(repo_id, relative_path, commit_sha, content, content_hash, line_count, language, artifact_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		repoID, row.relativePath, row.commitSHA, row.content, "hash-"+row.relativePath,
		strings.Count(row.content, "\n")+1, row.language, row.artifactType); err != nil {
		t.Fatalf("insert %q: %v", row.relativePath, err)
	}
}

// fileContentsLiveRows holds the content and path classes the extractors and
// the story can meet. Paths cover characters that would break a LIKE, a
// regular expression or an unquoted array literal.
func fileContentsLiveRows() []fileContentsLiveRow {
	rows := []fileContentsLiveRow{
		{relativePath: "plain.yaml", content: "a: b\n", commitSHA: "c0ffee", language: "yaml", artifactType: "config"},
		{relativePath: "multibyte.tf", content: "# ü-é 日本語 🙂\nvariable \"x\" {}\n", commitSHA: nil, language: "hcl", artifactType: nil},
		{relativePath: "empty.json", content: "", commitSHA: "", language: nil, artifactType: nil},
		{relativePath: "blank.tfvars", content: " \n\t\n", commitSHA: nil, language: nil, artifactType: nil},
		{relativePath: "two-mb.tf", content: strings.Repeat("resource \"a\" \"b\" {}\n", 100_000), commitSHA: "big", language: "hcl", artifactType: nil},
		{relativePath: "a b/with space.yaml", content: "s: 1\n"},
		{relativePath: `q"uote/it's.yaml`, content: "q: 1\n"},
		{relativePath: "pct%/under_score.yaml", content: "p: 1\n"},
		{relativePath: `back\slash/x.yaml`, content: "b: 1\n"},
		{relativePath: "ü-é/日本語.yaml", content: "u: 1\n"},
		{relativePath: "comma,brace{}/x.yaml", content: "c: 1\n"},
		{relativePath: " padded.yaml ", content: "pad: 1\n"},
		{relativePath: "padded.yaml", content: "nopad: 1\n"},
		{relativePath: "Case.YAML", content: "upper: 1\n"},
		{relativePath: "case.yaml", content: "lower: 1\n"},
		{relativePath: "./dot/lead.yaml", content: "d: 1\n"},
		{relativePath: "NULL", content: "word NULL as a path\n"},
		{relativePath: "100%", content: "full percent\n"},
		{relativePath: "a_c.yaml", content: "under\n"},
	}
	return rows
}

func sortedFiles(files []FileContent) []FileContent {
	out := append([]FileContent(nil), files...)
	sort.Slice(out, func(i, j int) bool { return out[i].RelativePath < out[j].RelativePath })
	return out
}

// TestGetFileContentsMatchesSingleReadsLive compares one batched read with the
// per-path reads it replaces, field by field, for array sizes 0, 1, 10 and
// every stored path, with absent, duplicated and cross-repository paths mixed
// in.
func TestGetFileContentsMatchesSingleReadsLive(t *testing.T) {
	ctx, db := openNonBlankLiveFixture(t)
	reader := NewContentReader(db)

	stored := fileContentsLiveRows()
	for _, row := range stored {
		insertFileContentsRow(ctx, t, db, fileContentsLiveRepo, row)
	}
	// A row of the same path in another repository must never come back.
	insertFileContentsRow(ctx, t, db, "repo-other", fileContentsLiveRow{relativePath: "plain.yaml", content: "other repository\n"})
	insertFileContentsRow(ctx, t, db, "repo-other", fileContentsLiveRow{relativePath: "only-other.yaml", content: "x\n"})

	allPaths := make([]string, 0, len(stored))
	for _, row := range stored {
		allPaths = append(allPaths, row.relativePath)
	}
	sets := map[string][]string{
		"1 path":                {"plain.yaml"},
		"1 absent path":         {"never-stored.yaml"},
		"10 paths":              allPaths[:10],
		"every stored path":     allPaths,
		"special characters":    {"a b/with space.yaml", `q"uote/it's.yaml`, "pct%/under_score.yaml", `back\slash/x.yaml`, "ü-é/日本語.yaml", "comma,brace{}/x.yaml"},
		"padded is distinct":    {" padded.yaml ", "padded.yaml"},
		"case is distinct":      {"Case.YAML", "case.yaml"},
		"only the padded":       {" padded.yaml "},
		"absent among present":  {"plain.yaml", "never-stored.yaml", "multibyte.tf", "only-other.yaml"},
		"duplicates":            {"plain.yaml", "plain.yaml", "empty.json", "plain.yaml", "empty.json"},
		"the word NULL":         {"NULL", "plain.yaml"},
		"pattern characters":    {"%", "_", "%.yaml", "pct%", "a_c.yaml", "a%c.yaml"},
		"leading dot slash":     {"./dot/lead.yaml", "dot/lead.yaml"},
		"large file and others": {"two-mb.tf", "plain.yaml", "empty.json"},
	}
	for name, paths := range sets {
		name, paths := name, paths
		t.Run(name, func(t *testing.T) {
			want := make([]FileContent, 0, len(paths))
			seen := map[string]bool{}
			for _, path := range paths {
				if seen[path] {
					continue
				}
				seen[path] = true
				single, err := reader.GetFileContent(ctx, fileContentsLiveRepo, path)
				if err != nil {
					t.Fatalf("GetFileContent(%q) error = %v", path, err)
				}
				if single != nil {
					want = append(want, *single)
				}
			}
			got, err := reader.GetFileContents(ctx, fileContentsLiveRepo, paths)
			if err != nil {
				t.Fatalf("GetFileContents() error = %v", err)
			}
			if !reflect.DeepEqual(sortedFiles(got), sortedFiles(want)) {
				t.Fatalf("batched rows differ from %d single reads:\n  got  %d rows\n  want %d rows", len(want), len(got), len(want))
			}
			for _, file := range got {
				if file.RepoID != fileContentsLiveRepo {
					t.Fatalf("row %q carries repo %q, want %q", file.RelativePath, file.RepoID, fileContentsLiveRepo)
				}
			}
		})
	}

	t.Run("empty input issues no statement", func(t *testing.T) {
		files, err := reader.GetFileContents(ctx, fileContentsLiveRepo, nil)
		if err != nil || len(files) != 0 {
			t.Fatalf("nil input = %v, %v", files, err)
		}
		files, err = reader.GetFileContents(ctx, fileContentsLiveRepo, []string{})
		if err != nil || len(files) != 0 {
			t.Fatalf("empty input = %v, %v", files, err)
		}
	})

	t.Run("array literal with a NULL element and with no element", func(t *testing.T) {
		// The reader cannot send a NULL element: a Go string is never NULL. The
		// statement itself must still be safe against one, so it is run raw.
		for literal, wantRows := range map[string]int{`{"plain.yaml",NULL}`: 1, `{NULL}`: 0, `{}`: 0} {
			rows, err := db.QueryContext(ctx, contentartifacts.GetFilesStatement, fileContentsLiveRepo, literal)
			if err != nil {
				t.Fatalf("statement with %s: %v", literal, err)
			}
			n := 0
			for rows.Next() {
				n++
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("rows with %s: %v", literal, err)
			}
			_ = rows.Close()
			if n != wantRows {
				t.Fatalf("statement with %s returned %d rows, want %d", literal, n, wantRows)
			}
		}
	})
}

// TestGetFileContentsPlanUsesAnIndexLive explains the statement the reader
// sends, as a generic plan with the array unknown (the plan a prepared driver
// statement may reuse) and with literal arrays, on a repository of 8 files and
// on one of 50, among 12,000 rows of other repositories. It requires an index
// scan on the repository and path key and no sequential scan.
func TestGetFileContentsPlanUsesAnIndexLive(t *testing.T) {
	ctx, db := openNonBlankLiveFixture(t)

	// The pattern index the production schema adds in migration 137.
	if _, err := db.ExecContext(ctx, `CREATE INDEX content_files_repo_path_pattern_idx
		ON content_files (repo_id, relative_path text_pattern_ops)`); err != nil {
		t.Fatalf("create pattern index: %v", err)
	}
	for r := 0; r < 300; r++ {
		repo := fmt.Sprintf("repo-fill-%03d", r)
		if _, err := db.ExecContext(ctx, `INSERT INTO content_files
			(repo_id, relative_path, content, content_hash, line_count)
			SELECT $1, 'dir/file-' || g || '.yaml', repeat('k: v' || g || E'\n', 40), 'h', 40
			FROM generate_series(1, 40) g`, repo); err != nil {
			t.Fatalf("seed filler: %v", err)
		}
	}
	for repo, count := range map[string]int{"repo-small": 8, "repo-fifty": 50} {
		for i := 0; i < count; i++ {
			insertFileContentsRow(ctx, t, db, repo, fileContentsLiveRow{
				relativePath: fmt.Sprintf("deploy/m%03d/main.tf", i), content: "module \"m\" {}\n",
			})
		}
	}
	if _, err := db.ExecContext(ctx, `ANALYZE content_files`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	type planCase struct {
		repo  string
		paths int
	}
	for _, pc := range []planCase{{"repo-small", 8}, {"repo-fifty", 10}, {"repo-fifty", 50}} {
		repo := pc.repo
		literal := make([]string, 0, pc.paths)
		for i := 0; i < pc.paths; i++ {
			literal = append(literal, fmt.Sprintf(`"deploy/m%03d/main.tf"`, i))
		}
		array := "{" + strings.Join(literal, ",") + "}"
		for mode, setup := range map[string]string{
			"custom plan":  "SET plan_cache_mode = force_custom_plan",
			"generic plan": "SET plan_cache_mode = force_generic_plan",
		} {
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatalf("conn: %v", err)
			}
			if _, err := conn.ExecContext(ctx, setup); err != nil {
				t.Fatalf("%s: %v", setup, err)
			}
			if _, err := conn.ExecContext(ctx, `PREPARE gfc(text, text[]) AS `+contentartifacts.GetFilesStatement); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			// EXPLAIN is a utility statement and takes no bind values, so the
			// repository and the array are literals. No path here holds a
			// quote.
			rows, err := conn.QueryContext(ctx, fmt.Sprintf(`EXPLAIN (COSTS OFF) EXECUTE gfc('%s', '%s')`, repo, array))
			if err != nil {
				t.Fatalf("%s explain: %v", mode, err)
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
			// The pooled session outlives this block: leave it as found.
			if _, err := conn.ExecContext(ctx, `DEALLOCATE gfc`); err != nil {
				t.Fatalf("deallocate: %v", err)
			}
			if _, err := conn.ExecContext(ctx, `RESET plan_cache_mode`); err != nil {
				t.Fatalf("reset: %v", err)
			}
			_ = conn.Close()
			text := strings.Join(plan, "\n")
			shown := text
			if len(shown) > 400 {
				shown = shown[:400] + "..."
			}
			t.Logf("%s, %s, %d paths:\n%s", repo, mode, pc.paths, shown)
			if strings.Contains(text, "Seq Scan") {
				t.Fatalf("%s %s: sequential scan in plan:\n%s", repo, mode, text)
			}
			if !strings.Contains(text, "Index Scan") && !strings.Contains(text, "Bitmap Index Scan") {
				t.Fatalf("%s %s: no index scan in plan:\n%s", repo, mode, text)
			}
			if !strings.Contains(text, "content_files_repo_path_pattern_idx") && !strings.Contains(text, "content_files_pkey") {
				t.Fatalf("%s %s: plan names neither the pattern index nor the primary key:\n%s", repo, mode, text)
			}
		}
	}
}
