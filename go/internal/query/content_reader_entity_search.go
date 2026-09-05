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

// SearchEntitiesByLanguageAndType returns materialized content entities for one
// repo/language/entity-type filter using entity names as the primary lookup.
//
// It is the unscoped form of the read: no caller grant is bound, so a request
// with an empty repoID scans every repository. Scoped callers must go through
// SearchEntitiesByLanguageAndTypeForAccess instead.
func (cr *ContentReader) SearchEntitiesByLanguageAndType(
	ctx context.Context,
	repoID, language, entityType, query string,
	limit int,
) ([]EntityContent, error) {
	return cr.SearchEntitiesByLanguageAndTypeForAccess(ctx, languageEntitySearch{
		RepoID:     repoID,
		Language:   language,
		EntityType: entityType,
		Query:      query,
		Limit:      limit,
	})
}

// SearchEntitiesByLanguageAndTypeForAccess is the grant-bound form: a
// corpus-wide search (empty RepoID) carries the caller's granted repository ids
// into the statement's own WHERE, so the LIMIT page is taken from the granted
// set rather than from a cross-tenant-polluted one (#5167 W3 P1
// filter-before-limit). An empty grant list leaves the scan unrestricted, which
// is what the unscoped shared, admin, and local callers want; a grantless
// SCOPED caller is failed closed by codeContentGrantScope before it gets here.
func (cr *ContentReader) SearchEntitiesByLanguageAndTypeForAccess(
	ctx context.Context,
	search languageEntitySearch,
) ([]EntityContent, error) {
	repoID, language, entityType, query, limit := search.RepoID, search.Language, search.EntityType, search.Query, search.Limit
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entities_by_language_and_type"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	languageVariants := normalizedLanguageVariants(language)
	filters, args, nextArg := buildLanguageTypeEntityFilters(repoID, search.AllowedRepositoryIDs, languageVariants, entityType, query)
	// #nosec G201 -- interpolates only $N placeholder strings from buildLanguageTypeEntityFilters and an integer arg index; no user data concatenated into SQL
	sqlQuery := fmt.Sprintf(`
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE %s
		ORDER BY relative_path, start_line, entity_name
		LIMIT $%d
	`, strings.Join(filters, " AND "), nextArg)
	args = append(args, limit)

	rows, err := cr.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entities by language and type: %w", err)
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
			return nil, fmt.Errorf("scan language/type entity result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan language/type entity result: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}

	return results, nil
}

// buildLanguageTypeEntityFilters is the one SQL choke point behind every
// content read POST /api/v0/code/language-query makes: the content-only entity
// types, the graphless and zero-row fallbacks, and the metadata enrichment pass
// all reach the store through it.
//
// The `if repoID != ""` branch below used to have no else, so a scoped caller
// who omitted repo_id got a statement with no repository restriction at all and
// read the whole corpus. allowedRepositoryIDs closes that: it binds the
// caller's grant in the same WHERE, ahead of the ORDER BY and LIMIT, through
// the same appendRepositoryGrantFilter the four batch-1 content builders emit.
func buildLanguageTypeEntityFilters(
	repoID string,
	allowedRepositoryIDs []string,
	languageVariants []string,
	entityType string,
	query string,
) ([]string, []any, int) {
	filters := make([]string, 0, 4)
	args := make([]any, 0, 4)
	nextArg := 1
	if entityType != "" {
		filter, filterArgs, next := contentEntityTypeFilter(entityType, nextArg)
		filters = append(filters, filter)
		args = append(args, filterArgs...)
		nextArg = next
	}
	if repoID != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", nextArg))
		args = append(args, repoID)
		nextArg++
	} else {
		filters, args, nextArg = appendRepositoryGrantFilter(filters, args, nextArg, allowedRepositoryIDs)
	}
	if len(languageVariants) > 0 {
		parts := make([]string, 0, len(languageVariants))
		for _, variant := range languageVariants {
			parts = append(parts, fmt.Sprintf("language = $%d", nextArg))
			args = append(args, variant)
			nextArg++
		}
		filters = append(filters, "("+strings.Join(parts, " OR ")+")")
	}
	if query != "" {
		filters = append(filters, fmt.Sprintf("entity_name ILIKE $%d", nextArg))
		args = append(args, "%"+query+"%")
		nextArg++
	}
	return filters, args, nextArg
}
