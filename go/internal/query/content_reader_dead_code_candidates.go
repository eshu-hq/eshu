// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// DeadCodeCandidateRows returns label-scoped cleanup candidates from the
// content read model, preserving the graph candidate response shape.
func (cr *ContentReader) DeadCodeCandidateRows(
	ctx context.Context,
	query deadCodeCandidateQuery,
) ([]map[string]any, error) {
	repoID := strings.TrimSpace(query.RepoID)
	language := strings.ToLower(strings.TrimSpace(query.Language))
	label := query.Label
	limit, offset := query.Limit, query.Offset
	if cr == nil || cr.db == nil {
		return nil, nil
	}
	entityType, ok := deadCodeCandidateEntityType(label)
	if !ok {
		return nil, fmt.Errorf("unsupported dead code candidate label %q", label)
	}
	if limit <= 0 {
		limit = deadCodeCandidateQueryMin
	}
	if offset < 0 {
		offset = 0
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "dead_code_candidate_rows"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	// #5167: a corpus-wide scan (repoID == "") carries the caller's granted
	// repository ids into the WHERE, so LIMIT/OFFSET pages the granted set.
	args := []any{repoID, entityType, language}
	grant := ""
	if len(query.AllowedRepositoryIDs) > 0 {
		args = append(args, pgarray.Array(query.AllowedRepositoryIDs))
		grant = fmt.Sprintf("\n\t\t  AND repo_id = ANY($%d)", len(args))
	}
	args = append(args, limit, offset)
	// #nosec G201 -- interpolates only integer argument indices and the fixed
	// grant clause above; no caller-supplied text is concatenated into the SQL.
	statement := fmt.Sprintf(`
		SELECT entity_id, entity_name, entity_type, repo_id, relative_path,
		       coalesce(language, ''), start_line, end_line, metadata
		FROM content_entities
		WHERE ($1 = '' OR repo_id = $1)
		  AND entity_type = $2
		  AND ($3 = '' OR lower(coalesce(language, '')) = $3)%s
		ORDER BY repo_id, relative_path, entity_name, entity_id
		LIMIT $%d OFFSET $%d
	`, grant, len(args)-1, len(args))
	rows, err := cr.db.QueryContext(ctx, statement, args...)
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
	case "Function", "Class", "Struct", "Interface", "Trait", "SqlFunction":
		return label, true
	default:
		return "", false
	}
}
