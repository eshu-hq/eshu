package query

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RepositoryCoverage returns content-store coverage for one repository.
func (cr *ContentReader) RepositoryCoverage(ctx context.Context, repoID string) (RepositoryContentCoverage, error) {
	if cr == nil || cr.db == nil {
		return RepositoryContentCoverage{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "repository_coverage"),
			attribute.String("db.sql.table", "content_files,content_entities"),
		),
	)
	defer span.End()

	var coverage RepositoryContentCoverage
	coverage.Available = true

	if err := cr.db.QueryRowContext(ctx, `
		SELECT count(*) FROM content_files WHERE repo_id = $1
	`, repoID).Scan(&coverage.FileCount); err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("query file count: %w", err)
	}

	if err := cr.db.QueryRowContext(ctx, `
		SELECT count(*) FROM content_entities WHERE repo_id = $1
	`, repoID).Scan(&coverage.EntityCount); err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("query entity count: %w", err)
	}

	fileIndexedAt, err := queryMaxIndexedAt(ctx, cr.db, repositoryCoverageContentFilesTable, repoID)
	if err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("query content file indexed_at: %w", err)
	}
	entityIndexedAt, err := queryMaxIndexedAt(ctx, cr.db, repositoryCoverageContentEntitiesTable, repoID)
	if err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("query content entity indexed_at: %w", err)
	}
	coverage.FileIndexedAt = fileIndexedAt
	coverage.EntityIndexedAt = entityIndexedAt

	rows, err := cr.db.QueryContext(ctx, `
		SELECT coalesce(language, 'unknown') as language, count(*) as file_count
		FROM content_files
		WHERE repo_id = $1
		GROUP BY language
		ORDER BY file_count DESC
	`, repoID)
	if err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("query language distribution: %w", err)
	}
	defer func() { _ = rows.Close() }()

	languages := make([]RepositoryLanguageCount, 0)
	for rows.Next() {
		var language RepositoryLanguageCount
		if err := rows.Scan(&language.Language, &language.FileCount); err != nil {
			return RepositoryContentCoverage{}, fmt.Errorf("scan language row: %w", err)
		}
		languages = append(languages, language)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("iterate language rows: %w", err)
	}
	coverage.Languages = languages

	entityRows, err := cr.db.QueryContext(ctx, `
		SELECT entity_type, count(*) as entity_count
		FROM content_entities
		WHERE repo_id = $1
		GROUP BY entity_type
		ORDER BY entity_count DESC, entity_type
	`, repoID)
	if err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("query entity type distribution: %w", err)
	}
	defer func() { _ = entityRows.Close() }()

	entityTypes := make([]RepositoryEntityTypeCount, 0)
	for entityRows.Next() {
		var entityType RepositoryEntityTypeCount
		if err := entityRows.Scan(&entityType.EntityType, &entityType.Count); err != nil {
			span.RecordError(err)
			return RepositoryContentCoverage{}, fmt.Errorf("scan entity type row: %w", err)
		}
		entityTypes = append(entityTypes, entityType)
	}
	if err := entityRows.Err(); err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("iterate entity type rows: %w", err)
	}
	coverage.EntityTypes = entityTypes

	return coverage, nil
}
