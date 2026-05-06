package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const hasCompletedAcceptanceUnitDomainIntentsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM shared_projection_intents
    WHERE scope_id = $1
      AND acceptance_unit_id = $2
      AND projection_domain = $3
      AND completed_at IS NOT NULL
    LIMIT 1
)
`

// HasCompletedAcceptanceUnitDomainIntents reports whether any prior intent for
// the bounded acceptance unit and domain completed. It intentionally ignores
// source_run_id so new generations can see older completed graph projection
// state for the same accepted unit.
func (s *SharedIntentStore) HasCompletedAcceptanceUnitDomainIntents(
	ctx context.Context,
	key reducer.SharedProjectionAcceptanceKey,
	domain string,
) (bool, error) {
	sqlRows, err := s.db.QueryContext(
		ctx,
		hasCompletedAcceptanceUnitDomainIntentsSQL,
		key.ScopeID,
		key.AcceptanceUnitID,
		domain,
	)
	if err != nil {
		return false, fmt.Errorf("query completed shared projection history: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	if !sqlRows.Next() {
		return false, nil
	}
	var exists bool
	if err := sqlRows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan completed shared projection history: %w", err)
	}
	return exists, sqlRows.Err()
}
