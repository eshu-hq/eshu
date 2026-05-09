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
func (cr *ContentReader) SearchEntitiesByLanguageAndType(
	ctx context.Context,
	repoID, language, entityType, query string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
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
	filters, args, nextArg := buildLanguageTypeEntityFilters(repoID, languageVariants, entityType, query)
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

func buildLanguageTypeEntityFilters(
	repoID string,
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
