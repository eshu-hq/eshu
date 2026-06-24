// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// Page-size bounds for the streaming search-document load (issue #3440). The old
// loader pulled every content row for a whale repository (~159K entities / ~94MB
// of file content) into a single slice, which dominated handler time and blocked
// reducer workers for minutes. Keyset pagination with these bounds keeps peak
// memory and per-page work proportional to one page, not the whole repository.
const (
	// eshuSearchDocumentEntityPageSize bounds entity rows per page. Entities are
	// small (no large content column) so 2000 rows is a comfortable round-trip.
	eshuSearchDocumentEntityPageSize = 2000

	// eshuSearchDocumentFilePageSize bounds file rows per page. Files carry the
	// full content column, so the row cap is smaller than the entity cap.
	eshuSearchDocumentFilePageSize = 256

	// eshuSearchDocumentFilePageByteBudget bounds the total file content bytes
	// buffered before a page is flushed. A single file can be tens of MB, which
	// is an unavoidable per-row floor, but this budget prevents buffering many
	// large files at once. 16 MiB keeps a page bounded while admitting at least
	// one oversized file per page.
	eshuSearchDocumentFilePageByteBudget = 16 << 20
)

// resolveEshuSearchDocumentRepoIDQuery resolves the scope's repository once so
// the paginated content queries can anchor directly on the indexed repo_id
// column instead of repeating the ingestion_scopes subquery per page.
const resolveEshuSearchDocumentRepoIDQuery = `
SELECT payload->>'repo_id'
FROM ingestion_scopes
WHERE scope_id = $1
`

// content_entities and content_files hold the current indexed snapshot for a
// repository, keyed by repo_id (rows are overwritten on re-index, not kept per
// generation). The projection reads the current snapshot and the writer tags the
// resulting facts with the intent's generation; the active-generation reader and
// generation-scoped retirement keep the read model converged.
//
// Both queries keyset-paginate: entities by entity_id (the PRIMARY KEY) and
// files by relative_path (part of the PRIMARY KEY (repo_id, relative_path)).
// Walking the indexed key in order with a per-page LIMIT removes the unbounded
// SELECT and the ~50MB external-merge sort the old ORDER BY produced, and keeps
// each read bounded to one page.
const loadEshuSearchDocumentEntitiesPageQuery = `
SELECT entity_id,
       repo_id,
       COALESCE(relative_path, ''),
       COALESCE(entity_type, ''),
       COALESCE(entity_name, ''),
       COALESCE(start_line, 0),
       COALESCE(end_line, 0),
       COALESCE(language, ''),
       COALESCE(artifact_type, ''),
       COALESCE(source_cache, ''),
       COALESCE(metadata, '{}'::jsonb),
       indexed_at
FROM content_entities
WHERE repo_id = $1
  AND entity_id > $2
ORDER BY entity_id
LIMIT $3
`

const loadEshuSearchDocumentFilesPageQuery = `
SELECT repo_id,
       COALESCE(relative_path, ''),
       COALESCE(language, ''),
       COALESCE(artifact_type, ''),
       COALESCE(content, ''),
       indexed_at
FROM content_files
WHERE repo_id = $1
  AND relative_path > $2
ORDER BY relative_path
LIMIT $3
`

// EshuSearchDocumentSourceLoader streams the current indexed content for a
// scope's repository as curated-search projection inputs in bounded keyset
// pages. It implements reducer.SearchDocumentSourceLoader.
type EshuSearchDocumentSourceLoader struct {
	db Queryer
	// entityPageSize and filePageSize bound rows per keyset page. They default
	// to the package constants; tests override them to exercise pagination
	// without materialising production-scale fixtures.
	entityPageSize int
	filePageSize   int
}

// NewEshuSearchDocumentSourceLoader builds a content source loader over db with
// the production page-size bounds.
func NewEshuSearchDocumentSourceLoader(db Queryer) EshuSearchDocumentSourceLoader {
	return EshuSearchDocumentSourceLoader{
		db:             db,
		entityPageSize: eshuSearchDocumentEntityPageSize,
		filePageSize:   eshuSearchDocumentFilePageSize,
	}
}

