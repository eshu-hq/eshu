// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index"
)

// PostgresSemanticSearchIndexStore adapts the durable Postgres search index to
// the query-layer semantic-search port.
type PostgresSemanticSearchIndexStore struct {
	db db.Queryer
}

// NewPostgresSemanticSearchIndexStore creates a Postgres-backed persisted
// search-index reader for semantic search.
func NewPostgresSemanticSearchIndexStore(db *sql.DB) PostgresSemanticSearchIndexStore {
	return PostgresSemanticSearchIndexStore{db: postgres.NewSQLReadStore(db)}
}

// NewPostgresSemanticSearchIndexStoreWithReadStore reads the persisted index
// and active curated documents through one guarded query-only connection.
func NewPostgresSemanticSearchIndexStoreWithReadStore(reader db.Queryer) PostgresSemanticSearchIndexStore {
	return PostgresSemanticSearchIndexStore{db: reader}
}

// Search returns persisted-index candidates for one bounded repository corpus.
func (s PostgresSemanticSearchIndexStore) Search(
	ctx context.Context,
	query SemanticSearchIndexQuery,
) (SemanticSearchIndexResult, error) {
	if s.db == nil {
		return SemanticSearchIndexResult{}, fmt.Errorf("semantic search index database is required")
	}
	result, err := indexstore.NewEshuSearchIndexReader(s.db).Search(
		ctx,
		indexstore.EshuSearchIndexSearch{
			ScopeID:     query.ScopeID,
			RepoID:      query.RepoID,
			Query:       query.Request.Query,
			Anchor:      query.Request.Scope.Anchor(),
			SourceKinds: query.SourceKinds,
			Languages:   query.Languages,
			Limit:       query.Request.Limit + 1,
		},
	)
	if err != nil {
		return SemanticSearchIndexResult{}, err
	}
	return SemanticSearchIndexResult{
		Candidates:           result.Candidates,
		IndexedDocumentCount: result.IndexedDocumentCount,
		CorpusMayBeTruncated: result.CorpusMayBeTruncated,
	}, nil
}

// ListActiveDocuments returns active curated documents for request-time local
// hybrid retrieval.
func (s PostgresSemanticSearchIndexStore) ListActiveDocuments(
	ctx context.Context,
	query semanticSearchDocumentQuery,
) ([]semanticSearchDocumentRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("semantic search document database is required")
	}
	rows, err := postgres.NewEshuSearchDocumentReader(s.db).ListActiveDocuments(
		ctx,
		postgres.EshuSearchDocumentFilter{
			ScopeID:     query.ScopeID,
			RepoID:      query.RepoID,
			SourceKinds: query.SourceKinds,
			Languages:   query.Languages,
			Limit:       query.Limit,
		},
	)
	if err != nil {
		return nil, err
	}
	documents := make([]semanticSearchDocumentRow, 0, len(rows))
	for _, row := range rows {
		documents = append(documents, semanticSearchDocumentRow{Document: row.Document})
	}
	return documents, nil
}
