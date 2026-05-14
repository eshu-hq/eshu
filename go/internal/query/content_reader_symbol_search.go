package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (cr *ContentReader) searchSymbols(
	ctx context.Context,
	req symbolSearchRequest,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_symbols"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	filters, args, nextArg := symbolSearchFilters(req)
	query := fmt.Sprintf(`
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE %s
		ORDER BY repo_id, relative_path, start_line, entity_name
		LIMIT $%d OFFSET $%d
	`, strings.Join(filters, " AND "), nextArg, nextArg+1)
	args = append(args, req.Limit, req.Offset)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search symbols: %w", err)
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
			return nil, fmt.Errorf("scan symbol result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan symbol result: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

func symbolSearchFilters(req symbolSearchRequest) ([]string, []any, int) {
	filters := make([]string, 0, 4)
	args := make([]any, 0, 4)
	nextArg := 2

	if req.mustMatchMode() == "exact" {
		filters = append(filters, "entity_name = $1")
		args = append(args, req.symbol())
	} else {
		filters = append(filters, "entity_name ILIKE '%' || $1 || '%'")
		args = append(args, req.symbol())
	}

	if req.RepoID != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", nextArg))
		args = append(args, req.RepoID)
		nextArg++
	}
	if language := strings.TrimSpace(req.Language); language != "" {
		parts := make([]string, 0, len(normalizedLanguageVariants(language)))
		for _, variant := range normalizedLanguageVariants(language) {
			parts = append(parts, fmt.Sprintf("language = $%d", nextArg))
			args = append(args, variant)
			nextArg++
		}
		filters = append(filters, "("+strings.Join(parts, " OR ")+")")
	}
	if entityTypes := req.normalizedEntityTypes(); len(entityTypes) > 0 {
		parts := make([]string, 0, len(entityTypes))
		for _, entityType := range entityTypes {
			filter, filterArgs, next := contentEntityTypeFilter(entityType, nextArg)
			parts = append(parts, filter)
			args = append(args, filterArgs...)
			nextArg = next
		}
		filters = append(filters, "("+strings.Join(parts, " OR ")+")")
	}

	return filters, args, nextArg
}
