// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const listActiveContainerImageSLSAFactsQuery = `
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
        'attestation.statement',
        'attestation.slsa_provenance',
        'attestation.signature_verification'
      )
  AND fact.source_system = 'sbom_attestation'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    $1::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $3
`

// ListActiveContainerImageSLSAFacts loads active attestation.statement,
// attestation.slsa_provenance, and attestation.signature_verification facts
// across SBOM-attestation-owned scopes (#5456 PR #5707 P1-b). The reducer's
// container_image_identity domain uses this as the cross-scope bridge to the
// SLSA provenance evidence: those facts are written by the SBOM-attestation
// collector in its own scope, a different scope than the OCI registry
// manifest (or Git/CI evidence) most container_image_identity refreshes run
// against, so a refresh triggered by any of those OTHER sources still needs
// to see currently-active SLSA evidence for the same digest. Without this,
// the slsa_provenance_commit identity tier could only ever apply within a
// same-scope refresh and would regress back to a weaker tier on the next
// independent OCI-only refresh.
//
// Mirrors ListActiveRepositoryFacts (facts_active_repository.go): a simple
// paginated cross-scope loader with its own dedicated partial index
// (fact_records_active_container_image_slsa_idx, migrations/075), no
// epoch-cache/fingerprint machinery — the identity-fact epoch cache
// (identity_epoch_cache.go) is reserved for the OCI/AWS/Azure/GCP/
// content_entity family this is deliberately kept separate from, so this
// change carries no risk to that cache's drift-locked partial-index
// contract (identity_epoch_cache_contract_test.go).
func (s FactStore) ListActiveContainerImageSLSAFacts(ctx context.Context) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActiveContainerImageSLSAFactsPage(ctx, cursorObservedAt, cursorFactID)
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

func (s FactStore) listActiveContainerImageSLSAFactsPage(
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
		listActiveContainerImageSLSAFactsQuery,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active container image SLSA facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active container image SLSA facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active container image SLSA facts: %w", err)
	}

	return loaded, nil
}
