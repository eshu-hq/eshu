// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ListRepositoryRefs returns source-backed Git refs observed for a repository.
func (cr *ContentReader) ListRepositoryRefs(ctx context.Context, repoID string) ([]RepositoryRef, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repository_refs"),
			attribute.String("db.sql.table", "repository_refs"),
		),
	)
	defer span.End()

	rows, err := cr.db.QueryContext(ctx, `
		SELECT name, ref_kind, head_sha, is_default, observed_at, indexed_at
		FROM repository_refs
		WHERE repo_id = $1
		ORDER BY is_default DESC, ref_kind, name
	`, repoID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repository refs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var refs []RepositoryRef
	for rows.Next() {
		var ref RepositoryRef
		if err := rows.Scan(
			&ref.Name,
			&ref.Kind,
			&ref.HeadSHA,
			&ref.Default,
			&ref.ObservedAt,
			&ref.IndexedAt,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repository ref: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return refs, err
	}
	return refs, nil
}
