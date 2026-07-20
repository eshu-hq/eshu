// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const listActiveSBOMAttestationAttachmentFactsQuery = `
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
    'oci_registry.image_referrer',
    'reducer_container_image_identity',
    'sbom.document',
    'sbom.component',
    'sbom.dependency_relationship',
    'sbom.external_reference',
    'attestation.statement',
    'attestation.signature_verification',
    'attestation.slsa_provenance',
    'sbom.warning'
)
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      fact.payload->>'subject_digest' = ANY($1::text[])
      OR fact.payload->>'digest' = ANY($1::text[])
      OR fact.payload->>'referrer_digest' = ANY($1::text[])
      OR fact.payload->>'document_digest' = ANY($1::text[])
      OR fact.payload->>'document_id' = ANY($1::text[])
      OR fact.payload->>'statement_digest' = ANY($1::text[])
      OR fact.payload->>'payload_digest' = ANY($1::text[])
      OR fact.payload->>'statement_id' = ANY($1::text[])
  )
  AND ($2 = '' OR fact.fact_id > $2)
ORDER BY fact.fact_id ASC
LIMIT $3
`

// ListActiveSBOMAttestationAttachmentFacts loads active document, referrer, and
// image identity rows for digest or document keys observed in one SBOM/attestation
// generation.
func (s FactStore) ListActiveSBOMAttestationAttachmentFacts(
	ctx context.Context,
	digests []string,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	digests = cleanStringFilterValues(digests)
	if len(digests) == 0 {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorFactID string
	for {
		page, err := s.listActiveSBOMAttestationAttachmentFactsPage(ctx, digests, cursorFactID)
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

func (s FactStore) listActiveSBOMAttestationAttachmentFactsPage(
	ctx context.Context,
	digests []string,
	cursorFactID string,
) ([]facts.Envelope, error) {
	rows, err := s.db.QueryContext(
		ctx,
		listActiveSBOMAttestationAttachmentFactsQuery,
		digests,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active sbom attestation attachment facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, len(digests))
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active sbom attestation attachment facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active sbom attestation attachment facts: %w", err)
	}
	return loaded, nil
}
