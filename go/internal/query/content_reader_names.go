// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SearchFileContentAnyRepo searches file content by pattern across all repos.
func (cr *ContentReader) SearchFileContentAnyRepo(
	ctx context.Context,
	pattern string,
	limit int,
) ([]FileContent, error) {
	return cr.searchFileContentAnyRepo(ctx, pattern, limit, false)
}

// SearchFileContentAnyRepoExactCase searches file content by exact-case
// substring across all repos. Use it for normalized tokens such as lowercased
// hostnames where case-insensitive matching is unnecessary and measurably more
// expensive on large corpora.
func (cr *ContentReader) SearchFileContentAnyRepoExactCase(
	ctx context.Context,
	pattern string,
	limit int,
) ([]FileContent, error) {
	return cr.searchFileContentAnyRepo(ctx, pattern, limit, true)
}

// SearchFileReferenceAnyRepo searches normalized file-reference indexes across
// repositories and reports whether the reference table is available.
func (cr *ContentReader) SearchFileReferenceAnyRepo(
	ctx context.Context,
	kind string,
	value string,
	limit int,
) ([]FileContent, bool, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_file_reference_any_repo"),
			attribute.String("db.sql.table", "content_file_references"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	var available bool
	err := cr.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM content_file_references
			WHERE reference_kind = $1
			LIMIT 1
		)
	`, kind).Scan(&available)
	if err != nil {
		if contentReferenceIndexUnavailable(err) {
			return nil, false, nil
		}
		span.RecordError(err)
		return nil, false, fmt.Errorf("check content file reference index: %w", err)
	}
	if !available {
		return nil, false, nil
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT f.repo_id, f.relative_path, coalesce(f.commit_sha, ''),
		       '', f.content_hash, f.line_count, coalesce(f.language, ''),
		       coalesce(f.artifact_type, '')
		FROM content_file_references ref
		JOIN content_files f
		  ON f.repo_id = ref.repo_id
		 AND f.relative_path = ref.relative_path
		WHERE ref.reference_kind = $1
		  AND ref.reference_value = $2
		ORDER BY f.repo_id, f.relative_path
		LIMIT $3
	`, kind, value, limit)
	if err != nil {
		if contentReferenceIndexUnavailable(err) {
			return nil, false, nil
		}
		span.RecordError(err)
		return nil, true, fmt.Errorf("search content file references across repos: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContent
	for rows.Next() {
		var file FileContent
		if err := rows.Scan(
			&file.RepoID,
			&file.RelativePath,
			&file.CommitSHA,
			&file.Content,
			&file.ContentHash,
			&file.LineCount,
			&file.Language,
			&file.ArtifactType,
		); err != nil {
			span.RecordError(err)
			return nil, true, fmt.Errorf("scan cross-repo file reference result: %w", err)
		}
		results = append(results, file)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, true, err
	}
	return results, true, nil
}

func contentReferenceIndexUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "content_file_references") &&
		(strings.Contains(message, "does not exist") ||
			strings.Contains(message, "undefined_table") ||
			strings.Contains(message, "42p01"))
}

