// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// testExtractorVersion is the extractor version the live derive proofs run.
const testExtractorVersion = 7

// newLiveDeriver returns a Deriver over db with both start-up checks passed.
func newLiveDeriver(t *testing.T, db *sql.DB, extractorVersion int) *artifacts.Deriver {
	t.Helper()
	deriver, err := artifacts.NewDeriver(postgres.SQLDB{DB: db}, artifacts.DeriverConfig{
		ExtractorVersion: extractorVersion,
		Encoding:         artifacts.Encoding{ServerEncoding: "UTF8", Supported: true},
		Fence:            artifacts.Fence{Supported: true},
	})
	if err != nil {
		t.Fatalf("NewDeriver() error = %v", err)
	}
	return deriver
}

// putFile upserts one content_files row in its own transaction, so the fence
// bumps the repository's version at its COMMIT.
func putFile(ctx context.Context, t *testing.T, db *sql.DB, repoID, relativePath, body string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
VALUES ($1, $2, $3, md5($3), 1, NULL, now())
ON CONFLICT (repo_id, relative_path) DO UPDATE
SET content = EXCLUDED.content, content_hash = EXCLUDED.content_hash`, repoID, relativePath, body); err != nil {
		t.Fatalf("put %s:%s: %v", repoID, relativePath, err)
	}
}

// putFiles writes a repository's files in one transaction.
func putFiles(ctx context.Context, t *testing.T, db *sql.DB, repoID string, files []contentartifacts.File) {
	t.Helper()
	paths := make([]string, 0, len(files))
	bodies := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.RelativePath)
		bodies = append(bodies, file.Content)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT $1, p, b, md5(b), 1, NULL, now()
FROM unnest($2::text[], $3::text[]) AS f(p, b)`, repoID, pgTextArray(paths), pgTextArray(bodies)); err != nil {
		t.Fatalf("put files of %s: %v", repoID, err)
	}
}

// pgTextArray renders a text[] literal.
func pgTextArray(values []string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, value := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// storedRows returns the stored rows of a repository in a stable order.
func storedRows(ctx context.Context, t *testing.T, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, repoID string,
) []artifacts.Row {
	t.Helper()
	rows, err := q.QueryContext(ctx, `
SELECT raw_path, relative_path, evidence_kind, name_scoped FROM config_artifact_paths
WHERE repo_id = $1 ORDER BY raw_path COLLATE "C", relative_path COLLATE "C", evidence_kind`, repoID)
	if err != nil {
		t.Fatalf("read stored rows of %s: %v", repoID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []artifacts.Row
	for rows.Next() {
		var row artifacts.Row
		if err := rows.Scan(&row.RawPath, &row.RelativePath, &row.EvidenceKind, &row.NameScoped); err != nil {
			t.Fatalf("scan stored row: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read stored rows of %s: %v", repoID, err)
	}
	return out
}

// liveState is a repository's state row, nil fields for NULL.
type liveState struct {
	ContentVersion   *int64
	ExtractorVersion int
	RowCount         *int
	FailureCount     int
	LastErrorClass   *string
	FilesListed      int
}

// stateOf returns the state row of a repository, or nil.
func stateOf(ctx context.Context, t *testing.T, db *sql.DB, repoID string) *liveState {
	t.Helper()
	var state liveState
	err := db.QueryRowContext(ctx, `
SELECT content_version, extractor_version, row_count, failure_count, last_error_class, files_listed
FROM config_artifact_repo_state WHERE repo_id = $1`, repoID).Scan(
		&state.ContentVersion, &state.ExtractorVersion, &state.RowCount, &state.FailureCount,
		&state.LastErrorClass, &state.FilesListed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("read state of %s: %v", repoID, err)
	}
	return &state
}

// applyName is the read-time name step of protocol section 4 over one
// repository's stored rows: strip the name from name-scoped rows only, set
// source_repo, then the one dedup and sort of the merge.
func applyName(rows []artifacts.Row, name string) []map[string]any {
	built := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		rowPath := row.RawPath
		if row.NameScoped {
			rowPath = contentartifacts.StripRepositoryName(row.RawPath, name)
		}
		built = append(built, map[string]any{
			"path": rowPath, "source_repo": name,
			"relative_path": row.RelativePath, "evidence_kind": row.EvidenceKind,
		})
	}
	return contentartifacts.MergeConfigPaths(built)
}

// rowKeys renders rows as comparable keys.
func rowKeys(rows []map[string]any) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, fmt.Sprintf("%v\x1f%v\x1f%v\x1f%v", row["path"], row["source_repo"], row["relative_path"], row["evidence_kind"]))
	}
	return keys
}

