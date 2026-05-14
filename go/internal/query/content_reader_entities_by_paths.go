package query

import (
	"context"
	"fmt"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ListRepoEntitiesByPaths returns indexed entities for a bounded path set in
// one repository.
func (cr *ContentReader) ListRepoEntitiesByPaths(
	ctx context.Context,
	repoID string,
	relativePaths []string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_entities_by_paths"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	paths := uniqueStrings(relativePaths)
	if len(paths) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE repo_id = $1
		  AND relative_path = ANY($2)
		ORDER BY relative_path, start_line, entity_name, entity_id
		LIMIT $3
	`, repoID, pq.Array(paths), limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo entities by paths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]EntityContent, 0)
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
			return nil, fmt.Errorf("scan repo entity by path: %w", err)
		}
		var err error
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repo entity by path: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}
