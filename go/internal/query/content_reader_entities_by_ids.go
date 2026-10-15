// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
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

// ListEntityMetadataByIDsForAccess reads the content rows for a bounded set of
// entity ids with the caller's repository scope bound in the same WHERE
// (#7247). It is the language-query enrichment's id-anchored read: the ids are
// the graph rows' own entity ids, so this replaces a second, differently
// ordered content page that shared few rows with the graph page.
//
// entity_id is the table's primary key, so the read returns at most one row
// per id, never more than taxonomy.MaxLanguageEntityIDLookup rows, and needs no
// ORDER BY or LIMIT: the caller merges by id. It projects no source_cache, the
// widest column, because the caller merges metadata only. An empty id set
// issues no statement; an oversized one is refused before any statement.
func (cr *ContentReader) ListEntityMetadataByIDsForAccess(
	ctx context.Context,
	lookup taxonomy.LanguageEntityIDLookup,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(ctx, "postgres.query", trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", "list_entity_metadata_by_ids"),
		attribute.String("db.sql.table", "content_entities"),
	))
	defer span.End()

	ids := uniqueStrings(lookup.EntityIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > taxonomy.MaxLanguageEntityIDLookup {
		err := fmt.Errorf("list entity metadata by ids: %d ids exceeds %d", len(ids), taxonomy.MaxLanguageEntityIDLookup)
		span.RecordError(err)
		return nil, err
	}
	span.SetAttributes(attribute.Int("eshu.content.entity_ids", len(ids)))

	statement, args := buildEntityMetadataByIDsQuery(ids, lookup)
	rows, err := cr.db.QueryContext(ctx, statement, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list entity metadata by ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]EntityContent, 0, len(ids))
	for rows.Next() {
		var entity EntityContent
		var rawMetadata []byte
		if err := rows.Scan(
			&entity.EntityID, &entity.RepoID, &entity.RelativePath, &entity.EntityType,
			&entity.EntityName, &entity.StartLine, &entity.EndLine, &entity.Language, &rawMetadata,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity metadata by id: %w", err)
		}
		if entity.Metadata, err = decodeEntityMetadata(rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity metadata by id: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list entity metadata by ids: %w", err)
	}
	return results, nil
}

// buildEntityMetadataByIDsQuery renders the id-anchored metadata read. A
// non-empty RepoID binds the one repository; otherwise the caller's grant is
// bound through appendRepositoryGrantFilter, the same filter the language
// page read uses; both empty leaves the read to the id set alone (unscoped
// callers).
func buildEntityMetadataByIDsQuery(ids []string, lookup taxonomy.LanguageEntityIDLookup) (string, []any) {
	filters := []string{"entity_id = ANY($1)"}
	args := []any{array.Of(ids)}
	if lookup.RepoID != "" {
		filters = append(filters, "repo_id = $2")
		args = append(args, lookup.RepoID)
	} else {
		filters, args, _ = appendRepositoryGrantFilter(filters, args, 2, lookup.AllowedRepositoryIDs)
	}
	// #nosec G201 -- filters are constant text plus $N placeholders; every
	// value is a bind argument.
	statement := fmt.Sprintf(`
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), metadata
		FROM content_entities
		WHERE %s
	`, strings.Join(filters, " AND "))
	return statement, args
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
