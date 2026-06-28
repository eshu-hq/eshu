// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	"github.com/lib/pq"
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
	`, repoID, pq.Array(languages), pathPrefix, limit)
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

// RepoFilePathContext reports whether requestPath exists in the repository's
// indexed file set (as a file or a directory prefix) and the repository's indexed
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
		      AND ($2 = '' OR relative_path = $2 OR strpos(relative_path, $2 || '/') = 1)
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
