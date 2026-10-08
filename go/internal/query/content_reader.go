// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ContentReader reads file and entity content from the Postgres content store.
type ContentReader struct {
	db          db.ReadStore
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewContentReader constructs a Postgres-backed content store reader.
func NewContentReader(db *sql.DB) *ContentReader {
	return NewContentReaderWithReadStore(postgres.NewSQLReadStore(db))
}

// NewContentReaderWithReadStore constructs a content reader on a guarded read store.
// See read-access.md for the content read contract.
func NewContentReaderWithReadStore(readStore db.ReadStore) *ContentReader {
	return &ContentReader{
		db:     readStore,
		tracer: otel.Tracer("eshu/go/internal/query"),
	}
}

// This block is the compile-time half of the #6060 interface-export
// tripwire.
//
// interface_export_tripwire_test.go and
// interface_export_tripwire_evidence_repository_test.go prove that a given
// handler entry point actually takes the fast path when h.Content (or a
// content parameter) satisfies one of the 14 exported-method interfaces
// below -- but every one of those tests wires a same-package fake, not the
// production type. A fake keeps satisfying its interface forever no matter
// what happens to *ContentReader, so those tests alone cannot catch the
// defect class they exist for: the interface and its implementer drifting
// into separate packages, or a rename that quietly un-exports one of these
// methods again. Either leaves every fake-backed tripwire green while
// production silently falls back to the slower or lossier path.
//
// These assertions close that gap. Each line fails `go build`, not only
// `go test`, the moment *ContentReader stops satisfying the interface next
// to it -- exactly the regression a runtime tripwire cannot see.
//
// All 14 are bound to *ContentReader because it is the only production
// implementer: cmd/api/wiring.go and cmd/mcp-server/wiring.go both wire
// a ContentReader as the sole content store passed to every handler's
// Content field, and no other type in this package defines any of these 14
// method sets (confirmed by symbol search across internal/query when this
// block was added; see the #6060 PR description for the break/restore
// proof).
var (
	_ cloudInventoryReadModelStore               = (*ContentReader)(nil)
	_ codequery.HardcodedSecretInvestigator      = (*ContentReader)(nil)
	_ codequery.SymbolContentSearcher            = (*ContentReader)(nil)
	_ codequery.CodeTopicContentInvestigator     = (*ContentReader)(nil)
	_ querycontract.PagedContentSearcher         = (*ContentReader)(nil)
	_ documentationReadModelStore                = (*ContentReader)(nil)
	_ relationshipEvidenceReadModelStore         = (*ContentReader)(nil)
	_ repositoryDeploymentEvidenceReadModelStore = (*ContentReader)(nil)
	_ repositoryEntryPointReadModelStore         = (*ContentReader)(nil)
	_ repositoryReadModelSummaryStore            = (*ContentReader)(nil)
	_ repositoryRelationshipReadModelStore       = (*ContentReader)(nil)
	_ serviceStoryTargetSupportStore             = (*ContentReader)(nil)
	_ evidenceCitationFileStore                  = (*ContentReader)(nil)
	_ semanticEvidenceStore                      = (*ContentReader)(nil)
)

// The dead-code investigation looks these two narrow coverage ports up by type
// assertion (codequery/deadcode/investigation_coverage.go) and falls back to the
// full RepositoryCoverage -- the entity aggregate that reads about 1 GiB of heap
// on a large repository (#7525) -- when either is missing. A fake-backed test
// cannot see *ContentReader lose one of them, so these assertions fail `go build`
// the moment a signature drift would silently route production back to the
// expensive read. The repository-context count port is the same class: without
// it, context would silently fall back to the full summary and its workload-name
// read (#7542). They sit in their own block so the 14 above keep their
// alignment.
var (
	_ querycontract.RepositoryContextCoverageReadModelStore = (*ContentReader)(nil)
	_ querycontract.RepositoryFilesIndexedAtReadModelStore  = (*ContentReader)(nil)
	_ repository.RepositoryReadModelCountsStore             = (*ContentReader)(nil)
)

// EntityContent is one indexed entity and its content metadata.
type EntityContent = querycontract.EntityContent

// GetFileContent returns one file by repo_id and relative_path.
func (cr *ContentReader) GetFileContent(ctx context.Context, repoID, relativePath string) (*FileContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_file_content"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	row := cr.db.QueryRowContext(ctx, `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       content, content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1 AND relative_path = $2
	`, repoID, relativePath)

	var f FileContent
	err := row.Scan(&f.RepoID, &f.RelativePath, &f.CommitSHA,
		&f.Content, &f.ContentHash, &f.LineCount, &f.Language, &f.ArtifactType)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get file content: %w", err)
	}
	return &f, nil
}

// GetFileLines returns a line range from one file.
func (cr *ContentReader) GetFileLines(ctx context.Context, repoID, relativePath string, startLine, endLine int) (*FileContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_file_lines"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	fc, err := cr.GetFileContent(ctx, repoID, relativePath)
	if err != nil || fc == nil {
		if err != nil {
			span.RecordError(err)
		}
		return fc, err
	}

	lines := strings.Split(fc.Content, "\n")
	if startLine < 1 {
		startLine = 1
	}
	if endLine < 1 || endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > len(lines) {
		fc.Content = ""
		fc.LineCount = 0
		return fc, nil
	}

	selected := lines[startLine-1 : endLine]
	fc.Content = strings.Join(selected, "\n")
	fc.LineCount = len(selected)
	return fc, nil
}

// SearchFileContent searches file content using trigram matching.
func (cr *ContentReader) SearchFileContent(ctx context.Context, repoID, pattern string, limit int) ([]FileContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_file_content"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       '', content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1 AND content ILIKE '%' || $2 || '%'
		ORDER BY relative_path
		LIMIT $3
	`
	rows, err := cr.db.QueryContext(ctx, query, repoID, pattern, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search file content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContent
	for rows.Next() {
		var f FileContent
		if err := rows.Scan(&f.RepoID, &f.RelativePath, &f.CommitSHA,
			&f.Content, &f.ContentHash, &f.LineCount, &f.Language, &f.ArtifactType); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan file search result: %w", err)
		}
		results = append(results, f)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// SearchEntitiesByName returns entities whose materialized name matches the
// requested pattern, optionally restricted to one entity type.
func (cr *ContentReader) SearchEntitiesByName(
	ctx context.Context,
	repoID string,
	entityType string,
	name string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entities_by_name"),
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
		WHERE repo_id = $1 AND entity_name ILIKE '%' || $2 || '%'
	`
	args := []any{repoID, name}
	nextArg := 3
	if entityType != "" {
		filter, filterArgs, next := contentEntityTypeFilter(entityType, nextArg)
		query += ` AND ` + filter
		args = append(args, filterArgs...)
		nextArg = next
	}
	// #nosec G202 -- appends only an integer arg index ($N) for the LIMIT clause; no user data concatenated into SQL
	query += fmt.Sprintf(`
		ORDER BY relative_path, start_line
		LIMIT $%d
	`, nextArg)
	args = append(args, limit)

	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entities by name: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var e EntityContent
		var rawMetadata []byte
		if err := rows.Scan(&e.EntityID, &e.RepoID, &e.RelativePath, &e.EntityType,
			&e.EntityName, &e.StartLine, &e.EndLine, &e.Language, &e.SourceCache, &rawMetadata); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity name result: %w", err)
		}
		e.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan entity name result: %w", err)
		}
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// SearchEntitiesReferencingComponent returns content entities whose metadata
// records JSX usage of the requested component name.
func (cr *ContentReader) SearchEntitiesReferencingComponent(
	ctx context.Context,
	repoID string,
	componentName string,
	limit int,
) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "search_entities_referencing_component"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 50
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE repo_id = $1
		  AND coalesce(metadata -> 'jsx_component_usage', '[]'::jsonb) ? $2
		ORDER BY relative_path, start_line, entity_name
		LIMIT $3
	`, repoID, componentName, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("search entities referencing component: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []EntityContent
	for rows.Next() {
		var entity EntityContent
		var rawMetadata []byte
		if err := rows.Scan(
			&entity.EntityID,
			&entity.RepoID,
			&entity.RelativePath,
			&entity.EntityType,
			&entity.EntityName,
			&entity.StartLine,
			&entity.EndLine,
			&entity.Language,
			&entity.SourceCache,
			&rawMetadata,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan referencing component result: %w", err)
		}
		entity.Metadata, err = decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan referencing component result: %w", err)
		}
		results = append(results, entity)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// ListRepoFiles returns all indexed files for one repository.
func (cr *ContentReader) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]FileContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_files"),
			attribute.String("db.sql.table", "content_files"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 500
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       '', content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1
		ORDER BY relative_path
		LIMIT $2
	`, repoID, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileContent
	for rows.Next() {
		var f FileContent
		if err := rows.Scan(&f.RepoID, &f.RelativePath, &f.CommitSHA,
			&f.Content, &f.ContentHash, &f.LineCount, &f.Language, &f.ArtifactType); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repo file: %w", err)
		}
		results = append(results, f)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}

// ListRepoEntities returns all indexed entities for one repository.
func (cr *ContentReader) ListRepoEntities(ctx context.Context, repoID string, limit int) ([]EntityContent, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_entities"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 500
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
		       start_line, end_line, coalesce(language, ''), coalesce(source_cache, ''),
		       metadata
		FROM content_entities
		WHERE repo_id = $1
		ORDER BY relative_path, start_line
		LIMIT $2
	`, repoID, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo entities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEntityContentRows(rows, span, "scan repo entity")
}

// codeTopicTermsMaterialization returns the terms CTE materialization marker.
// #7246: the scoped one-term statement plans with the literal (custom plan). A
// plain VALUES list lets the entity probe's trigram estimate for a
// corpus-common term skip the repo bitmap; MATERIALIZED hides the parameter so
// the planner ANDs content_entities_repo_idx. Every other shape keeps the
// plain CTE.
func codeTopicTermsMaterialization(scopedOneTerm bool) string {
	if scopedOneTerm {
		return "MATERIALIZED "
	}
	return ""
}

// scopedCodeTopicFileBranch preserves the measured explicit-repository
// single-statement file probe. The caller must supply the repository filter.
// Keep path/content pool accounting in sync with codetopicparallel.FileBranch;
// only the term binding differs.
func scopedCodeTopicFileBranch(termArg int, where string, candidateCap int) string {
	termParam := fmt.Sprintf("term_param AS MATERIALIZED (SELECT $%d::text AS term),\n\t\t  ", termArg)
	contentTerm := "(SELECT term FROM term_param)"
	// #nosec G201 -- termArg and candidateCap are integers; where contains
	// only generated placeholder predicates from codeTopicFilters.
	return fmt.Sprintf(`(
		  WITH %[4]spath_pool AS MATERIALIZED (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $%[1]d AS matched_term
		    FROM content_files f
		    WHERE f.relative_path ILIKE '%%' || $%[1]d || '%%'
		    %[2]s LIMIT %[3]d
		  ),
		  content_pool AS (
		    SELECT f.repo_id, f.relative_path, coalesce(f.language, '') AS language,
		           least(greatest(coalesce(f.line_count, 1), 1), 80) AS end_line, $%[1]d AS matched_term
		    FROM content_files f
		    WHERE f.content ILIKE '%%' || %[5]s || '%%'
		      AND f.relative_path NOT ILIKE '%%' || $%[1]d || '%%'
		    %[2]s LIMIT (SELECT %[3]d - count(*) FROM path_pool)
		  )
		  SELECT * FROM path_pool
		  UNION ALL
		  SELECT * FROM content_pool
		)`, termArg, where, candidateCap, termParam, contentTerm)
}
