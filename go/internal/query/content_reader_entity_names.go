// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	entityNameSearchMaxLimit   = 200
	entityNameSearchProbeLimit = entityNameSearchMaxLimit + 1
)

var (
	errEntityNameSearchUnavailable        = errors.New("global entity-name content index is unavailable")
	errGlobalGraphEntitySearchUnsupported = errors.New("global graph entity search is unsupported")
)

// EntityNameMatch controls the case-sensitive entity_name predicate.
type EntityNameMatch string

const (
	// EntityNameMatchExact requires a case-sensitive complete name match.
	EntityNameMatchExact EntityNameMatch = "exact"
	// EntityNameMatchSubstring requires a case-sensitive substring match.
	EntityNameMatchSubstring EntityNameMatch = "substring"
)

// EntityNameScope controls repository authorization for an entity-name search.
type EntityNameScope string

const (
	// EntityNameScopeAll searches every repository visible to an all-scopes caller.
	EntityNameScopeAll EntityNameScope = "all"
	// EntityNameScopeRepositories searches one explicit authorized repository set.
	EntityNameScopeRepositories EntityNameScope = "repositories"
)

// EntityNameSearch is the bounded, authorization-aware content name-search contract.
type EntityNameSearch struct {
	Name          string
	Match         EntityNameMatch
	Scope         EntityNameScope
	RepositoryIDs []string
	Languages     []string
	EntityType    string
	MetadataKey   string
	MetadataValue string
	Limit         int
}

// EntityNameSearcher is the narrow extension used by global entity-name routes.
type EntityNameSearcher interface {
	SearchEntityNames(context.Context, EntityNameSearch) ([]EntityContent, error)
}

