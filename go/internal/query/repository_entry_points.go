// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B3 stayer for #6060: methods on the root ContentReader must live in package query; the shared read model moved to querycontract.

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// repositoryEntryPointReadModel is the shared read model, aliased so this
// package's call sites keep their unexported spelling while a ContentStore
// double outside package query can still name it (#6060).
type repositoryEntryPointReadModel = querycontract.RepositoryEntryPointReadModel

type repositoryEntryPointReadModelStore interface {
	RepositoryEntryPoints(context.Context, string) (repositoryEntryPointReadModel, error)
}

// loadRepositoryEntryPoints returns content-derived entry points when the
// content store can answer the narrow query directly. The implementation
// moved to querycontract for #6060; this wrapper keeps root callers
// unchanged. The unexported store interface stays as the structural twin
// the ContentReader assertion below resolves against.
func loadRepositoryEntryPoints(ctx context.Context, content ContentStore, repoID string) []map[string]any {
	return querycontract.LoadRepositoryEntryPoints(ctx, content, repoID)
}

// RepositoryEntryPoints reads only known entry-point function names from
// content_entities, avoiding graph backends with weak IN-list filtering.
func (cr *ContentReader) RepositoryEntryPoints(ctx context.Context, repoID string) (repositoryEntryPointReadModel, error) {
	if cr == nil || cr.db == nil || repoID == "" {
		return repositoryEntryPointReadModel{}, nil
	}
	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_name, relative_path, coalesce(language, '') AS language
		FROM content_entities
		WHERE repo_id = $1
		  AND entity_type = 'Function'
		  AND entity_name IN (
			'main', 'handler', 'app', 'create_app', 'lambda_handler',
			'Main', 'Handler', 'App', 'CreateApp', 'LambdaHandler'
		  )
		ORDER BY entity_name, relative_path
	`, repoID)
	if err != nil {
		return repositoryEntryPointReadModel{}, fmt.Errorf("query repository entry points: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]map[string]any, 0)
	for rows.Next() {
		var name, relativePath, language string
		if err := rows.Scan(&name, &relativePath, &language); err != nil {
			return repositoryEntryPointReadModel{}, fmt.Errorf("scan repository entry point: %w", err)
		}
		if !querycontract.IsRepositoryEntryPointName(name) {
			continue
		}
		result = append(result, map[string]any{
			"name":          name,
			"relative_path": relativePath,
			"language":      language,
		})
	}
	if err := rows.Err(); err != nil {
		return repositoryEntryPointReadModel{}, fmt.Errorf("iterate repository entry points: %w", err)
	}
	return repositoryEntryPointReadModel{Available: true, Rows: result}, nil
}
