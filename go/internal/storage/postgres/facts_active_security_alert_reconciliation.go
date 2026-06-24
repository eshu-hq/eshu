// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const listActiveSecurityAlertReconciliationFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind IN (
    'security_alert.repository_alert',
    'reducer_package_consumption_correlation',
    'reducer_supply_chain_impact_finding'
)
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      fact.payload->>'repository_id' = ANY($1::text[])
      OR fact.payload->>'package_id' = ANY($2::text[])
      OR fact.payload->'cve_ids' ?| $3::text[]
      OR fact.payload->'ghsa_ids' ?| $4::text[]
      OR fact.payload->>'cve_id' = ANY($3::text[])
      OR fact.payload->>'advisory_id' = ANY($4::text[])
  )
  AND ($5 = '' OR fact.fact_id > $5)
ORDER BY fact.fact_id ASC
LIMIT $6
`

// ListActiveSecurityAlertReconciliationFacts loads active owned dependency and
// reducer impact evidence used to compare provider-reported repository alerts.
func (s FactStore) ListActiveSecurityAlertReconciliationFacts(
	ctx context.Context,
	filter reducer.SecurityAlertReconciliationFactFilter,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	filter.RepositoryIDs = cleanStringFilterValues(filter.RepositoryIDs)
	filter.PackageIDs = cleanStringFilterValues(filter.PackageIDs)
	filter.CVEIDs = cleanStringFilterValues(filter.CVEIDs)
	filter.GHSAIDs = cleanStringFilterValues(filter.GHSAIDs)
	if len(filter.RepositoryIDs) == 0 && len(filter.PackageIDs) == 0 &&
		len(filter.CVEIDs) == 0 && len(filter.GHSAIDs) == 0 {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorFactID string
	for {
		page, err := s.listActiveSecurityAlertReconciliationFactsPage(ctx, filter, cursorFactID)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}
		cursorFactID = page[len(page)-1].FactID
	}
}

func (s FactStore) listActiveSecurityAlertReconciliationFactsPage(
	ctx context.Context,
	filter reducer.SecurityAlertReconciliationFactFilter,
	cursorFactID string,
) ([]facts.Envelope, error) {
	rows, err := s.db.QueryContext(
		ctx,
		listActiveSecurityAlertReconciliationFactsQuery,
		filter.RepositoryIDs,
		filter.PackageIDs,
		filter.CVEIDs,
		filter.GHSAIDs,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0,
		len(filter.RepositoryIDs)+len(filter.PackageIDs)+len(filter.CVEIDs)+len(filter.GHSAIDs))
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active security alert reconciliation facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", err)
	}
	return loaded, nil
}
