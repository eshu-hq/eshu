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
	`, repoID, array.Of(ids), limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo entities by ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEntityContentRows(rows, span, "scan repo entity by id")
}

// ListRepoEntitiesByKeys reads at most two content rows for each unique exact
// path, type, name, and line key in one repository. Two rows expose ambiguity
// without allowing a busy key to starve later keys from the result page.
func (cr *ContentReader) ListRepoEntitiesByKeys(
	ctx context.Context,
	repoID string,
	keys []querycontract.EntityContentKey,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query", trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", "list_repo_entities_by_keys"),
		attribute.String("db.sql.table", "content_entities"),
	))
	defer span.End()

	unique := make([]querycontract.EntityContentKey, 0, len(keys))
	seen := make(map[querycontract.EntityContentKey]struct{}, len(keys))
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
		if len(unique) > querycontract.MaxEntityContentKeys {
			err := fmt.Errorf("list repo entities by keys: exceeds %d unique keys", querycontract.MaxEntityContentKeys)
			span.RecordError(err)
			return nil, err
		}
	}
	if len(unique) == 0 {
		return nil, nil
	}

	var statement strings.Builder
	statement.WriteString(`
		WITH keys(relative_path, entity_type, entity_name, start_line, key_order) AS (
			VALUES `)
	args := make([]any, 0, 1+4*len(unique))
	args = append(args, repoID)
	for i, key := range unique {
		if i > 0 {
			statement.WriteString(", ")
		}
		base := 2 + 4*i
		fmt.Fprintf(&statement, "($%d::text, $%d::text, $%d::text, $%d::integer, %d)", base, base+1, base+2, base+3, i)
		args = append(args, key.RelativePath, key.EntityType, key.EntityName, key.StartLine)
	}
	statement.WriteString(`
		)
		SELECT hit.entity_id, hit.repo_id, hit.relative_path, hit.entity_type,
		       hit.entity_name, hit.start_line, hit.end_line, hit.language,
		       hit.source_cache, hit.metadata
		FROM keys
		JOIN LATERAL (
			SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
			       start_line, end_line, coalesce(language, '') AS language,
			       coalesce(source_cache, '') AS source_cache, metadata
			FROM content_entities
			WHERE repo_id = $1
			  AND relative_path = keys.relative_path
			  AND entity_type = keys.entity_type
			  AND entity_name = keys.entity_name
			  AND start_line = keys.start_line
			ORDER BY entity_id
			LIMIT 2
		) AS hit ON TRUE
		ORDER BY keys.key_order, hit.entity_id
	`)

	rows, err := cr.db.QueryContext(ctx, statement.String(), args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo entities by keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanEntityContentRows(rows, span, "scan repo entity by key")
}
