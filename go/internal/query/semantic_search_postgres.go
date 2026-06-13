package query

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// PostgresSemanticSearchDocumentStore adapts the durable Postgres search
// document reader to the query-layer semantic-search port.
type PostgresSemanticSearchDocumentStore struct {
	db *sql.DB
}

// NewPostgresSemanticSearchDocumentStore creates a Postgres-backed curated
// search-document reader for semantic search.
func NewPostgresSemanticSearchDocumentStore(db *sql.DB) PostgresSemanticSearchDocumentStore {
	return PostgresSemanticSearchDocumentStore{db: db}
}

// ListActiveDocuments returns active curated search documents for one bounded
// repository corpus.
func (s PostgresSemanticSearchDocumentStore) ListActiveDocuments(
	ctx context.Context,
	filter semanticSearchDocumentFilter,
) ([]semanticSearchDocumentRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("semantic search document database is required")
	}
	rows, err := postgres.NewEshuSearchDocumentStore(postgres.SQLDB{DB: s.db}).ListActiveDocuments(
		ctx,
		postgres.EshuSearchDocumentFilter{
			ScopeID:     filter.ScopeID,
			RepoID:      filter.RepoID,
			SourceKinds: filter.SourceKinds,
			Limit:       filter.Limit,
			Offset:      filter.Offset,
		},
	)
	if err != nil {
		return nil, err
	}
	out := make([]semanticSearchDocumentRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, semanticSearchDocumentRow{
			FactID:       row.FactID,
			ScopeID:      row.ScopeID,
			GenerationID: row.GenerationID,
			SourceSystem: row.SourceSystem,
			ObservedAt:   row.ObservedAt,
			Document:     row.Document,
		})
	}
	return out, nil
}
