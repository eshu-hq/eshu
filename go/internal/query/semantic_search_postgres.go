package query

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// PostgresSemanticSearchIndexStore adapts the durable Postgres search index to
// the query-layer semantic-search port.
type PostgresSemanticSearchIndexStore struct {
	db *sql.DB
}

// NewPostgresSemanticSearchIndexStore creates a Postgres-backed persisted
// search-index reader for semantic search.
func NewPostgresSemanticSearchIndexStore(db *sql.DB) PostgresSemanticSearchIndexStore {
	return PostgresSemanticSearchIndexStore{db: db}
}

// Search returns persisted-index candidates for one bounded repository corpus.
func (s PostgresSemanticSearchIndexStore) Search(
	ctx context.Context,
	query semanticSearchIndexQuery,
) (semanticSearchIndexResult, error) {
	if s.db == nil {
		return semanticSearchIndexResult{}, fmt.Errorf("semantic search index database is required")
	}
	result, err := postgres.NewEshuSearchIndexStore(postgres.SQLDB{DB: s.db}).Search(
		ctx,
		postgres.EshuSearchIndexSearch{
			ScopeID:     query.ScopeID,
			RepoID:      query.RepoID,
			Query:       query.Request.Query,
			Anchor:      query.Request.Scope.Anchor(),
			SourceKinds: query.SourceKinds,
			Limit:       query.Request.Limit + 1,
		},
	)
	if err != nil {
		return semanticSearchIndexResult{}, err
	}
	return semanticSearchIndexResult{
		Candidates:           result.Candidates,
		IndexedDocumentCount: result.IndexedDocumentCount,
		CorpusMayBeTruncated: result.CorpusMayBeTruncated,
	}, nil
}
