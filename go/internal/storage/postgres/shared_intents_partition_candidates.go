package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The shared intent store backs both the dedicated code-call runner and the
// generic shared projection runner, so it must satisfy the indexed and unhashed
// candidate reader contracts for each. Locking these at compile time prevents a
// future signature drift from silently dropping the generic runner back to the
// in-memory domain scan.
var (
	_ reducer.CodeCallProjectionPartitionCandidateReader = (*SharedIntentStore)(nil)
	_ reducer.CodeCallProjectionUnhashedCandidateReader  = (*SharedIntentStore)(nil)
	_ reducer.CodeCallProjectionRefreshFenceLookup       = (*SharedIntentStore)(nil)
	_ reducer.SharedProjectionPartitionCandidateReader   = (*SharedIntentStore)(nil)
	_ reducer.SharedProjectionUnhashedCandidateReader    = (*SharedIntentStore)(nil)
)

const listPendingDomainPartitionIntentsSQL = `
SELECT intent_id, projection_domain, partition_key, scope_id,
       acceptance_unit_id, repository_id,
       source_run_id, generation_id, payload, created_at, completed_at
FROM shared_projection_intents
WHERE projection_domain = $1
  AND partition_hash IS NOT NULL
  AND mod(partition_hash, $3::numeric) = $2::numeric
  AND completed_at IS NULL
ORDER BY created_at ASC, intent_id ASC
LIMIT $4
`

const listPendingDomainUnhashedIntentsSQL = `
SELECT intent_id, projection_domain, partition_key, scope_id,
       acceptance_unit_id, repository_id,
       source_run_id, generation_id, payload, created_at, completed_at
FROM shared_projection_intents
WHERE projection_domain = $1
  AND partition_hash IS NULL
  AND completed_at IS NULL
ORDER BY created_at ASC, intent_id ASC
LIMIT $2
`

// ListPendingDomainPartitionIntents lists pending shared intents for one domain
// whose stored partition hash belongs to the selected worker partition.
func (s *SharedIntentStore) ListPendingDomainPartitionIntents(
	ctx context.Context,
	domain string,
	partitionID int,
	partitionCount int,
	limit int,
) ([]reducer.SharedProjectionIntentRow, error) {
	if partitionCount <= 0 {
		return nil, fmt.Errorf("partitionCount must be positive, got %d", partitionCount)
	}
	if partitionID < 0 || partitionID >= partitionCount {
		return nil, fmt.Errorf("partitionID %d outside [0, %d)", partitionID, partitionCount)
	}
	l := max(limit, 1)

	sqlRows, err := s.db.QueryContext(
		ctx,
		listPendingDomainPartitionIntentsSQL,
		domain,
		partitionID,
		partitionCount,
		l,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	return scanSharedIntentRows(sqlRows)
}

// ListPendingDomainUnhashedIntents lists pending legacy shared intents for one
// domain that do not yet have a stored partition hash.
func (s *SharedIntentStore) ListPendingDomainUnhashedIntents(
	ctx context.Context,
	domain string,
	limit int,
) ([]reducer.SharedProjectionIntentRow, error) {
	l := max(limit, 1)

	sqlRows, err := s.db.QueryContext(ctx, listPendingDomainUnhashedIntentsSQL, domain, l)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	return scanSharedIntentRows(sqlRows)
}