// resolvedEntityPageSize returns the effective entity page size, falling back to
// the production default when unset (zero value).
func (l EshuSearchDocumentSourceLoader) resolvedEntityPageSize() int {
	if l.entityPageSize > 0 {
		return l.entityPageSize
	}
	return eshuSearchDocumentEntityPageSize
}

// resolvedFilePageSize returns the effective file page size, falling back to the
// production default when unset (zero value).
func (l EshuSearchDocumentSourceLoader) resolvedFilePageSize() int {
	if l.filePageSize > 0 {
		return l.filePageSize
	}
	return eshuSearchDocumentFilePageSize
}

// StreamSearchDocumentSources resolves the scope's repository and streams its
// indexed content to page in bounded keyset pages: all entity pages first, then
// all file pages. The generationID is part of the contract (it tags the written
// facts) but the content snapshot is the repository's current indexed state, so
// the load is keyed by repository. Runtime summaries are not yet a content-store
// source. Streaming bounds peak memory to one page regardless of repository size
// (issue #3440); the handler projects and writes each page incrementally.
func (l EshuSearchDocumentSourceLoader) StreamSearchDocumentSources(
	ctx context.Context,
	scopeID string,
	generationID string,
	page func(reducer.SearchDocumentProjectionInput) error,
) error {
	if l.db == nil {
		return fmt.Errorf("eshu search document source loader requires a database")
	}
	if page == nil {
		return fmt.Errorf("eshu search document source loader requires a page callback")
	}
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" || strings.TrimSpace(generationID) == "" {
		return fmt.Errorf("eshu search document source loader requires scope and generation")
	}

	repoID, err := l.resolveRepoID(ctx, scopeID)
	if err != nil {
		return err
	}
	if repoID == "" {
		// No repository resolved for the scope: nothing to stream.
		return nil
	}

	if err := l.streamEntities(ctx, repoID, page); err != nil {
		return err
	}
	return l.streamFiles(ctx, repoID, page)
}

