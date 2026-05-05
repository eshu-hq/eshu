package query

import (
	"context"
	"fmt"
)

// RepositoryCoverage returns content-store coverage for one repository.
func (cr *ContentReader) RepositoryCoverage(ctx context.Context, repoID string) (RepositoryContentCoverage, error) {
	if cr == nil || cr.db == nil {
		return RepositoryContentCoverage{}, nil
	}

	var coverage RepositoryContentCoverage
	coverage.Available = true

	if err := cr.db.QueryRowContext(ctx, `
		SELECT count(*) FROM content_files WHERE repo_id = $1
	`, repoID).Scan(&coverage.FileCount); err != nil {
		return RepositoryContentCoverage{}, fmt.Errorf("query file count: %w", err)
	}

	if err := cr.db.QueryRowContext(ctx, `
		SELECT count(*) FROM content_entities WHERE repo_id = $1
	`, repoID).Scan(&coverage.EntityCount); err != nil {
		return RepositoryContentCoverage{}, fmt.Errorf("query entity count: %w", err)
	}

	fileIndexedAt, err := queryMaxIndexedAt(ctx, cr.db, "content_files", repoID)
	if err != nil {
		return RepositoryContentCoverage{}, fmt.Errorf("query content file indexed_at: %w", err)
	}
	entityIndexedAt, err := queryMaxIndexedAt(ctx, cr.db, "content_entities", repoID)
	if err != nil {
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
		return RepositoryContentCoverage{}, fmt.Errorf("iterate language rows: %w", err)
	}
	coverage.Languages = languages

	return coverage, nil
}
