// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ListRepoFilesByLanguage returns indexed files for one repository whose language
// is in the provided set (already alias-expanded and lowercased by the caller)
// and that fall under pathPrefix (a directory subtree, or "" for the whole repo),
// ordered by path and capped at limit. Pushing BOTH the language and the path
// predicate into the read means the cap applies to the matching+scoped set, so a
// language (or a deep subdirectory) whose files sort beyond the repository file
// cap is still returned for large repositories — the scale fix the in-Go post-cap
// filter could not provide. The language match is the bare normalized column
// (`language = ANY(...)`), identical to the by-language inventory reads, so it
// uses content_files_language_repo_idx; stored language is already normalized.
// An empty language set delegates to ListRepoFiles.
func (cr *ContentReader) ListRepoFilesByLanguage(ctx context.Context, repoID string, languages []string, pathPrefix string, limit int) ([]FileContent, error) {
	if len(languages) == 0 {
		return cr.ListRepoFiles(ctx, repoID, limit)
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_files_by_language"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 500
	}

	// strpos(relative_path, $3 || '/') = 1 is a literal prefix test (no LIKE
	// wildcards); combined with relative_path = $3 it scopes to the requested
	// subtree before the LIMIT, so a path with matching files beyond the cap in
	// the full repository is still returned.
	rows, err := cr.db.QueryContext(ctx, `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       '', content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1
		  AND language = ANY($2::text[])
		  AND ($3 = '' OR relative_path = $3 OR strpos(relative_path, $3 || '/') = 1)
		ORDER BY relative_path
		LIMIT $4
	`, repoID, array.Of(languages), pathPrefix, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo files by language: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContent
	for rows.Next() {
		var f FileContent
		if err := rows.Scan(&f.RepoID, &f.RelativePath, &f.CommitSHA,
			&f.Content, &f.ContentHash, &f.LineCount, &f.Language, &f.ArtifactType); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repo file by language: %w", err)
		}
		results = append(results, f)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// RepoFilePathContext reports whether requestPath exists as a directory prefix
// in the repository's indexed file set and the repository's indexed
// commit ref, both computed UNFILTERED by language in a single query. The tree
// handler uses it on the language-filtered path so a real directory with zero
// files in the requested language still returns an empty listing (not a 404) and
// still reports the repository ref, even though the language listing itself is
// filtered and capped in the database. An empty requestPath always exists (the
// repository root).
func (cr *ContentReader) RepoFilePathContext(ctx context.Context, repoID, requestPath string) (bool, string, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "repo_file_path_context"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	// strpos(relative_path, $2 || '/') = 1 is a literal prefix test (no LIKE
	// wildcards), so a path containing % or _ cannot widen the match. The ref
	// subquery is ORDER BY relative_path so it returns the same commit_sha as the
	// fallback repositoryTreeRef (first file by path), keeping the two paths'
	// `ref` deterministic and identical.
	var (
		exists bool
		ref    string
	)
	err := cr.db.QueryRowContext(ctx, `
		SELECT
		  EXISTS (
		    SELECT 1 FROM content_files
		    WHERE repo_id = $1
		      AND ($2 = '' OR strpos(relative_path, $2 || '/') = 1)
		  ),
		  coalesce((
		    SELECT commit_sha FROM content_files
		    WHERE repo_id = $1 AND commit_sha IS NOT NULL AND commit_sha <> ''
		    ORDER BY relative_path
		    LIMIT 1
		  ), '')
	`, repoID, requestPath).Scan(&exists, &ref)
	if err != nil {
		span.RecordError(err)
		return false, "", fmt.Errorf("repo file path context: %w", err)
	}
	return exists, ref, nil
}

// ListRepoFilesByPath returns indexed files strictly beneath one directory,
// ordered by relative path and capped after the path predicate. An empty path
// delegates to the whole-repository listing. The path is escaped as a literal
// LIKE prefix so %, _, and ! in file names do not widen the subtree.
func (cr *ContentReader) ListRepoFilesByPath(ctx context.Context, repoID, pathPrefix string, limit int) ([]FileContent, error) {
	if pathPrefix == "" {
		return cr.ListRepoFiles(ctx, repoID, limit)
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_files_by_path"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 500
	}
	pattern := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(pathPrefix) + "/%"
	rows, err := cr.db.QueryContext(ctx, `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       '', content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1 AND relative_path LIKE $2 ESCAPE '!'
		ORDER BY relative_path
		LIMIT $3
	`, repoID, pattern, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo files by path: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContent
	for rows.Next() {
		var file FileContent
		if err := rows.Scan(&file.RepoID, &file.RelativePath, &file.CommitSHA,
			&file.Content, &file.ContentHash, &file.LineCount, &file.Language, &file.ArtifactType); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repo file by path: %w", err)
		}
		results = append(results, file)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

var (
	goSpaceCharactersOnce sync.Once
	goSpaceCharacters     string
)

// goSpaceCharacterSet returns every rune for which unicode.IsSpace holds, the
// exact set strings.TrimSpace trims. It is derived by enumeration so the SQL
// predicate below cannot drift from the Go standard library: a Unicode table
// update in a later Go release changes the set with the toolchain.
func goSpaceCharacterSet() string {
	goSpaceCharactersOnce.Do(func() {
		var builder strings.Builder
		for r := rune(0); r <= unicode.MaxRune; r++ {
			if unicode.IsSpace(r) {
				builder.WriteRune(r)
			}
		}
		goSpaceCharacters = builder.String()
	})
	return goSpaceCharacters
}

// NonBlankRepoFilePaths returns the paths among relativePaths whose content is
// not blank under strings.TrimSpace, without moving any content bytes (#7126).
//
// The statement trims with an explicit character set instead of the default
// btrim, which trims only U+0020, and instead of a regular expression class,
// whose whitespace meaning follows the database locale. The set is every rune
// unicode.IsSpace accepts, so content is blank in SQL exactly when
// strings.TrimSpace(content) is empty in Go. A byte-order mark (U+FEFF) and
// U+200B are not in the set, as in Go: a file holding only one is non-blank.
// The set holds multibyte characters, so the statement assumes a UTF8 database
// (a SQL_ASCII database trims byte-wise; LATIN1 errors on non-Latin content).
func (cr *ContentReader) NonBlankRepoFilePaths(ctx context.Context, repoID string, relativePaths []string) ([]string, error) {
	if len(relativePaths) == 0 {
		return nil, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "non_blank_repo_file_paths"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	rows, err := cr.db.QueryContext(ctx, `
		SELECT relative_path
		FROM content_files
		WHERE repo_id = $1
		  AND relative_path = ANY($2::text[])
		  AND btrim(content, $3::text) <> ''
	`, repoID, array.StringArray(relativePaths), goSpaceCharacterSet())
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("non-blank repo file paths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	nonBlank := make([]string, 0, len(relativePaths))
	for rows.Next() {
		var relativePath string
		if err := rows.Scan(&relativePath); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan non-blank repo file path: %w", err)
		}
		nonBlank = append(nonBlank, relativePath)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("non-blank repo file paths: %w", err)
	}
	return nonBlank, nil
}

// getFileContentsStatement is the one statement GetFileContents issues. It is a
// constant so the live plan test explains the text the reader sends, and keeps
// the column list of GetFileContent so both scan into the same FileContent.
const getFileContentsStatement = `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       content, content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1 AND relative_path = ANY($2::text[])
	`

// GetFileContents returns the stored rows for relativePaths in one statement
// (#7126). Each row is selected with the columns of GetFileContent and scanned
// the same way, so a caller that read N files with N GetFileContent calls
// reads the same rows with one call. The predicate is `relative_path =
// ANY($2)`: equality against the array, not LIKE, so a path with %, _, a
// backslash, a quote or a space matches only itself. The (repo_id,
// relative_path) primary key leaves at most one row per path, which the
// content_files_repo_path_pattern_idx index serves without a sequential scan.
//
// The input is deduplicated on exact string value (no trimming: a padded path
// is a different path), and an empty input issues no statement. A path with no
// stored row is absent from the result, as GetFileContent returns no row for
// it. Row order is unspecified; callers key rows by RelativePath. An error from
// the query, a scan or the row iteration returns no rows.
func (cr *ContentReader) GetFileContents(ctx context.Context, repoID string, relativePaths []string) ([]FileContent, error) {
	if len(relativePaths) == 0 {
		return nil, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_file_contents"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	paths := make([]string, 0, len(relativePaths))
	seen := make(map[string]struct{}, len(relativePaths))
	for _, relativePath := range relativePaths {
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}
		paths = append(paths, relativePath)
	}

	rows, err := cr.db.QueryContext(ctx, getFileContentsStatement, repoID, array.StringArray(paths))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get file contents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	files := make([]FileContent, 0, len(paths))
	for rows.Next() {
		var f FileContent
		if err := rows.Scan(&f.RepoID, &f.RelativePath, &f.CommitSHA,
			&f.Content, &f.ContentHash, &f.LineCount, &f.Language, &f.ArtifactType); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan file contents: %w", err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get file contents: %w", err)
	}
	return files, nil
}
