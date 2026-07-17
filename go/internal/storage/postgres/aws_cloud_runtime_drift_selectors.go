// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/lib/pq"
)

const (
	awsReplatformingSelectorDefaultLimit = 100
	awsReplatformingSelectorMaxLimit     = 200
)

// AWSReplatformingScopeSelectorRow is one active AWS collector scope that can
// bound a replatforming review. FindingCount may be zero; an active collected
// scope with no current drift findings is an authoritative empty choice, not an
// absent selector.
type AWSReplatformingScopeSelectorRow struct {
	ScopeID      string
	AccountID    string
	Region       string
	Service      string
	FindingCount int
}

// AWSReplatformingScopeSelectorPage is one bounded, deterministic selector page.
type AWSReplatformingScopeSelectorPage struct {
	Scopes    []AWSReplatformingScopeSelectorRow
	Truncated bool
}

// ListActiveReplatformingScopes lists active AWS collector scopes and the
// current active-generation drift-finding count for each scope. It reads the
// small ingestion-scope inventory first and uses the existing
// (scope_id, generation_id, fact_kind) fact index for each count, avoiding an
// all-finding DISTINCT scan. The limit is capped and fetched with one lookahead
// row so truncation is explicit.
func (s AWSCloudRuntimeDriftFindingStore) ListActiveReplatformingScopes(
	ctx context.Context,
	limit int,
	allowedScopeIDs []string,
) (AWSReplatformingScopeSelectorPage, error) {
	if s.db == nil {
		return AWSReplatformingScopeSelectorPage{}, fmt.Errorf("aws cloud runtime drift finding store database is required")
	}
	if limit <= 0 {
		limit = awsReplatformingSelectorDefaultLimit
	}
	if limit > awsReplatformingSelectorMaxLimit {
		limit = awsReplatformingSelectorMaxLimit
	}
	query := listActiveReplatformingScopesSQL
	args := []any{AWSCloudRuntimeDriftFindingFactKind, limit + 1}
	if len(allowedScopeIDs) > 0 {
		query = listScopedActiveReplatformingScopesSQL
		args = []any{AWSCloudRuntimeDriftFindingFactKind, pq.StringArray(allowedScopeIDs), limit + 1}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return AWSReplatformingScopeSelectorPage{}, fmt.Errorf("list active AWS replatforming scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	scopes := make([]AWSReplatformingScopeSelectorRow, 0, limit+1)
	for rows.Next() {
		var row AWSReplatformingScopeSelectorRow
		if err := rows.Scan(&row.ScopeID, &row.AccountID, &row.Region, &row.Service, &row.FindingCount); err != nil {
			return AWSReplatformingScopeSelectorPage{}, fmt.Errorf("scan active AWS replatforming scope: %w", err)
		}
		scopes = append(scopes, row)
	}
	if err := rows.Err(); err != nil {
		return AWSReplatformingScopeSelectorPage{}, fmt.Errorf("iterate active AWS replatforming scopes: %w", err)
	}
	truncated := len(scopes) > limit
	if truncated {
		scopes = scopes[:limit]
	}
	return AWSReplatformingScopeSelectorPage{Scopes: scopes, Truncated: truncated}, nil
}

const listActiveReplatformingScopesSQL = `
SELECT
    scope.scope_id,
    split_part(scope.scope_id, ':', 2) AS account_id,
    split_part(scope.scope_id, ':', 3) AS region,
    split_part(scope.scope_id, ':', 4) AS service,
    (
        SELECT COUNT(*)
        FROM fact_records AS fact
        WHERE fact.scope_id = scope.scope_id
          AND fact.generation_id = scope.active_generation_id
          AND fact.fact_kind = $1
          AND fact.is_tombstone = false
    ) AS finding_count
FROM ingestion_scopes AS scope
WHERE scope.source_system = 'aws'
  AND scope.collector_kind = 'aws'
  AND scope.scope_kind = 'region'
  AND scope.status = 'active'
  AND scope.active_generation_id IS NOT NULL
  AND scope.scope_id ~ '^aws:[0-9]{12}:[a-z0-9-]+:[a-z0-9-]+$'
ORDER BY scope.scope_id
LIMIT $2
`

const listScopedActiveReplatformingScopesSQL = `
SELECT
    scope.scope_id,
    split_part(scope.scope_id, ':', 2) AS account_id,
    split_part(scope.scope_id, ':', 3) AS region,
    split_part(scope.scope_id, ':', 4) AS service,
    (
        SELECT COUNT(*)
        FROM fact_records AS fact
        WHERE fact.scope_id = scope.scope_id
          AND fact.generation_id = scope.active_generation_id
          AND fact.fact_kind = $1
          AND fact.is_tombstone = false
    ) AS finding_count
FROM ingestion_scopes AS scope
WHERE scope.source_system = 'aws'
  AND scope.collector_kind = 'aws'
  AND scope.scope_kind = 'region'
  AND scope.status = 'active'
  AND scope.active_generation_id IS NOT NULL
  AND scope.scope_id ~ '^aws:[0-9]{12}:[a-z0-9-]+:[a-z0-9-]+$'
  AND scope.scope_id = ANY($2)
ORDER BY scope.scope_id
LIMIT $3
`
