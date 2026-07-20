// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ListRepoEntitiesByIDs returns the wide EntityContent rows for a bounded set
// of entity IDs in one repository. It is the hydration half of the impact-trace
// directed SELECTS scan (#5363): after the narrow ListRepoK8sSelectCandidates
// scan decides which Services actually selector-match the traced Deployment
// (typically 0-5, hard-capped by serviceStoryItemLimit), only those matched IDs
// are re-fetched here through the same wide column shape and metadata decode as
// every other surfaced K8sResource row, so the wire-row construction stays on
// the one tested path. Rows are ordered deterministically for a stable result.
func (cr *ContentReader) ListRepoEntitiesByIDs(
	ctx context.Context,
	repoID string,
	entityIDs []string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_entities_by_ids"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	ids := uniqueStrings(entityIDs)
	if len(ids) == 0 {
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
		  AND entity_id = ANY($2)
		ORDER BY relative_path, start_line, entity_id
		LIMIT $3
	`, repoID, pq.Array(ids), limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo entities by ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEntityContentRows(rows, span, "scan repo entity by id")
}
