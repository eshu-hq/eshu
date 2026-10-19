// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// reopenDeployableUnitCorrelationsForReposQuery is the completion-driven
// wake (#8294): one UPDATE that reopens every succeeded
// deployable_unit_correlation item whose recorded fingerprint repo_ids
// overlap the appended resolved rows' repos. It composes the shared
// succeeded-to-pending SET clause (#7731) so the wake resets exactly the
// columns the reducer claim path reads, and it lists nothing: the overlap
// predicate against the fingerprint rows bounds the write set in the
// statement itself.
//
// Concurrency contract. The UPDATE touches only status = 'succeeded' rows.
// The reducer claim path (claimReducerWorkQuery) selects only
// pending/retrying/claimed/running FOR UPDATE SKIP LOCKED, so the two never
// lock the same row and no claim deadlocks against a wake. Leases are never
// stolen: claimed/running rows are excluded by the status predicate, and a
// succeeded row's lease columns are already NULL. The write is idempotent: a
// second wake over the same repos matches zero rows because the first moved
// them to pending, and a concurrent maintenance-pass reopen of the same item
// converges the same way. Duplicate resolution delivery is safe end to end:
// the insert is ON CONFLICT DO NOTHING (a re-run reports zero inserted and
// skips the wake) and the wake only moves succeeded rows. No worker count,
// batch size, or claim shape changes: this is one extra statement per
// resolution that actually appended rows.
const reopenDeployableUnitCorrelationsForReposQuery = `
UPDATE fact_work_items
SET ` + ReopenSucceededReducerSetClause + `
WHERE stage = 'reducer'
  AND domain = 'deployable_unit_correlation'
  AND status = 'succeeded'
  AND EXISTS (
    SELECT 1
    FROM deployable_unit_reopen_fingerprints AS fingerprint
    WHERE fingerprint.work_item_id = fact_work_items.work_item_id
      AND fingerprint.repo_ids && $2::text[]
  )
`

// ReopenDeployableUnitCorrelationsForRepos reopens the succeeded
// deployable_unit_correlation items whose recorded input overlaps repoIDs:
// the completion half of the #8294 wake. The cross-repo resolver calls it
// with the inserted set UpsertResolved reported, in the same pass that
// appended the rows, so a dependent item that succeeded earlier in the pass
// re-reads the appended rows instead of waiting for the next maintenance
// pass. It returns how many items reopened. Empty input reopens nothing
// without touching the database.
func (s *RelationshipStore) ReopenDeployableUnitCorrelationsForRepos(
	ctx context.Context,
	repoIDs []string,
) (int64, error) {
	repos := uniqueNonEmptyStrings(repoIDs)
	if len(repos) == 0 {
		return 0, nil
	}
	result, err := s.database.ExecContext(
		ctx,
		reopenDeployableUnitCorrelationsForReposQuery,
		time.Now().UTC(),
		array.StringArray(repos),
	)
	if err != nil {
		return 0, fmt.Errorf("reopen deployable_unit_correlation items for appended resolved rows: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reopen deployable_unit_correlation items for appended resolved rows: rows affected: %w", err)
	}
	return affected, nil
}
