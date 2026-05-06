package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const getFileContentQuery = `
SELECT repo_id,
       relative_path,
       commit_sha,
       content,
       content_hash,
       line_count,
       language,
       artifact_type,
       template_dialect,
       iac_relevant
FROM content_files
WHERE repo_id = $1 AND relative_path = $2
`

const getEntityContentQuery = `
SELECT entity_id,
       ce.repo_id,
       ce.relative_path,
       ce.entity_type,
       ce.entity_name,
       ce.start_line,
       ce.end_line,
       ce.start_byte,
       ce.end_byte,
       ce.language,
       ce.artifact_type,
       ce.template_dialect,
       ce.iac_relevant,
       ce.source_cache
FROM content_entities ce
WHERE entity_id = $1
`

// FileContentRow represents a single file content row returned by reads and searches.
type FileContentRow struct {
	RepoID          string
	RelativePath    string
	CommitSHA       string
	Content         string
	ContentHash     string
	LineCount       int
	Language        string
	ArtifactType    string
	TemplateDialect string
	IACRelevant     *bool
}

// EntityContentRow represents a single entity content row returned by reads and searches.
type EntityContentRow struct {
	EntityID        string
	RepoID          string
	RelativePath    string
	EntityType      string
	EntityName      string
	StartLine       int
	EndLine         int
	StartByte       *int
	EndByte         *int
	Language        string
	ArtifactType    string
	TemplateDialect string
	IACRelevant     *bool
	SourceCache     string
}

// ContentStore provides read and batch-write access to the Postgres content store.
type ContentStore struct {
	db  ExecQueryer
	Now func() time.Time
}

// NewContentStore constructs a Postgres-backed content store.
func NewContentStore(db ExecQueryer) ContentStore {
	return ContentStore{db: db}
}

// GetFileContent returns a single file content row for one repo-relative path.
// Returns nil when no matching row exists.
func (s ContentStore) GetFileContent(
	ctx context.Context,
	repoID string,
	relativePath string,
) (*FileContentRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("content store database is required")
	}

	rows, err := s.db.QueryContext(ctx, getFileContentQuery, repoID, relativePath)
	if err != nil {
		return nil, fmt.Errorf("get file content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get file content: %w", err)
		}
		return nil, nil
	}

	row, err := scanFileContentRow(rows)
	if err != nil {
		return nil, fmt.Errorf("get file content: %w", err)
	}

	return &row, nil
}

// GetEntityContent returns a single entity content row for the given entity ID.
// Returns nil when no matching row exists.
func (s ContentStore) GetEntityContent(
	ctx context.Context,
	entityID string,
) (*EntityContentRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("content store database is required")
	}

	rows, err := s.db.QueryContext(ctx, getEntityContentQuery, entityID)
	if err != nil {
		return nil, fmt.Errorf("get entity content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get entity content: %w", err)
		}
		return nil, nil
	}

	row, err := scanEntityContentRow(rows)
	if err != nil {
		return nil, fmt.Errorf("get entity content: %w", err)
	}

	return &row, nil
}

// SearchFileContent searches indexed file content using an ILIKE pattern with
// optional repo ID filter. Results are capped at the given limit.
func (s ContentStore) SearchFileContent(
	ctx context.Context,
	query string,
	repoID string,
	limit int,
) ([]FileContentRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("content store database is required")
	}
	if limit <= 0 {
		limit = 50
	}

	sqlQuery, args := buildFileSearchQuery(query, repoID, limit)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search file content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContentRow
	for rows.Next() {
		row, scanErr := scanFileContentRow(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("search file content: %w", scanErr)
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search file content: %w", err)
	}

	return results, nil
}

// SearchEntityContent searches cached entity snippets using an ILIKE pattern
// with optional repo ID filter. Results are capped at the given limit.
func (s ContentStore) SearchEntityContent(
	ctx context.Context,
	query string,
	repoID string,
	limit int,
) ([]EntityContentRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("content store database is required")
	}
	if limit <= 0 {
		limit = 50
	}

	sqlQuery, args := buildEntitySearchQuery(query, repoID, limit)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search entity content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContentRow
	for rows.Next() {
		row, scanErr := scanEntityContentRow(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("search entity content: %w", scanErr)
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search entity content: %w", err)
	}

	return results, nil
}

