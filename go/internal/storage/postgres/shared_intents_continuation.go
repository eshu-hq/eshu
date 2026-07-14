// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const listPendingDomainIntentsAfterSQL = `
SELECT intent_id, projection_domain, partition_key, scope_id,
       acceptance_unit_id, repository_id,
       source_run_id, generation_id, payload, created_at, completed_at
FROM shared_projection_intents
WHERE projection_domain = $1
  AND completed_at IS NULL
  AND (created_at, intent_id) > ($2, $3)
ORDER BY created_at ASC, intent_id ASC
LIMIT $4
`

// ListPendingDomainIntentsAfter continues the deterministic pending-domain
// ordering after one (created_at, intent_id) cursor. Repo-dependency shards use
// it to avoid treating a full page owned by other repositories as exhaustion.
func (s *SharedIntentStore) ListPendingDomainIntentsAfter(
	ctx context.Context,
	domain string,
	afterCreatedAt time.Time,
	afterIntentID string,
	limit int,
) ([]reducer.SharedProjectionIntentRow, error) {
	l := max(limit, 1)
	sqlRows, err := s.db.QueryContext(
		ctx,
		listPendingDomainIntentsAfterSQL,
		domain,
		afterCreatedAt,
		afterIntentID,
		l,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	return scanSharedIntentRows(sqlRows)
}