// resolveRepoID resolves the repository id for the scope once.
func (l EshuSearchDocumentSourceLoader) resolveRepoID(ctx context.Context, scopeID string) (string, error) {
	rows, err := l.db.QueryContext(ctx, resolveEshuSearchDocumentRepoIDQuery, scopeID)
	if err != nil {
		return "", fmt.Errorf("resolve search document repo id: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var repoID string
	if rows.Next() {
		if err := rows.Scan(&repoID); err != nil {
			return "", fmt.Errorf("scan search document repo id: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate search document repo id: %w", err)
	}
	return strings.TrimSpace(repoID), nil
}

// streamEntities keyset-paginates entities by entity_id and emits one page per
// fetched batch. The cursor advances to the last entity_id of each page, so the
// loop terminates when a page returns fewer rows than the page size.
func (l EshuSearchDocumentSourceLoader) streamEntities(
	ctx context.Context,
	repoID string,
	page func(reducer.SearchDocumentProjectionInput) error,
) error {
	pageSize := l.resolvedEntityPageSize()
	cursor := ""
	for {
		entities, next, err := l.loadEntityPage(ctx, repoID, cursor, pageSize)
		if err != nil {
			return err
		}
		if len(entities) == 0 {
			return nil
		}
		if err := page(reducer.SearchDocumentProjectionInput{ContentEntities: entities}); err != nil {
			return err
		}
		if len(entities) < pageSize {
			return nil
		}
		cursor = next
	}
}

// loadEntityPage fetches one bounded entity page after cursor and returns the
// rows plus the next cursor (the last entity_id read).
func (l EshuSearchDocumentSourceLoader) loadEntityPage(
	ctx context.Context,
	repoID string,
	cursor string,
	pageSize int,
) ([]searchdocs.ContentEntity, string, error) {
	rows, err := l.db.QueryContext(ctx, loadEshuSearchDocumentEntitiesPageQuery, repoID, cursor, pageSize)
	if err != nil {
		return nil, "", fmt.Errorf("load search document content entities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entities := make([]searchdocs.ContentEntity, 0, pageSize)
	next := cursor
	for rows.Next() {
		var (
			entity      searchdocs.ContentEntity
			metadataRaw []byte
			indexedAt   time.Time
		)
		if err := rows.Scan(
			&entity.EntityID,
			&entity.RepoID,
			&entity.RelativePath,
			&entity.EntityType,
			&entity.EntityName,
			&entity.StartLine,
			&entity.EndLine,
			&entity.Language,
			&entity.ArtifactType,
			&entity.SourceCache,
			&metadataRaw,
			&indexedAt,
		); err != nil {
			return nil, "", fmt.Errorf("scan search document content entity: %w", err)
		}
		entity.Metadata = decodeEshuSearchDocumentMetadata(metadataRaw)
		entity.IndexedAt = indexedAt
		entities = append(entities, entity)
		next = entity.EntityID
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate search document content entities: %w", err)
	}
	return entities, next, nil
}

// streamFiles keyset-paginates files by relative_path and emits one page per
// fetched batch. The cursor advances to the last relative_path of each page.
func (l EshuSearchDocumentSourceLoader) streamFiles(
	ctx context.Context,
	repoID string,
	page func(reducer.SearchDocumentProjectionInput) error,
) error {
	pageSize := l.resolvedFilePageSize()
	cursor := ""
	for {
		files, next, more, err := l.loadFilePage(ctx, repoID, cursor, pageSize)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return nil
		}
		if err := page(reducer.SearchDocumentProjectionInput{ContentFiles: files}); err != nil {
			return err
		}
		if !more {
			return nil
		}
		cursor = next
	}
}

// loadFilePage fetches up to one bounded file page after cursor, stopping early
// when the buffered content byte budget is reached so a page of large files
// cannot itself exhaust memory. It returns the rows, the next cursor (the last
// relative_path read), and whether more rows may remain. Rows beyond an
// early budget flush are re-fetched from the advanced cursor on the next call,
// so no file is skipped or duplicated.
func (l EshuSearchDocumentSourceLoader) loadFilePage(
	ctx context.Context,
	repoID string,
	cursor string,
	pageSize int,
) ([]searchdocs.ContentFile, string, bool, error) {
	rows, err := l.db.QueryContext(ctx, loadEshuSearchDocumentFilesPageQuery, repoID, cursor, pageSize)
	if err != nil {
		return nil, "", false, fmt.Errorf("load search document content files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	files := make([]searchdocs.ContentFile, 0, pageSize)
	next := cursor
	bufferedBytes := 0
	rowsRead := 0
	for rows.Next() {
		var (
			file      searchdocs.ContentFile
			indexedAt time.Time
		)
		if err := rows.Scan(
			&file.RepoID,
			&file.RelativePath,
			&file.Language,
			&file.ArtifactType,
			&file.Content,
			&indexedAt,
		); err != nil {
			return nil, "", false, fmt.Errorf("scan search document content file: %w", err)
		}
		file.IndexedAt = indexedAt
		files = append(files, file)
		next = file.RelativePath
		rowsRead++
		bufferedBytes += len(file.Content)
		if bufferedBytes >= eshuSearchDocumentFilePageByteBudget {
			// Flush early to bound memory; more rows in this LIMIT batch (and
			// possibly later batches) remain and are re-fetched from `next`.
			return files, next, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, fmt.Errorf("iterate search document content files: %w", err)
	}
	// A full LIMIT batch means more rows may remain; a short batch is the end.
	more := rowsRead == pageSize
	return files, next, more, nil
}

// decodeEshuSearchDocumentMetadata best-effort decodes content entity metadata
// JSON into a string map, returning nil on any malformed payload so projection
// proceeds without the metadata-based exclusion signal rather than failing.
func decodeEshuSearchDocumentMetadata(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil
	}
	if len(generic) == 0 {
		return nil
	}
	metadata := make(map[string]string, len(generic))
	for key, value := range generic {
		if str, ok := value.(string); ok {
			metadata[key] = str
		}
	}
	return metadata
}
