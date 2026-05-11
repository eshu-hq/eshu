package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// DeadCodeCandidateRows returns label-scoped cleanup candidates from the
// content read model, preserving the graph candidate response shape.
func (cr *ContentReader) DeadCodeCandidateRows(
	ctx context.Context,
	repoID string,
	label string,
	language string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	repoID = strings.TrimSpace(repoID)
	language = strings.ToLower(strings.TrimSpace(language))
	if cr == nil || cr.db == nil || repoID == "" {
		return nil, nil
	}
	entityType, ok := deadCodeCandidateEntityType(label)
	if !ok {
		entityType = "Function"
	}
	if limit <= 0 {
		limit = deadCodeCandidateQueryMin
	}
	if offset < 0 {
		offset = 0
	}

	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "dead_code_candidate_rows"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, entity_name, entity_type, repo_id, relative_path,
		       coalesce(language, ''), start_line, end_line, metadata
		FROM content_entities
		WHERE repo_id = $1
		  AND entity_type = $2
		  AND ($3 = '' OR lower(coalesce(language, '')) = $3)
		ORDER BY relative_path, entity_name, entity_id
		LIMIT $4 OFFSET $5
	`, repoID, entityType, language, limit, offset)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("dead code candidate rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]map[string]any, 0, limit)
	for rows.Next() {
		var entityID string
		var entityName string
		var entityType string
		var rowRepoID string
		var relativePath string
		var language string
		var startLine int
		var endLine int
		var rawMetadata []byte
		if err := rows.Scan(
			&entityID,
			&entityName,
			&entityType,
			&rowRepoID,
			&relativePath,
			&language,
			&startLine,
			&endLine,
			&rawMetadata,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan dead code candidate row: %w", err)
		}
		metadata, err := decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("dead code candidate rows: %w", err)
		}
		result := map[string]any{
			"entity_id":  entityID,
			"name":       entityName,
			"labels":     []any{label},
			"file_path":  relativePath,
			"repo_id":    rowRepoID,
			"language":   language,
			"start_line": startLine,
			"end_line":   endLine,
		}
		if len(metadata) > 0 {
			result["metadata"] = metadata
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return results, nil
}

func deadCodeCandidateEntityType(label string) (string, bool) {
	switch label {
	case "Function", "Class", "Struct", "Interface", "SqlFunction":
		return label, true
	default:
		return "", false
	}
}
