package query

import (
	"context"
	"fmt"
)

type repositoryEntryPointReadModel struct {
	Available bool
	Rows      []map[string]any
}

type repositoryEntryPointReadModelStore interface {
	repositoryEntryPoints(context.Context, string) (repositoryEntryPointReadModel, error)
}

// loadRepositoryEntryPoints returns content-derived entry points when the
// content store can answer the narrow query directly.
func loadRepositoryEntryPoints(ctx context.Context, content ContentStore, repoID string) []map[string]any {
	store, ok := content.(repositoryEntryPointReadModelStore)
	if !ok || repoID == "" {
		return nil
	}
	readModel, err := store.repositoryEntryPoints(ctx, repoID)
	if err != nil || !readModel.Available {
		return nil
	}
	return readModel.Rows
}

// repositoryEntryPoints reads only known entry-point function names from
// content_entities, avoiding graph backends with weak IN-list filtering.
func (cr *ContentReader) repositoryEntryPoints(ctx context.Context, repoID string) (repositoryEntryPointReadModel, error) {
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
		if !isRepositoryEntryPointName(name) {
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
