package query

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// PostgresIaCReachabilityStore adapts reducer-materialized Postgres rows to
// the query package's stable IaC response contract.
type PostgresIaCReachabilityStore struct {
	store *postgres.IaCReachabilityStore
}

// NewPostgresIaCReachabilityStore creates a query adapter over the reducer's
// IaC reachability table.
func NewPostgresIaCReachabilityStore(db *sql.DB) *PostgresIaCReachabilityStore {
	return &PostgresIaCReachabilityStore{store: postgres.NewIaCReachabilityStore(postgres.SQLDB{DB: db})}
}

// ListLatestCleanupFindings returns active-generation cleanup rows for the
// requested repository ids.
func (s *PostgresIaCReachabilityStore) ListLatestCleanupFindings(
	ctx context.Context,
	repoIDs []string,
	families []string,
	includeAmbiguous bool,
	limit int,
	offset int,
) ([]IaCReachabilityFindingRow, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	rows, err := s.store.ListLatestCleanupFindings(ctx, repoIDs, families, includeAmbiguous, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]IaCReachabilityFindingRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, IaCReachabilityFindingRow{
			ID:           row.Family + ":" + row.RepoID + ":" + row.ArtifactPath,
			Family:       row.Family,
			RepoID:       row.RepoID,
			ArtifactPath: row.ArtifactPath,
			Reachability: string(row.Reachability),
			Finding:      string(row.Finding),
			Confidence:   row.Confidence,
			Evidence:     append([]string(nil), row.Evidence...),
			Limitations:  append([]string(nil), row.Limitations...),
		})
	}
	return result, nil
}

// CountLatestCleanupFindings returns the full active-generation cleanup count
// for the same filters used by ListLatestCleanupFindings.
func (s *PostgresIaCReachabilityStore) CountLatestCleanupFindings(
	ctx context.Context,
	repoIDs []string,
	families []string,
	includeAmbiguous bool,
) (int, error) {
	if s == nil || s.store == nil {
		return 0, nil
	}
	return s.store.CountLatestCleanupFindings(ctx, repoIDs, families, includeAmbiguous)
}

// HasLatestRows reports whether active-generation IaC reachability has been
// materialized for the requested repository ids and optional families.
func (s *PostgresIaCReachabilityStore) HasLatestRows(
	ctx context.Context,
	repoIDs []string,
	families []string,
) (bool, error) {
	if s == nil || s.store == nil {
		return false, nil
	}
	return s.store.HasLatestRows(ctx, repoIDs, families)
}
