// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// ContainerImageIdentityHeldSupportStore reads the bounded prior authority
// needed only when collector completeness holds an exact image reference.
type ContainerImageIdentityHeldSupportStore struct {
	db Queryer
}

// NewContainerImageIdentityHeldSupportStore constructs the bounded prior
// support reader.
func NewContainerImageIdentityHeldSupportStore(
	db Queryer,
) ContainerImageIdentityHeldSupportStore {
	return ContainerImageIdentityHeldSupportStore{db: db}
}

// LoadHeldContainerImageIdentitySupports loads supports from the exact active
// typed set, or from active-generation legacy facts before the first v3
// publication. It never falls back to a superseded last_set_id.
func (s ContainerImageIdentityHeldSupportStore) LoadHeldContainerImageIdentitySupports(
	ctx context.Context,
	scopeID string,
	generationID string,
	activationEpoch int64,
	imageRefs []string,
) ([]reducer.ContainerImageIdentityPriorSupport, error) {
	if s.db == nil {
		return nil, fmt.Errorf("container image identity held support database is required")
	}
	if len(imageRefs) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(
		ctx,
		containerImageIdentityHeldSupportQuery,
		scopeID,
		generationID,
		activationEpoch,
		pgarray.Array(imageRefs),
	)
	if err != nil {
		return nil, fmt.Errorf("query held container image identity supports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	supports := make([]reducer.ContainerImageIdentityPriorSupport, 0)
	for rows.Next() {
		var support reducer.ContainerImageIdentityPriorSupport
		if err := rows.Scan(
			&support.Digest,
			&support.ImageRef,
			&support.RepositoryID,
			&support.Outcome,
			&support.IdentityStrength,
			&support.SourceRevision,
			&support.SourceRevisionProvenance,
			&support.Reason,
			&support.CanonicalWrites,
			pgarray.Array(&support.SourceRepositoryIDs),
			pgarray.Array(&support.BuildProvenanceRepositoryIDs),
			pgarray.Array(&support.BaseImageForRepositoryIDs),
			pgarray.Array(&support.WorkloadIDs),
			pgarray.Array(&support.ServiceIDs),
			pgarray.Array(&support.SourceLayers),
			pgarray.Array(&support.EvidenceFactIDs),
			pgarray.Array(&support.MissingEvidence),
		); err != nil {
			return nil, fmt.Errorf("scan held container image identity support: %w", err)
		}
		supports = append(supports, support)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read held container image identity supports: %w", err)
	}
	return supports, nil
}

const containerImageIdentityHeldSupportQuery = `
WITH current_state AS MATERIALIZED (
    SELECT state.active_set_id
    FROM container_image_identity_scope_state AS state
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = state.scope_id
     AND scope.active_generation_id = state.active_generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = state.scope_id
     AND generation.generation_id = state.active_generation_id
     AND generation.status = 'active'
    WHERE state.scope_id = $1
      AND state.active_generation_id = $2
      AND state.activation_epoch = $3
),
typed_supports AS (
    SELECT
        support.digest,
        support.image_ref,
        support.repository_id,
        support.outcome,
        support.identity_strength,
        support.source_revision,
        support.source_revision_provenance,
        support.reason,
        support.canonical_writes,
        support.source_repository_ids,
        support.build_provenance_repository_ids,
        support.base_image_for_repository_ids,
        support.workload_ids,
        support.service_ids,
        support.source_layers,
        support.evidence_fact_ids,
        support.missing_evidence
    FROM current_state AS state
    JOIN container_image_identity_supports AS support
      ON support.set_id = state.active_set_id
    WHERE state.active_set_id IS NOT NULL
      AND support.image_ref = ANY($4::TEXT[])
),
legacy_supports AS (
    SELECT
        btrim(COALESCE(fact.payload->>'digest', '')) AS digest,
        btrim(COALESCE(fact.payload->>'image_ref', '')) AS image_ref,
        btrim(COALESCE(fact.payload->>'repository_id', '')) AS repository_id,
        btrim(COALESCE(fact.payload->>'outcome', '')) AS outcome,
        btrim(COALESCE(fact.payload->>'identity_strength', '')) AS identity_strength,
        btrim(COALESCE(fact.payload->>'source_revision', '')) AS source_revision,
        btrim(COALESCE(fact.payload->>'source_revision_provenance', '')) AS source_revision_provenance,
        btrim(COALESCE(fact.payload->>'reason', '')) AS reason,
        CASE
            WHEN COALESCE(fact.payload->>'canonical_writes', '') ~ '^[0-9]+$'
                THEN (fact.payload->>'canonical_writes')::INTEGER
            ELSE 0
        END AS canonical_writes,
        CASE WHEN jsonb_typeof(fact.payload->'source_repository_ids') = 'array'
            THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'source_repository_ids'))
            ELSE '{}'::TEXT[] END AS source_repository_ids,
        CASE WHEN jsonb_typeof(fact.payload->'build_provenance_repository_ids') = 'array'
            THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'build_provenance_repository_ids'))
            ELSE '{}'::TEXT[] END AS build_provenance_repository_ids,
        CASE WHEN jsonb_typeof(fact.payload->'base_image_for_repository_ids') = 'array'
            THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'base_image_for_repository_ids'))
            ELSE '{}'::TEXT[] END AS base_image_for_repository_ids,
        CASE WHEN jsonb_typeof(fact.payload->'workload_ids') = 'array'
            THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'workload_ids'))
            ELSE '{}'::TEXT[] END AS workload_ids,
        CASE WHEN jsonb_typeof(fact.payload->'service_ids') = 'array'
            THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'service_ids'))
            ELSE '{}'::TEXT[] END AS service_ids,
        CASE WHEN jsonb_typeof(fact.payload->'source_layers') = 'array'
            THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'source_layers'))
            ELSE '{}'::TEXT[] END AS source_layers,
        CASE WHEN jsonb_typeof(fact.payload->'evidence_fact_ids') = 'array'
            THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'evidence_fact_ids'))
            ELSE '{}'::TEXT[] END AS evidence_fact_ids,
        CASE WHEN jsonb_typeof(fact.payload->'missing_evidence') = 'array'
            THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'missing_evidence'))
            ELSE '{}'::TEXT[] END AS missing_evidence
    FROM current_state AS state
    JOIN fact_records AS fact
      ON fact.scope_id = $1
     AND fact.generation_id = $2
     AND fact.fact_kind = 'reducer_container_image_identity'
     AND NOT fact.is_tombstone
    WHERE state.active_set_id IS NULL
      AND btrim(COALESCE(fact.payload->>'digest', '')) <> ''
      AND btrim(COALESCE(fact.payload->>'image_ref', '')) = ANY($4::TEXT[])
)
SELECT * FROM typed_supports
UNION ALL
SELECT * FROM legacy_supports
ORDER BY digest, image_ref, repository_id, outcome
`
