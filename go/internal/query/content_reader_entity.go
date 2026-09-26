// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	entitycontract "github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// GetEntityContent returns one entity by entity_id.
func (cr *ContentReader) GetEntityContent(ctx context.Context, entityID string) (*EntityContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
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

// GetEntityContentInRepositories returns one entity only when its repository is
// in the already-authorized repository set.
func (cr *ContentReader) GetEntityContentInRepositories(
	ctx context.Context,
	entityID string,
	repoIDs []string,
) (*EntityContent, error) {
	entityID = strings.TrimSpace(entityID)
	repoIDs = cleanedAuthStrings(repoIDs)
	if cr == nil || cr.db == nil || entityID == "" || len(repoIDs) == 0 {
		return nil, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_entity_content_in_repositories"),
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
		  AND repo_id = ANY(string_to_array($2, E'\x1f'))
	`, entityID, strings.Join(repoIDs, "\x1f"))

	var e EntityContent
	var rawMetadata []byte
	err := row.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
		&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get scoped entity content: %w", err)
	}
	e.Metadata, err = decodeEntityMetadata(rawMetadata)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get scoped entity content: %w", err)
	}
	return &e, nil
}

// SearchEntitiesByNameInRepositories is SearchEntitiesByNameAnyRepo bound to an
// already-authorized repository set: the same name match, order and LIMIT, with
// `repo_id = ANY(...)` in the same WHERE so the LIMIT counts granted rows only
// (#5167). It is one statement however many repositories the grant holds -- a
// per-repository loop would cost one round trip per granted repository and
// would have to re-implement the caller's "exactly one match" rule across
// pages. An empty repoIDs reads nothing: the caller has no grant to search.
//
// The grant binds as a text[] parameter (`repo_id = ANY($2)`, the
// appendRepositoryGrantFilter shape) rather than the string_to_array form
// GetEntityContentInRepositories uses. pgx caches named statements, so
// PostgreSQL may switch to a generic plan; on 200k rows a generic plan for a
// common name under a 1000-repository grant measured ~31 ms with the array
// parameter and ~168 ms with string_to_array, which the generic plan
// re-evaluates on every heap recheck. Custom plans measure the same for both.
// See docs/internal/evidence/5167-code-relationships-grant.md.
func (cr *ContentReader) SearchEntitiesByNameInRepositories(
	ctx context.Context,
	repoIDs []string,
	entityType string,
	name string,
	limit int,
) ([]EntityContent, error) {
	repoIDs = cleanedAuthStrings(repoIDs)
	if cr == nil || cr.db == nil || len(repoIDs) == 0 {
		return nil, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entities_by_name_in_repositories"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE entity_name ILIKE '%' || $1 || '%'
		  AND repo_id = ANY($2)
	`
	args := []any{name, array.Of(repoIDs)}
	if entityType != "" {
		query += ` AND entity_type = $3
			ORDER BY repo_id, relative_path, start_line
			LIMIT $4
		`
		args = append(args, entityType, limit)
	} else {
		query += `
			ORDER BY repo_id, relative_path, start_line
			LIMIT $3
		`
		args = append(args, limit)
	}

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entities by name in repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var e EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
			&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan scoped entity name result: %w", err)
		}
		e.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan scoped entity name result: %w", err)
		}
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// GetEntityContents returns entities keyed by entity_id in one bounded query.
func (cr *ContentReader) GetEntityContents(ctx context.Context, entityIDs []string) (map[string]*EntityContent, error) {
	entityIDs = cleanEntityContentIDs(entityIDs)
	if cr == nil || cr.db == nil || len(entityIDs) == 0 {
		return map[string]*EntityContent{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
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
	// #nosec G202 -- concatenates only $N parameter placeholders (generated from loop indices) into the IN list; entity values are bound args, not SQL text
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

// GetEntityContentsInRepositories returns entities by ID with the repository
// authorization predicate bound in the SQL read itself.
func (cr *ContentReader) GetEntityContentsInRepositories(
	ctx context.Context,
	entityIDs []string,
	repoIDs []string,
) (map[string]*EntityContent, error) {
	entityIDs = cleanEntityContentIDs(entityIDs)
	repoIDs = cleanedAuthStrings(repoIDs)
	if cr == nil || cr.db == nil || len(entityIDs) == 0 || len(repoIDs) == 0 {
		return map[string]*EntityContent{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_entity_contents_in_repositories"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE entity_id = ANY(string_to_array($1, E'\x1f'))
		  AND repo_id = ANY(string_to_array($2, E'\x1f'))
	`, strings.Join(entityIDs, "\x1f"), strings.Join(repoIDs, "\x1f"))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get scoped entity contents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entities := make(map[string]*EntityContent, len(entityIDs))
	for rows.Next() {
		var e EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
			&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan scoped entity content: %w", err)
		}
		e.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("get scoped entity contents: %w", err)
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

// decodeEntityMetadata is the one metadata JSONB decode; it drops fingerprint keys (#7167).
func decodeEntityMetadata(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, fmt.Errorf("decode entity metadata: %w", err)
	}
	entitycontract.StripFingerprintMetadata(metadata)
	if len(metadata) == 0 {
		return nil, nil
	}
	return metadata, nil
}
