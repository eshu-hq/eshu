// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/repository"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// repositoryEntityCoverageSQL is the single grouped pass over one repository's
// content_entities rows that RepositoryCoverage derives the entity total, the
// newest indexed_at, and the type distribution from (#7126).
const repositoryEntityCoverageSQL = `
		SELECT entity_type, count(*) as entity_count, max(indexed_at) as indexed_at
		FROM content_entities
		WHERE repo_id = $1
		GROUP BY entity_type
		ORDER BY entity_count DESC, entity_type
	`

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

	fileIndexedAt, err := repository.QueryMaxIndexedAt(ctx, cr.db, repository.CoverageContentFilesTable, repoID)
	if err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("query content file indexed_at: %w", err)
	}
	coverage.FileIndexedAt = fileIndexedAt

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

	// One grouped pass over the repository's content_entities rows yields the
	// type distribution, and the total and newest indexed_at are derived from
	// its groups. The earlier shape ran three separate whole-repository scans
	// (count, max(indexed_at), GROUP BY entity_type), which dominated the
	// repository story on large repositories (#7126).
	entityRows, err := cr.db.QueryContext(ctx, repositoryEntityCoverageSQL, repoID)
	if err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("query entity type distribution: %w", err)
	}
	defer func() { _ = entityRows.Close() }()

	entityTypes := make([]RepositoryEntityTypeCount, 0)
	var entityIndexedAt time.Time
	for entityRows.Next() {
		var entityType RepositoryEntityTypeCount
		var groupIndexedAt sql.NullTime
		if err := entityRows.Scan(&entityType.EntityType, &entityType.Count, &groupIndexedAt); err != nil {
			span.RecordError(err)
			return RepositoryContentCoverage{}, fmt.Errorf("scan entity type row: %w", err)
		}
		entityTypes = append(entityTypes, entityType)
		coverage.EntityCount += entityType.Count
		if groupIndexedAt.Valid && groupIndexedAt.Time.After(entityIndexedAt) {
			entityIndexedAt = groupIndexedAt.Time
		}
	}
	if err := entityRows.Err(); err != nil {
		span.RecordError(err)
		return RepositoryContentCoverage{}, fmt.Errorf("iterate entity type rows: %w", err)
	}
	coverage.EntityTypes = entityTypes
	if !entityIndexedAt.IsZero() {
		coverage.EntityIndexedAt = entityIndexedAt.UTC()
	}

	return coverage, nil
}
