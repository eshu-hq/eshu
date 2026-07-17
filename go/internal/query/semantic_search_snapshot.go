// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const loadSemanticSearchSnapshotQuery = `
SELECT
    scope.active_generation_id,
    projection.projection_revision,
    projection.document_count,
    vector.projection_revision,
    vector.build_fence,
    vector.state
FROM ingestion_scopes scope
JOIN eshu_search_document_projection_state projection
  ON projection.scope_id = scope.scope_id
 AND projection.generation_id = scope.active_generation_id
JOIN eshu_search_vector_scope_state vector
  ON vector.scope_id = scope.scope_id
 AND vector.generation_id = scope.active_generation_id
WHERE scope.scope_id = $1
  AND projection.state = 'ready'
  AND vector.provider_profile_id = $2
  AND vector.source_class = $3
  AND vector.embedding_model_id = $4
  AND vector.vector_index_version = $5
LIMIT 1
`

// SemanticSearchSnapshotRequest identifies the durable document/vector state
// whose in-process retrieval index may be reused.
type SemanticSearchSnapshotRequest struct {
	ScopeID            string
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
}

// SemanticSearchSnapshot is the exact invalidation identity for one active
// persisted search-document/vector corpus.
type SemanticSearchSnapshot struct {
	GenerationID               string
	DocumentProjectionRevision int64
	DocumentCount              int
	VectorProjectionRevision   int64
	VectorBuildFence           int64
	VectorState                string
}

// Cacheable reports whether the snapshot is a complete, revision-aligned
// vector corpus. Missing, building, failed, empty, or mismatched state must not
// enter the request-time retrieval cache.
func (s SemanticSearchSnapshot) Cacheable() bool {
	return strings.TrimSpace(s.GenerationID) != "" &&
		s.DocumentCount > 0 &&
		s.DocumentProjectionRevision > 0 &&
		s.VectorProjectionRevision == s.DocumentProjectionRevision &&
		s.VectorBuildFence > 0 &&
		s.VectorState == "ready"
}

// SemanticSearchSnapshotStore loads the active durable invalidation identity
// before a cached semantic/hybrid search.
type SemanticSearchSnapshotStore interface {
	Load(context.Context, SemanticSearchSnapshotRequest) (SemanticSearchSnapshot, error)
}

// PostgresSemanticSearchSnapshotStore reads the active document/vector
// revision tuple through the relational projection state.
type PostgresSemanticSearchSnapshotStore struct {
	db pgstatus.Queryer
}

// NewPostgresSemanticSearchSnapshotStore constructs the production snapshot
// reader.
func NewPostgresSemanticSearchSnapshotStore(db pgstatus.Queryer) PostgresSemanticSearchSnapshotStore {
	return PostgresSemanticSearchSnapshotStore{db: db}
}

// Load returns an empty, non-cacheable snapshot when the active document or
// vector state is absent or unready.
func (s PostgresSemanticSearchSnapshotStore) Load(
	ctx context.Context,
	request SemanticSearchSnapshotRequest,
) (SemanticSearchSnapshot, error) {
	if s.db == nil {
		return SemanticSearchSnapshot{}, fmt.Errorf("semantic search snapshot store requires a database")
	}
	request = normalizeSemanticSearchSnapshotRequest(request)
	if err := validateSemanticSearchSnapshotRequest(request); err != nil {
		return SemanticSearchSnapshot{}, err
	}
	rows, err := s.db.QueryContext(
		ctx,
		loadSemanticSearchSnapshotQuery,
		request.ScopeID,
		request.ProviderProfileID,
		request.SourceClass,
		request.EmbeddingModelID,
		request.VectorIndexVersion,
	)
	if err != nil {
		return SemanticSearchSnapshot{}, fmt.Errorf("load semantic search snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return SemanticSearchSnapshot{}, fmt.Errorf("iterate semantic search snapshot: %w", err)
		}
		return SemanticSearchSnapshot{}, nil
	}
	var snapshot SemanticSearchSnapshot
	if err := rows.Scan(
		&snapshot.GenerationID,
		&snapshot.DocumentProjectionRevision,
		&snapshot.DocumentCount,
		&snapshot.VectorProjectionRevision,
		&snapshot.VectorBuildFence,
		&snapshot.VectorState,
	); err != nil {
		return SemanticSearchSnapshot{}, fmt.Errorf("scan semantic search snapshot: %w", err)
	}
	if err := rows.Err(); err != nil {
		return SemanticSearchSnapshot{}, fmt.Errorf("iterate semantic search snapshot: %w", err)
	}
	return snapshot, nil
}

func normalizeSemanticSearchSnapshotRequest(
	request SemanticSearchSnapshotRequest,
) SemanticSearchSnapshotRequest {
	request.ScopeID = strings.TrimSpace(request.ScopeID)
	request.ProviderProfileID = strings.TrimSpace(request.ProviderProfileID)
	request.SourceClass = strings.TrimSpace(request.SourceClass)
	request.EmbeddingModelID = strings.TrimSpace(request.EmbeddingModelID)
	request.VectorIndexVersion = strings.TrimSpace(request.VectorIndexVersion)
	return request
}

func validateSemanticSearchSnapshotRequest(request SemanticSearchSnapshotRequest) error {
	if request.ScopeID == "" {
		return fmt.Errorf("semantic search snapshot requires a scope id")
	}
	if request.ProviderProfileID == "" || request.SourceClass == "" ||
		request.EmbeddingModelID == "" || request.VectorIndexVersion == "" {
		return fmt.Errorf("semantic search snapshot requires a complete vector identity")
	}
	return nil
}
