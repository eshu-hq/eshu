// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

const (
	eshuSearchDocumentPendingDefaultLimit = 200
	eshuSearchDocumentPendingMaxLimit     = 1000
)

// listPendingSearchDocumentScopesQuery selects active repository scopes that
// have indexed content but no complete curated search-document projection for
// their active generation. The content EXISTS clause keeps empty repositories
// out of the sweep so they are not re-enqueued forever. Completion is a ready
// projection-state row plus an index-stat row with the same document count;
// unlike fact presence, that contract represents valid zero-document output.
const listPendingSearchDocumentScopesQuery = `
SELECT s.scope_id, s.active_generation_id, COALESCE(s.source_system, '')
FROM ingestion_scopes s
WHERE s.scope_kind = 'repository'
  AND s.active_generation_id IS NOT NULL
  AND s.payload->>'repo_id' IS NOT NULL
  AND EXISTS (
        SELECT 1 FROM content_entities ce WHERE ce.repo_id = s.payload->>'repo_id'
        UNION ALL
        SELECT 1 FROM content_files cf WHERE cf.repo_id = s.payload->>'repo_id'
        LIMIT 1
      )
  AND NOT EXISTS (
        SELECT 1
        FROM eshu_search_document_projection_state projection
        JOIN eshu_search_index_stats idx
          ON idx.scope_id = projection.scope_id
         AND idx.generation_id = projection.generation_id
         AND projection.document_count = idx.document_count
        WHERE projection.scope_id = s.scope_id
          AND projection.generation_id = s.active_generation_id
          AND projection.state = 'ready'
      )
ORDER BY s.scope_id
LIMIT $1
`

// EshuSearchDocumentPendingStore lists repository scopes whose active generation
// needs a curated search-document projection. It implements
// projector.PendingSearchDocumentLister.
type EshuSearchDocumentPendingStore struct {
	db ExecQueryer
}

// NewEshuSearchDocumentPendingStore builds a pending-projection lister over db.
func NewEshuSearchDocumentPendingStore(db ExecQueryer) EshuSearchDocumentPendingStore {
	return EshuSearchDocumentPendingStore{db: db}
}

// ListPendingSearchDocumentScopes returns repository scopes with indexed content
// but no search-document projection for their active generation, bounded by
// limit.
func (s EshuSearchDocumentPendingStore) ListPendingSearchDocumentScopes(
	ctx context.Context,
	limit int,
) ([]projector.PendingSearchDocumentScope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("eshu search document pending store requires a database")
	}
	if limit <= 0 {
		limit = eshuSearchDocumentPendingDefaultLimit
	}
	if limit > eshuSearchDocumentPendingMaxLimit {
		limit = eshuSearchDocumentPendingMaxLimit
	}

	rows, err := s.db.QueryContext(ctx, listPendingSearchDocumentScopesQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending search document scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopes []projector.PendingSearchDocumentScope
	for rows.Next() {
		var scope projector.PendingSearchDocumentScope
		if err := rows.Scan(&scope.ScopeID, &scope.GenerationID, &scope.SourceSystem); err != nil {
			return nil, fmt.Errorf("scan pending search document scope: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending search document scopes: %w", err)
	}
	return scopes, nil
}