// SearchEntityNames searches current content entities with every authorization
// and semantic filter applied before the bounded deterministic LIMIT.
func (cr *ContentReader) SearchEntityNames(ctx context.Context, search EntityNameSearch) ([]EntityContent, error) {
	search, empty, err := normalizeEntityNameSearch(search)
	if err != nil || empty {
		return []EntityContent{}, err
	}

	ctx, span := cr.tracer.Start(ctx, "postgres.query", trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", "search_entity_names"),
		attribute.String("db.sql.table", "content_entities"),
	))
	defer span.End()

	query, args := buildEntityNameSearchQuery(search)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		err = contentSubstringIndexReadError(err)
		span.RecordError(err)
		return nil, fmt.Errorf("search entity names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]EntityContent, 0)
	for rows.Next() {
		var entity EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&entity.EntityID, &entity.RepoID, &entity.RelativePath, &entity.EntityType,
			&entity.EntityName, &entity.StartLine, &entity.EndLine, &entity.Language, &entity.SourceCache, &rawMetadata,
			&entity.RepoName); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity name search result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("decode entity name search metadata: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

func buildEntityNameSearchQuery(search EntityNameSearch) (string, []any) {
	operator := "= $1"
	nameArg := search.Name
	if search.Match == EntityNameMatchSubstring {
		operator = `LIKE '%' || $1 || '%' ESCAPE '\'`
		nameArg = escapeEntityNameLikeLiteral(search.Name)
	}
	query := `
		WITH matched AS MATERIALIZED (
			SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
			       start_line, end_line, coalesce(language, '') AS language,
			       coalesce(source_cache, '') AS source_cache, metadata
			FROM content_entities
			WHERE eshu_require_content_substring_indexes_ready()
			  AND entity_name ` + operator
	args := []any{nameArg}
	if search.Scope == EntityNameScopeRepositories {
		args = append(args, pq.Array(search.RepositoryIDs))
		query += fmt.Sprintf(" AND repo_id = ANY($%d::text[])", len(args))
	}
	if len(search.Languages) > 0 {
		args = append(args, pq.Array(search.Languages))
		query += fmt.Sprintf(" AND coalesce(language, '') = ANY($%d::text[])", len(args))
	}
	if search.EntityType != "" {
		args = append(args, search.EntityType)
		query += fmt.Sprintf(" AND entity_type = $%d", len(args))
	}
	if search.MetadataKey != "" {
		args = append(args, search.MetadataValue)
		query += fmt.Sprintf(" AND coalesce(metadata ->> '%s', '') = $%d", search.MetadataKey, len(args))
	}
	args = append(args, search.Limit)
	query += fmt.Sprintf(`
			ORDER BY repo_id, relative_path, start_line, entity_name, entity_id
			LIMIT $%d
		),
		matched_repository_ids AS MATERIALIZED (
			SELECT DISTINCT repo_id
			FROM matched
		),
		repository_catalog AS MATERIALIZED (
			SELECT DISTINCT ON (coalesce(scope.payload->>'repo_id', scope.payload->>'id', scope.scope_id))
			       coalesce(scope.payload->>'repo_id', scope.payload->>'id', scope.scope_id) AS repo_id,
			       coalesce(scope.payload->>'name', scope.payload->>'repo_name',
			                scope.payload->>'repo_slug', scope.scope_id) AS repo_name
			FROM ingestion_scopes AS scope
			JOIN matched_repository_ids
			  ON matched_repository_ids.repo_id =
			     coalesce(scope.payload->>'repo_id', scope.payload->>'id', scope.scope_id)
			WHERE scope.scope_kind = 'repository'
			ORDER BY coalesce(scope.payload->>'repo_id', scope.payload->>'id', scope.scope_id),
			         scope.scope_id
		)
		SELECT matched.entity_id, matched.repo_id, matched.relative_path,
		       matched.entity_type, matched.entity_name, matched.start_line,
		       matched.end_line, matched.language, matched.source_cache,
		       matched.metadata, coalesce(repository.repo_name, '') AS repo_name
		FROM matched
		LEFT JOIN repository_catalog AS repository ON repository.repo_id = matched.repo_id
		ORDER BY matched.repo_id, matched.relative_path, matched.start_line,
		         matched.entity_name, matched.entity_id
	`, len(args))
	return query, args
}

func normalizeEntityNameSearch(search EntityNameSearch) (EntityNameSearch, bool, error) {
	search.Name = strings.TrimSpace(search.Name)
	if search.Name == "" {
		return search, false, errors.New("entity name is required")
	}
	if search.Match != EntityNameMatchExact && search.Match != EntityNameMatchSubstring {
		return search, false, fmt.Errorf("invalid entity name match mode %q", search.Match)
	}
	if search.Scope != EntityNameScopeAll && search.Scope != EntityNameScopeRepositories {
		return search, false, fmt.Errorf("invalid entity name scope %q", search.Scope)
	}
	if search.Limit <= 0 || search.Limit > entityNameSearchProbeLimit {
		return search, false, fmt.Errorf("entity name search limit must be between 1 and %d", entityNameSearchProbeLimit)
	}
	search.RepositoryIDs = sortedUniqueNonEmptyStrings(search.RepositoryIDs)
	if search.Scope == EntityNameScopeAll && len(search.RepositoryIDs) > 0 {
		return search, false, errors.New("all-repository entity name scope rejects repository IDs")
	}
	if search.Scope == EntityNameScopeRepositories && len(search.RepositoryIDs) == 0 {
		return search, true, nil
	}
	languageVariants := make([]string, 0, len(search.Languages))
	for _, language := range search.Languages {
		if strings.TrimSpace(language) != "" {
			languageVariants = append(languageVariants, normalizedLanguageVariants(language)...)
		}
	}
	search.Languages = sortedUniqueNonEmptyStrings(languageVariants)
	if err := validateEntityNameMetadataFilter(search.MetadataKey, search.MetadataValue); err != nil {
		return search, false, err
	}
	return search, false, nil
}

func escapeEntityNameLikeLiteral(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func validateEntityNameMetadataFilter(key, value string) error {
	if key == "" && value == "" {
		return nil
	}
	allowed := map[string]string{
		"semantic_kind":  "guard",
		"module_kind":    "protocol_implementation",
		"attribute_kind": "module_attribute",
	}
	if allowed[key] != value {
		return fmt.Errorf("invalid entity name metadata filter %q=%q", key, value)
	}
	return nil
}

func sortedUniqueNonEmptyStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
