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

// listPendingDomainPartitionIntentsSQL fetches up to $4 pending intents for
// one domain partition.  Refresh intents sort before ALL upsert intents via
// is_refresh_intent DESC as the primary sort key — the generated BOOLEAN
// column (payload->>'action' = 'refresh') is stored alongside the row so the
// planner can use index
// shared_projection_intents_domain_partition_refresh_first_idx
// (projection_domain, is_refresh_intent DESC, created_at ASC, intent_id ASC)
// and avoid a full sort on large pending backlogs (#3451, #3474).
//
// Using is_refresh_intent DESC as the PRIMARY sort key (before created_at)
// guarantees refresh intents enter every batch regardless of their created_at
// relative to the head upsert edges.  The #3451 ordering (created_at ASC,
// is_refresh_intent DESC) was only a same-timestamp tiebreaker: when deferred
// upsert edges are older than the paired refresh intent, they re-fill the
// batch head every cycle and permanently starve the refresh (#3474).
const listPendingDomainPartitionIntentsSQL = `
SELECT intent_id, projection_domain, partition_key, scope_id,
       acceptance_unit_id, repository_id,
       source_run_id, generation_id, payload, created_at, completed_at
FROM shared_projection_intents
WHERE projection_domain = $1
  AND partition_hash IS NOT NULL
  AND mod(partition_hash, $3::numeric) = $2::numeric
  AND completed_at IS NULL
ORDER BY is_refresh_intent DESC,
         created_at ASC,
         intent_id ASC
LIMIT $4
`

// listPendingDomainUnhashedIntentsSQL fetches legacy NULL-partition_hash rows
// for one domain.  The same refresh-first primary ordering applies so a
// refresh intent emitted before partition hashing was backfilled cannot be
// starved by older deferred upsert edges during a migration window (#3474).
const listPendingDomainUnhashedIntentsSQL = `
SELECT intent_id, projection_domain, partition_key, scope_id,
       acceptance_unit_id, repository_id,
       source_run_id, generation_id, payload, created_at, completed_at
FROM shared_projection_intents
WHERE projection_domain = $1
  AND partition_hash IS NULL
  AND completed_at IS NULL
ORDER BY is_refresh_intent DESC, created_at ASC, intent_id ASC
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