// missingFrom counts the keys of want absent from got, as a multiset.
func missingFrom(want, got []string) []string {
	left := map[string]int{}
	for _, key := range got {
		left[key]++
	}
	var missing []string
	for _, key := range want {
		if left[key] == 0 {
			missing = append(missing, key)
			continue
		}
		left[key]--
	}
	return missing
}

// liveQueryReader adapts the query package's ContentReader, the live path's
// content store, to the neutral reader port, as repositoryartifacts does.
type liveQueryReader struct{ reader *query.ContentReader }

func (r liveQueryReader) ListFiles(ctx context.Context, repoID string, limit int) ([]contentartifacts.File, error) {
	rows, err := r.reader.ListRepoFiles(ctx, repoID, limit)
	return contractFiles(rows), err
}

func (r liveQueryReader) ReadFiles(ctx context.Context, repoID string, relativePaths []string) ([]contentartifacts.File, error) {
	rows, err := r.reader.GetFileContents(ctx, repoID, relativePaths)
	return contractFiles(rows), err
}

func (r liveQueryReader) NonBlankPaths(ctx context.Context, repoID string, relativePaths []string) ([]string, error) {
	return r.reader.NonBlankRepoFilePaths(ctx, repoID, relativePaths)
}

func contractFiles(rows []query.FileContent) []contentartifacts.File {
	files := make([]contentartifacts.File, 0, len(rows))
	for _, row := range rows {
		files = append(files, contentartifacts.File{
			RepoID: row.RepoID, RelativePath: row.RelativePath, CommitSHA: row.CommitSHA,
			Content: row.Content, ContentHash: row.ContentHash, LineCount: row.LineCount,
			Language: row.Language, ArtifactType: row.ArtifactType,
		})
	}
	return files
}

// fixtureRepository is one golden fixture directory loaded as a repository.
type fixtureRepository struct {
	ID    string
	Name  string
	Files []contentartifacts.File
}

// fixtureRepositories reads tests/fixtures the way the S0 name-free proof
// does: every fixture directory and each child directory is one repository.
// Every text file up to 256 KiB is kept (not only candidates), so the list,
// the selection and the blank check run as they do on real content. A file
// that is not valid UTF-8 or holds a NUL cannot be a content_files row.
func fixtureRepositories(t *testing.T) []fixtureRepository {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..", "..", "tests", "fixtures")
	var repos []fixtureRepository
	add := func(dir string) {
		var files []contentartifacts.File
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(dir, p)
			content, readErr := os.ReadFile(p)
			if relErr != nil || readErr != nil || len(content) > 256<<10 || !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
				return nil
			}
			files = append(files, contentartifacts.File{RelativePath: filepath.ToSlash(rel), Content: string(content)})
			return nil
		})
		if len(files) == 0 {
			return
		}
		relDir, _ := filepath.Rel(root, dir)
		repos = append(repos, fixtureRepository{ID: "fx:" + filepath.ToSlash(relDir), Name: path.Base(filepath.ToSlash(dir)), Files: files})
	}
	top, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	for _, entry := range top {
		if !entry.IsDir() {
			continue
		}
		add(filepath.Join(root, entry.Name()))
		children, _ := os.ReadDir(filepath.Join(root, entry.Name()))
		for _, child := range children {
			if child.IsDir() {
				add(filepath.Join(root, entry.Name(), child.Name()))
			}
		}
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].ID < repos[j].ID })
	return repos
}

// newLiveDeriverDatabase is the production pool adapter over db.
func newLiveDeriverDatabase(db *sql.DB) artifacts.Database {
	return postgres.SQLDB{DB: db}
}