func (s ContentStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func scanFileContentRow(rows Rows) (FileContentRow, error) {
	var row FileContentRow
	var commitSHA sql.NullString
	var language sql.NullString
	var artifactType sql.NullString
	var templateDialect sql.NullString
	var iacRelevant sql.NullBool

	if err := rows.Scan(
		&row.RepoID,
		&row.RelativePath,
		&commitSHA,
		&row.Content,
		&row.ContentHash,
		&row.LineCount,
		&language,
		&artifactType,
		&templateDialect,
		&iacRelevant,
	); err != nil {
		return FileContentRow{}, err
	}

	if commitSHA.Valid {
		row.CommitSHA = commitSHA.String
	}
	if language.Valid {
		row.Language = language.String
	}
	if artifactType.Valid {
		row.ArtifactType = artifactType.String
	}
	if templateDialect.Valid {
		row.TemplateDialect = templateDialect.String
	}
	if iacRelevant.Valid {
		row.IACRelevant = &iacRelevant.Bool
	}

	return row, nil
}

func scanEntityContentRow(rows Rows) (EntityContentRow, error) {
	var row EntityContentRow
	var startByte sql.NullInt64
	var endByte sql.NullInt64
	var language sql.NullString
	var artifactType sql.NullString
	var templateDialect sql.NullString
	var iacRelevant sql.NullBool

	if err := rows.Scan(
		&row.EntityID,
		&row.RepoID,
		&row.RelativePath,
		&row.EntityType,
		&row.EntityName,
		&row.StartLine,
		&row.EndLine,
		&startByte,
		&endByte,
		&language,
		&artifactType,
		&templateDialect,
		&iacRelevant,
		&row.SourceCache,
	); err != nil {
		return EntityContentRow{}, err
	}

	if startByte.Valid {
		v := int(startByte.Int64)
		row.StartByte = &v
	}
	if endByte.Valid {
		v := int(endByte.Int64)
		row.EndByte = &v
	}
	if language.Valid {
		row.Language = language.String
	}
	if artifactType.Valid {
		row.ArtifactType = artifactType.String
	}
	if templateDialect.Valid {
		row.TemplateDialect = templateDialect.String
	}
	if iacRelevant.Valid {
		row.IACRelevant = &iacRelevant.Bool
	}

	return row, nil
}

func buildFileSearchQuery(pattern string, repoID string, limit int) (string, []any) {
	var filters []string
	var args []any
	argIdx := 1

	filters = append(filters, fmt.Sprintf("content ILIKE $%d", argIdx))
	args = append(args, "%"+pattern+"%")
	argIdx++

	if strings.TrimSpace(repoID) != "" {
		filters = append(filters, fmt.Sprintf("repo_id = $%d", argIdx))
		args = append(args, repoID)
		argIdx++
	}

	query := fmt.Sprintf(`
SELECT repo_id,
       relative_path,
       commit_sha,
       content,
       content_hash,
       line_count,
       language,
       artifact_type,
       template_dialect,
       iac_relevant
FROM content_files
WHERE %s
ORDER BY repo_id, relative_path
LIMIT $%d
`, strings.Join(filters, " AND "), argIdx)
	args = append(args, limit)

	return query, args
}

func buildEntitySearchQuery(pattern string, repoID string, limit int) (string, []any) {
	var filters []string
	var args []any
	argIdx := 1

	filters = append(filters, fmt.Sprintf("source_cache ILIKE $%d", argIdx))
	args = append(args, "%"+pattern+"%")
	argIdx++

	if strings.TrimSpace(repoID) != "" {
		filters = append(filters, fmt.Sprintf("ce.repo_id = $%d", argIdx))
		args = append(args, repoID)
		argIdx++
	}

	query := fmt.Sprintf(`
SELECT entity_id,
       ce.repo_id,
       ce.relative_path,
       ce.entity_type,
       ce.entity_name,
       ce.start_line,
       ce.end_line,
       ce.start_byte,
       ce.end_byte,
       ce.language,
       ce.artifact_type,
       ce.template_dialect,
       ce.iac_relevant,
       ce.source_cache
FROM content_entities ce
WHERE %s
ORDER BY ce.repo_id, ce.relative_path, ce.start_line, ce.entity_id
LIMIT $%d
`, strings.Join(filters, " AND "), argIdx)
	args = append(args, limit)

	return query, args
}
