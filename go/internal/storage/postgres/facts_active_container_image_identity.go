// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const listActiveContainerImageIdentityFactsQuery = `
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
WHERE (
    (
      fact.fact_kind IN ('oci_registry.image_tag_observation', 'oci_registry.image_manifest', 'oci_registry.image_index')
      AND fact.source_system = 'oci_registry'
    )
    OR (
      fact.fact_kind = 'aws_image_reference'
      AND fact.source_system = 'aws'
    )
    OR (
      fact.fact_kind = 'azure_image_reference'
      AND fact.source_system = 'azure'
    )
    OR (
      fact.fact_kind = 'gcp_image_reference'
      AND fact.source_system = 'gcp'
    )
    OR (
      fact.fact_kind = 'aws_relationship'
      AND fact.source_system = 'aws'
      AND fact.payload->>'target_type' = 'container_image'
    )
    OR (
      fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND (
        fact.payload->'entity_metadata' ? 'container_images'
        OR fact.payload->'metadata' ? 'container_images'
      )
    )
  )
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    $1::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $3
`

// ListActiveContainerImageIdentityFacts loads active OCI registry facts and
// active Git/AWS/Azure/GCP image-reference facts for cross-scope identity joins.
func (s FactStore) ListActiveContainerImageIdentityFacts(ctx context.Context) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActiveContainerImageIdentityFactsPage(ctx, cursorObservedAt, cursorFactID)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}

		last := page[len(page)-1]
		observedAt := last.ObservedAt.UTC()
		cursorObservedAt = &observedAt
		cursorFactID = last.FactID
	}
}

func (s FactStore) listActiveContainerImageIdentityFactsPage(
	ctx context.Context,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	rows, err := s.db.QueryContext(
		ctx,
		listActiveContainerImageIdentityFactsQuery,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active container image identity facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active container image identity facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active container image identity facts: %w", err)
	}

	return loaded, nil
}
