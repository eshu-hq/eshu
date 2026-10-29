// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// SearchPHPConfigEntities reads an ordered page of exact-name PHP evidence.
// A nil repoIDs searches the corpus; a nonempty list is the caller's grant.
func (cr *ContentReader) SearchPHPConfigEntities(ctx context.Context, repoIDs []string, name string, after *querycontract.PHPConfigCursor, limit int) ([]querycontract.EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query", trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", "search_php_config_entities"),
		attribute.String("db.sql.table", "content_entities"),
	))
	defer span.End()
	if limit < 1 || limit > 201 {
		return nil, fmt.Errorf("PHP configuration page limit must be 1..201")
	}
	filters := []string{"entity_type = 'Variable'", "entity_name = $1"}
	args := []any{name}
	if repoIDs != nil {
		if len(repoIDs) == 0 {
			return []querycontract.EntityContent{}, nil
		}
		filters = append(filters, fmt.Sprintf("repo_id = ANY($%d::text[])", len(args)+1))
		args = append(args, repoIDs)
	}
	if after != nil {
		base := len(args) + 1
		filters = append(filters, fmt.Sprintf("(repo_id, relative_path, start_line, entity_id) > ($%d, $%d, $%d, $%d)", base, base+1, base+2, base+3))
		args = append(args, after.RepoID, after.RelativePath, after.StartLine, after.EntityID)
	}
	args = append(args, limit)
	// #nosec G201 -- predicates are fixed strings and generated parameter indexes; user values are bound.
	statement := fmt.Sprintf(`SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		start_line, end_line, coalesce(language, ''), '' AS source_cache, metadata
		FROM content_entities
		WHERE %s
		ORDER BY repo_id, relative_path, start_line, entity_id
		LIMIT $%d`, strings.Join(filters, " AND "), len(args))
	rows, err := cr.db.QueryContext(ctx, statement, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search PHP configuration entities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]querycontract.EntityContent, 0, limit)
	for rows.Next() {
		var entity querycontract.EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&entity.EntityID, &entity.RepoID, &entity.RelativePath, &entity.EntityType, &entity.EntityName, &entity.StartLine, &entity.EndLine, &entity.Language, &entity.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan PHP configuration entity: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("decode PHP configuration entity: %w", err)
		}
		result = append(result, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("iterate PHP configuration entities: %w", err)
	}
	return result, nil
}
