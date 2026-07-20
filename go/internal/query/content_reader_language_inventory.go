// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// CountRepositoriesByLanguage returns aggregate repository/file coverage for a
// normalized language family. See the ContentStore interface doc comment
// (ports.go) for the #5167 access-scoping contract.
func (cr *ContentReader) CountRepositoriesByLanguage(
	ctx context.Context,
	languages []string,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (RepositoryLanguageAggregate, error) {
	if cr == nil || cr.db == nil || len(languages) == 0 {
		return RepositoryLanguageAggregate{}, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "count_repositories_by_language"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	query := `
		SELECT COUNT(DISTINCT repo_id) AS repository_count,
		       COUNT(*) AS file_count,
		       MAX(indexed_at) AS last_indexed_at
		FROM content_files
		WHERE language = ANY($1)`
	args := []any{pq.Array(languages)}
	if !allScopes {
		query += " AND (repo_id = ANY($2) OR repo_id = ANY($3))"
		args = append(args, pq.Array(allowedRepositoryIDs), pq.Array(allowedScopeIDs))
	}
	row := cr.db.QueryRowContext(ctx, query, args...)

	var aggregate RepositoryLanguageAggregate
	var repositoryCount, fileCount int64
	var lastIndexedAt sql.NullTime
	if err := row.Scan(&repositoryCount, &fileCount, &lastIndexedAt); err != nil {
		span.RecordError(err)
		return RepositoryLanguageAggregate{}, fmt.Errorf("count repositories by language: %w", err)
	}
	aggregate.RepositoryCount = int(repositoryCount)
	aggregate.FileCount = int(fileCount)
	if lastIndexedAt.Valid {
		aggregate.LastIndexedAt = lastIndexedAt.Time
	}
	return aggregate, nil
}

// ListRepositoriesByLanguage returns a bounded page of repositories that contain
// at least one file in the normalized language family. See the ContentStore
// interface doc comment (ports.go) for the #5167 access-scoping contract.
func (cr *ContentReader) ListRepositoriesByLanguage(
	ctx context.Context,
	languages []string,
	limit int,
	offset int,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]RepositoryLanguageRepository, error) {
	if cr == nil || cr.db == nil || len(languages) == 0 || limit <= 0 {
		return nil, nil
	}
	if offset < 0 {
		offset = 0
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repositories_by_language"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	languageRowsWhere := "WHERE language = ANY($1)"
	args := []any{pq.Array(languages)}
	if !allScopes {
		languageRowsWhere += " AND (repo_id = ANY($4) OR repo_id = ANY($5))"
	}
	args = append(args, limit, offset)
	if !allScopes {
		args = append(args, pq.Array(allowedRepositoryIDs), pq.Array(allowedScopeIDs))
	}

	rows, err := cr.db.QueryContext(ctx, `
		WITH catalog AS (
			SELECT `+repositoryCatalogIDExpr+` AS id,
			       coalesce(payload->>'name', payload->>'repo_name', payload->>'repo_slug', scope_id) AS name,
			       coalesce(payload->>'path', '') AS path,
			       coalesce(payload->>'local_path', payload->>'path', '') AS local_path,
			       coalesce(payload->>'remote_url', '') AS remote_url,
			       coalesce(payload->>'repo_slug', '') AS repo_slug,
			       CASE WHEN coalesce(payload->>'remote_url', '') <> '' THEN true ELSE false END AS has_remote
			FROM ingestion_scopes
			WHERE scope_kind = 'repository'
		),
		language_rows AS (
			SELECT repo_id,
			       coalesce(NULLIF(language, ''), 'unknown') AS language,
			       COUNT(*) AS file_count,
			       MAX(indexed_at) AS last_indexed_at
			FROM content_files
			`+languageRowsWhere+`
			GROUP BY repo_id, language
		),
		repo_totals AS (
			SELECT repo_id,
			       SUM(file_count) AS total_file_count,
			       MAX(last_indexed_at) AS last_indexed_at
			FROM language_rows
			GROUP BY repo_id
		),
		page AS (
			SELECT rt.repo_id,
			       coalesce(c.name, rt.repo_id) AS repo_name,
			       coalesce(c.path, '') AS path,
			       coalesce(c.local_path, '') AS local_path,
			       coalesce(c.remote_url, '') AS remote_url,
			       coalesce(c.repo_slug, '') AS repo_slug,
			       coalesce(c.has_remote, false) AS has_remote,
			       rt.total_file_count,
			       rt.last_indexed_at
			FROM repo_totals rt
			LEFT JOIN catalog c ON c.id = rt.repo_id
			ORDER BY total_file_count DESC, repo_name, repo_id
			LIMIT $2 OFFSET $3
		)
		SELECT page.repo_id, page.repo_name, page.path, page.local_path,
		       page.remote_url, page.repo_slug, page.has_remote,
		       language_rows.language, language_rows.file_count, page.last_indexed_at
		FROM page
		JOIN language_rows ON language_rows.repo_id = page.repo_id
		ORDER BY page.total_file_count DESC, page.repo_name, page.repo_id, language_rows.language
	`, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repositories by language: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byRepo := map[string]int{}
	repos := make([]RepositoryLanguageRepository, 0)
	for rows.Next() {
		var repo RepositoryCatalogEntry
		var language RepositoryLanguageCount
		var fileCount int64
		var indexedAt sql.NullTime
		if err := rows.Scan(
			&repo.ID,
			&repo.Name,
			&repo.Path,
			&repo.LocalPath,
			&repo.RemoteURL,
			&repo.RepoSlug,
			&repo.HasRemote,
			&language.Language,
			&fileCount,
			&indexedAt,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repository language row: %w", err)
		}
		idx, ok := byRepo[repo.ID]
		if !ok {
			repos = append(repos, RepositoryLanguageRepository{Repository: repo})
			idx = len(repos) - 1
			byRepo[repo.ID] = idx
		}
		language.FileCount = int(fileCount)
		repos[idx].Languages = append(repos[idx].Languages, language)
		repos[idx].FileCount += language.FileCount
		if indexedAt.Valid {
			repos[idx].IndexedAt = maxTime(repos[idx].IndexedAt, indexedAt.Time)
		}
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("iterate repository language rows: %w", err)
	}
	return repos, nil
}

// RepositoryLanguageInventory returns aggregate coverage for every indexed
// language bucket. See the ContentStore interface doc comment (ports.go) for
// the #5167 access-scoping contract.
func (cr *ContentReader) RepositoryLanguageInventory(
	ctx context.Context,
	limit int,
	offset int,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]RepositoryLanguageInventoryRow, error) {
	if cr == nil || cr.db == nil || limit <= 0 {
		return nil, nil
	}
	if offset < 0 {
		offset = 0
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "repository_language_inventory"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	where := ""
	args := []any{}
	if !allScopes {
		where = "WHERE (repo_id = ANY($3) OR repo_id = ANY($4))\n\t\t"
	}
	args = append(args, limit, offset)
	if !allScopes {
		args = append(args, pq.Array(allowedRepositoryIDs), pq.Array(allowedScopeIDs))
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT coalesce(NULLIF(language, ''), 'unknown') AS language,
		       COUNT(DISTINCT repo_id) AS repository_count,
		       COUNT(*) AS file_count,
		       MAX(indexed_at) AS last_indexed_at
		FROM content_files
		`+where+`GROUP BY coalesce(NULLIF(language, ''), 'unknown')
		ORDER BY repository_count DESC, file_count DESC, language
		LIMIT $1 OFFSET $2
	`, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("repository language inventory: %w", err)
	}
	defer func() { _ = rows.Close() }()

	inventory := make([]RepositoryLanguageInventoryRow, 0)
	for rows.Next() {
		var row RepositoryLanguageInventoryRow
		var repositoryCount, fileCount int64
		var lastIndexedAt sql.NullTime
		if err := rows.Scan(&row.Language, &repositoryCount, &fileCount, &lastIndexedAt); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repository language inventory row: %w", err)
		}
		row.Language = strings.TrimSpace(row.Language)
		row.RepositoryCount = int(repositoryCount)
		row.FileCount = int(fileCount)
		if lastIndexedAt.Valid {
			row.LastIndexedAt = lastIndexedAt.Time
		}
		inventory = append(inventory, row)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("iterate repository language inventory rows: %w", err)
	}
	return inventory, nil
}