func (cr *ContentReader) searchFileContentAnyRepo(
	ctx context.Context,
	pattern string,
	limit int,
	exactCase bool,
) ([]FileContent, error) {
	operation := "search_file_content_any_repo"
	operator := "ILIKE"
	if exactCase {
		operation = "search_file_content_any_repo_exact_case"
		operator = "LIKE"
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", operation),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	query := fmt.Sprintf(`
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       '', content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE eshu_require_content_substring_indexes_ready()
		  AND content %s '%%' || $1 || '%%'
		ORDER BY repo_id, relative_path
		LIMIT $2
	`, operator)
	rows, err := cr.db.QueryContext(ctx, query, pattern, limit)
	if err != nil {
		err = contentSubstringIndexReadError(err)
		span.RecordError(err)
		return nil, fmt.Errorf("search file content across repos: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContent
	for rows.Next() {
		var file FileContent
		if err := rows.Scan(
			&file.RepoID,
			&file.RelativePath,
			&file.CommitSHA,
			&file.Content,
			&file.ContentHash,
			&file.LineCount,
			&file.Language,
			&file.ArtifactType,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan cross-repo file search result: %w", err)
		}
		results = append(results, file)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// SearchEntitiesByNameAnyRepo searches content entities by name across all repos.
func (cr *ContentReader) SearchEntitiesByNameAnyRepo(
	ctx context.Context,
	entityType string,
	name string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entities_by_name_any_repo"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE entity_name ILIKE '%' || $1 || '%'
	`
	args := []any{name}
	if entityType != "" {
		query += ` AND entity_type = $2`
		args = append(args, entityType)
		query += `
			ORDER BY repo_id, relative_path, start_line
			LIMIT $3
		`
		args = append(args, limit)
	} else {
		query += `
			ORDER BY repo_id, relative_path, start_line
			LIMIT $2
		`
		args = append(args, limit)
	}

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entities by name across repos: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var entity EntityContent
		var rawMetadata []byte
		if err := rows.Scan(
			&entity.EntityID,
			&entity.RepoID,
			&entity.RelativePath,
			&entity.EntityType,
			&entity.EntityName,
			&entity.StartLine,
			&entity.EndLine,
			&entity.Language,
			&entity.SourceCache,
			&rawMetadata,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity name result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity name result: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// SearchEntitiesByExactName returns the entities in one repository whose
// materialized name is exactly name, optionally restricted to one entity type.
//
// It is SearchEntitiesByName's twin with `entity_name = $1` in place of
// `entity_name ILIKE '%' || $1 || '%'`, for the callers that want the one
// symbol they named rather than everything containing it. See the port
// declaration in querycontract/ports.go for the defect that separates them
// (#6555).
//
// The predicate is not a new shape for this table: SearchSymbols
// (content_reader_symbol_search.go) already ships `entity_name = $1`, with and
// without a `repo_id = $n` companion, against this same index set.
func (cr *ContentReader) SearchEntitiesByExactName(
	ctx context.Context,
	repoID string,
	entityType string,
	name string,
	limit int,
) ([]EntityContent, error) {
	return cr.searchEntitiesByExactName(ctx, repoID, entityType, name, limit)
}

// SearchEntitiesByExactNameAnyRepo is the corpus-wide twin of
// SearchEntitiesByExactName, for the unscoped caller that names no repository.
func (cr *ContentReader) SearchEntitiesByExactNameAnyRepo(
	ctx context.Context,
	entityType string,
	name string,
	limit int,
) ([]EntityContent, error) {
	return cr.searchEntitiesByExactName(ctx, "", entityType, name, limit)
}

// searchEntitiesByExactName serves both exact-name reads. An empty repoID
// leaves the repository predicate off entirely, which is what makes the
// any-repo call corpus-wide; every caller that must stay inside a grant passes
// a repository the grant already resolved.
func (cr *ContentReader) searchEntitiesByExactName(
	ctx context.Context,
	repoID string,
	entityType string,
	name string,
	limit int,
) ([]EntityContent, error) {
	operation := "search_entities_by_exact_name"
	if repoID == "" {
		operation = "search_entities_by_exact_name_any_repo"
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", operation),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	filters := []string{"entity_name = $1"}
	args := []any{name}
	nextArg := 2
	if repoID != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", nextArg))
		args = append(args, repoID)
		nextArg++
	}
	if entityType != "" {
		filter, filterArgs, next := contentEntityTypeFilter(entityType, nextArg)
		filters = append(filters, filter)
		args = append(args, filterArgs...)
		nextArg = next
	}
	// #nosec G201 -- interpolates only predicate strings built above and integer arg indices ($N); no user data concatenated into SQL
	query := fmt.Sprintf(`
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE %s
		ORDER BY repo_id, relative_path, start_line
		LIMIT $%d
	`, strings.Join(filters, " AND "), nextArg)
	args = append(args, limit)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entities by exact name: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var entity EntityContent
		var rawMetadata []byte
		if err := rows.Scan(
			&entity.EntityID, &entity.RepoID, &entity.RelativePath, &entity.EntityType,
			&entity.EntityName, &entity.StartLine, &entity.EndLine, &entity.Language,
			&entity.SourceCache, &rawMetadata,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan exact entity name result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan exact entity name result: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// SearchEntityContentAnyRepo searches entity source cache by pattern across all repos.
func (cr *ContentReader) SearchEntityContentAnyRepo(
	ctx context.Context,
	pattern string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entity_content_any_repo"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE eshu_require_content_substring_indexes_ready()
		  AND source_cache ILIKE '%' || $1 || '%'
		ORDER BY repo_id, relative_path, start_line
		LIMIT $2
	`, pattern, limit)
	if err != nil {
		err = contentSubstringIndexReadError(err)
		span.RecordError(err)
		return nil, fmt.Errorf("search entity content across repos: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var entity EntityContent
		var rawMetadata []byte
		if err := rows.Scan(
			&entity.EntityID,
			&entity.RepoID,
			&entity.RelativePath,
			&entity.EntityType,
			&entity.EntityName,
			&entity.StartLine,
			&entity.EndLine,
			&entity.Language,
			&entity.SourceCache,
			&rawMetadata,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan cross-repo entity search result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan cross-repo entity search result: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}
