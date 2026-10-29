// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SearchDockerfileInstallEntities reads a bounded exact-name page of persisted
// DockerfileInstall entities. The cursor is the last returned row's full sort key.
// repoIDs is nil only for an authorized unscoped request.
func (cr *ContentReader) SearchDockerfileInstallEntities(
	ctx context.Context, name string, repoIDs []string,
	afterRepoID, afterPath string, afterLine int, afterID string, limit int,
) ([]querycontract.EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query", trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", "search_dockerfile_install"),
		attribute.String("db.sql.table", "content_entities"),
	))
	defer span.End()
	if limit <= 0 || limit > 51 {
		limit = 51
	}
	query := `
SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
       start_line, end_line, metadata
FROM content_entities
WHERE entity_type = 'DockerfileInstall'
  AND entity_name = $1
  AND (repo_id, relative_path, start_line, entity_id) > ($2, $3, $4, $5)
ORDER BY repo_id, relative_path, start_line, entity_id
LIMIT $6`
	args := []any{name, afterRepoID, afterPath, afterLine, afterID, limit}
	if repoIDs != nil {
		query = strings.Replace(query, "ORDER BY repo_id,", "  AND repo_id = ANY($7)\nORDER BY repo_id,", 1)
		args = append(args, array.StringArray(repoIDs))
	}
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search Dockerfile install entities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]querycontract.EntityContent, 0)
	for rows.Next() {
		var row querycontract.EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&row.EntityID, &row.RepoID, &row.RelativePath, &row.EntityType, &row.EntityName, &row.StartLine, &row.EndLine, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan Dockerfile install entity: %w", err)
		}
		row.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("decode Dockerfile install metadata: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("read Dockerfile install entities: %w", err)
	}
	return result, nil
}
