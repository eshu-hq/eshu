// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ListRepoEntitiesByType returns indexed entities for one repository filtered
// to a single content entity_type. The limit applies to the TYPE-FILTERED row
// set (that narrower budget is the point: a repo-wide ListRepoEntities can push
// late-sorting rows of a rare type past its LIMIT and silently drop them).
//
// ORDER BY carries entity_id as a tiebreaker after relative_path, start_line
// so a truncated fetch (see fetchK8sResourceCandidates in
// content_relationships.go) drops a reproducible, deterministic set of rows
// instead of leaving which rows land past LIMIT to Postgres's unspecified
// tiebreak order among rows with equal relative_path/start_line.
func (cr *ContentReader) ListRepoEntitiesByType(ctx context.Context, repoID, entityType string, limit int) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_entities_by_type"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 500
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE repo_id = $1 AND entity_type = $2
		ORDER BY relative_path, start_line, entity_id
		LIMIT $3
	`, repoID, entityType, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo entities by type: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEntityContentRows(rows, span, "scan repo entity by type")
}

// scanEntityContentRows scans content_entities query rows (entity_id,
// repo_id, relative_path, entity_type, entity_name, start_line, end_line,
// language, source_cache, metadata, in that column order) into EntityContent
// values, decoding each row's metadata JSONB. The caller owns rows.Close().
// On a scan or decode error, span.RecordError is invoked and the returned
// error is wrapped with errContext so callers keep a distinct message per
// query site. Shared by ListRepoEntities and ListRepoEntitiesByType, which
// scan an identical column shape.
func scanEntityContentRows(rows *sql.Rows, span trace.Span, errContext string) ([]EntityContent, error) {
	var results []EntityContent
	for rows.Next() {
		var e EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
			&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("%s: %w", errContext, err)
		}
		metadata, err := decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("%s: %w", errContext, err)
		}
		e.Metadata = metadata
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}
