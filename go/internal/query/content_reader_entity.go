package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// GetEntityContent returns one entity by entity_id.
func (cr *ContentReader) GetEntityContent(ctx context.Context, entityID string) (*EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_entity_content"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	row := cr.db.QueryRowContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE entity_id = $1
	`, entityID)

	var e EntityContent
	var rawMetadata []byte
	err := row.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
		&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get entity content: %w", err)
	}
	e.Metadata, err = decodeEntityMetadata(rawMetadata)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get entity content: %w", err)
	}
	return &e, nil
}

// GetEntityContents returns entities keyed by entity_id in one bounded query.
func (cr *ContentReader) GetEntityContents(ctx context.Context, entityIDs []string) (map[string]*EntityContent, error) {
	entityIDs = cleanEntityContentIDs(entityIDs)
	if cr == nil || cr.db == nil || len(entityIDs) == 0 {
		return map[string]*EntityContent{}, nil
	}

	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_entity_contents"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	placeholders := make([]string, 0, len(entityIDs))
	args := make([]any, 0, len(entityIDs))
	for i, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE entity_id IN (`+strings.Join(placeholders, ", ")+`)
	`, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get entity contents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entities := make(map[string]*EntityContent, len(entityIDs))
	for rows.Next() {
		var e EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
			&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity content: %w", err)
		}
		e.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("get entity contents: %w", err)
		}
		entities[e.EntityID] = &e
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return entities, nil
}

func cleanEntityContentIDs(entityIDs []string) []string {
	if len(entityIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(entityIDs))
	cleaned := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		cleaned = append(cleaned, entityID)
	}
	return cleaned
}
