// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ContainerImageIdentityActivationEpochLookup returns the active lifecycle
// epoch captured before a handler reads evidence.
type ContainerImageIdentityActivationEpochLookup interface {
	ContainerImageIdentityActivationEpoch(context.Context, string, string) (int64, error)
}

// ContainerImageIdentityHeldSupportLoader loads only prior supports whose
// exact image references are protected by a completeness hold.
type ContainerImageIdentityHeldSupportLoader interface {
	LoadHeldContainerImageIdentitySupports(
		context.Context,
		string,
		string,
		int64,
		[]string,
	) ([]ContainerImageIdentityPriorSupport, error)
}

// PostgresContainerImageIdentitySupportWriter publishes anchor-free digest-v3
// truth into immutable typed support sets.
type PostgresContainerImageIdentitySupportWriter struct {
	ActivationLookup  ContainerImageIdentityActivationEpochLookup
	HeldSupportLoader ContainerImageIdentityHeldSupportLoader
	ClaimedExecer     ContainerImageIdentityClaimedExecer
	Now               func() time.Time
}

// ContainerImageIdentityActivationEpoch delegates the handler-time lifecycle
// snapshot to the Postgres state lookup wired with this writer.
func (w PostgresContainerImageIdentitySupportWriter) ContainerImageIdentityActivationEpoch(
	ctx context.Context,
	scopeID string,
	generationID string,
) (int64, error) {
	if w.ActivationLookup == nil {
		return 0, fmt.Errorf("container image identity activation lookup is required")
	}
	return w.ActivationLookup.ContainerImageIdentityActivationEpoch(ctx, scopeID, generationID)
}

// WriteContainerImageIdentityDecisions atomically installs a complete support
// set only while the exact queue claim and activation epoch remain current.
func (w PostgresContainerImageIdentitySupportWriter) WriteContainerImageIdentityDecisions(
	ctx context.Context,
	write ContainerImageIdentityWrite,
) (ContainerImageIdentityWriteResult, error) {
	if w.ClaimedExecer == nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("container image identity support claimed executor is required")
	}
	if err := validateContainerImageIdentitySupportWrite(write); err != nil {
		return ContainerImageIdentityWriteResult{}, err
	}
	prior, err := w.loadHeldContainerImageIdentitySupports(ctx, write)
	if err != nil {
		return ContainerImageIdentityWriteResult{}, err
	}
	supportSet, err := buildContainerImageIdentitySupportSet(write, prior)
	if err != nil {
		return ContainerImageIdentityWriteResult{}, err
	}
	supportJSON, err := json.Marshal(supportSet.Supports)
	if err != nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("marshal container image identity supports: %w", err)
	}
	now := reducerWriterNow(w.Now)
	legacyRowsDeleted, claimValid, err := w.ClaimedExecer.ExecContainerImageIdentityClaimed(
		ctx,
		containerImageIdentitySupportPublishQuery,
		supportSet.SetID,
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		supportSet.ContentHash,
		len(supportSet.Supports),
		strings.TrimSpace(write.IntentID),
		write.ClaimEpoch,
		write.ActivationEpoch,
		strings.TrimSpace(write.SourceSystem),
		reducerFactCollectorKind(write.SourceSystem),
		string(facts.SourceConfidenceInferred),
		strings.TrimSpace(write.IntentID),
		strings.TrimSpace(write.Cause),
		now,
		now,
		containerImageIdentityFencingToken(write),
		string(supportJSON),
	)
	if err != nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("publish container image identity support set: %w", err)
	}
	if !claimValid {
		return ContainerImageIdentityWriteResult{}, ErrContainerImageIdentityClaimRejected
	}
	result := containerImageIdentityWriteResult(
		supportSet.CurrentSupportCount,
		len(write.TombstoneDecisions),
		legacyRowsDeleted,
	)
	result.effectiveSupports = supportSet.Supports
	result.effectiveProjectionPresent = true
	return result, nil
}

func (w PostgresContainerImageIdentitySupportWriter) loadHeldContainerImageIdentitySupports(
	ctx context.Context,
	write ContainerImageIdentityWrite,
) ([]ContainerImageIdentityPriorSupport, error) {
	if len(write.HeldDecisions) == 0 {
		return nil, nil
	}
	if w.HeldSupportLoader == nil {
		return nil, fmt.Errorf("container image identity held support loader is required")
	}
	imageRefs := make([]string, 0, len(write.HeldDecisions))
	for _, decision := range write.HeldDecisions {
		imageRef := strings.TrimSpace(decision.ImageRef)
		if imageRef == "" {
			return nil, fmt.Errorf("container image identity held support image_ref is required")
		}
		imageRefs = append(imageRefs, imageRef)
	}
	prior, err := w.HeldSupportLoader.LoadHeldContainerImageIdentitySupports(
		ctx,
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		write.ActivationEpoch,
		uniqueSortedStrings(imageRefs),
	)
	if err != nil {
		return nil, fmt.Errorf("load held container image identity supports: %w", err)
	}
	return prior, nil
}

func validateContainerImageIdentitySupportWrite(write ContainerImageIdentityWrite) error {
	if err := validateContainerImageIdentityFence(write); err != nil {
		return err
	}
	if strings.TrimSpace(write.IntentID) == "" || strings.TrimSpace(write.ScopeID) == "" ||
		strings.TrimSpace(write.GenerationID) == "" {
		return errors.New("container image identity intent, scope, and generation are required")
	}
	if write.ClaimEpoch <= 0 {
		return errors.New("container image identity claim_epoch must be positive")
	}
	if write.ActivationEpoch <= 0 {
		return errors.New("container image identity activation_epoch must be positive")
	}
	return nil
}

const containerImageIdentitySupportPublishQuery = `
WITH current_state AS MATERIALIZED (
    SELECT state.scope_id
    FROM container_image_identity_scope_state AS state
    JOIN container_image_identity_storage_cutover AS cutover
      ON cutover.singleton
     AND cutover.identity_format = 'digest_v3'
    JOIN scope_generations AS generation
      ON generation.scope_id = state.scope_id
     AND generation.generation_id = state.active_generation_id
     AND generation.status = 'active'
    WHERE state.scope_id = $2
      AND state.active_generation_id = $3
      AND state.activation_epoch = $8
    FOR UPDATE OF state
),
current_claim AS MATERIALIZED (
    UPDATE fact_work_items AS work_item
    SET status = 'running',
        container_image_identity_v2_authorized_status = 'running',
        container_image_identity_v3_authorized_status = 'running'
    FROM current_state
    WHERE work_item.work_item_id = $6
      AND work_item.scope_id = $2
      AND work_item.generation_id = $3
      AND work_item.stage = 'reducer'
      AND work_item.domain = 'container_image_identity'
      AND work_item.status IN ('claimed', 'running')
      AND work_item.container_image_identity_claim_epoch = $7
      AND work_item.container_image_identity_v2_required
      AND work_item.container_image_identity_v2_authorized_status = work_item.status
      AND work_item.container_image_identity_v3_required
      AND work_item.container_image_identity_v3_authorized_status = work_item.status
    RETURNING work_item.work_item_id
),
inserted_set AS (
    INSERT INTO container_image_identity_support_sets (
        set_id, scope_id, content_hash, support_count
    )
    SELECT $1, $2, $4, $5
    FROM current_claim
    ON CONFLICT (scope_id, content_hash) DO NOTHING
    RETURNING set_id
),
resolved_set AS MATERIALIZED (
    SELECT set_id FROM inserted_set
    UNION ALL
    SELECT support_set.set_id
    FROM container_image_identity_support_sets AS support_set
    JOIN current_claim ON TRUE
    WHERE support_set.scope_id = $2
      AND support_set.content_hash = $4
      AND NOT EXISTS (SELECT 1 FROM inserted_set)
),
input_supports AS MATERIALIZED (
    SELECT support.*
    FROM jsonb_to_recordset($17::jsonb) AS support(
        digest TEXT,
        support_id TEXT,
        image_ref TEXT,
        repository_id TEXT,
        outcome TEXT,
        identity_strength TEXT,
        source_revision TEXT,
        source_revision_provenance TEXT,
        reason TEXT,
        canonical_writes INTEGER,
        source_repository_ids JSONB,
        build_provenance_repository_ids JSONB,
        base_image_for_repository_ids JSONB,
        workload_ids JSONB,
        service_ids JSONB,
        source_layers JSONB,
        evidence_fact_ids JSONB,
        missing_evidence JSONB
    )
),
existing_support_count AS MATERIALIZED (
    SELECT COUNT(*) AS support_count
    FROM container_image_identity_supports AS support
    JOIN resolved_set ON resolved_set.set_id = support.set_id
),
inserted_supports AS (
    INSERT INTO container_image_identity_supports (
        set_id, digest, support_id, image_ref, repository_id, outcome,
        identity_strength, source_revision, source_revision_provenance, reason,
        canonical_writes, source_repository_ids, build_provenance_repository_ids,
        base_image_for_repository_ids, workload_ids, service_ids, source_layers,
        evidence_fact_ids, missing_evidence
    )
    SELECT
        resolved_set.set_id,
        support.digest,
        decode(support.support_id, 'hex'),
        support.image_ref,
        support.repository_id,
        support.outcome,
        support.identity_strength,
        support.source_revision,
        support.source_revision_provenance,
        support.reason,
        support.canonical_writes,
        ARRAY(SELECT jsonb_array_elements_text(support.source_repository_ids)),
        ARRAY(SELECT jsonb_array_elements_text(support.build_provenance_repository_ids)),
        ARRAY(SELECT jsonb_array_elements_text(support.base_image_for_repository_ids)),
        ARRAY(SELECT jsonb_array_elements_text(support.workload_ids)),
        ARRAY(SELECT jsonb_array_elements_text(support.service_ids)),
        ARRAY(SELECT jsonb_array_elements_text(support.source_layers)),
        ARRAY(SELECT jsonb_array_elements_text(support.evidence_fact_ids)),
        ARRAY(SELECT jsonb_array_elements_text(support.missing_evidence))
    FROM resolved_set
    CROSS JOIN input_supports AS support
    ON CONFLICT (set_id, digest, support_id) DO NOTHING
    RETURNING 1
),
deleted_legacy AS (
    DELETE FROM fact_records AS fact
    USING resolved_set, current_claim
    WHERE fact.scope_id = $2
      AND fact.generation_id = $3
      AND fact.fact_kind = 'reducer_container_image_identity'
    RETURNING 1
),
legacy_cleanup AS MATERIALIZED (
    SELECT COUNT(*) AS deleted_count FROM deleted_legacy
),
written_support_count AS MATERIALIZED (
    SELECT
        existing_support_count.support_count + COUNT(inserted_supports.*) AS support_count
    FROM existing_support_count
    LEFT JOIN inserted_supports ON TRUE
    GROUP BY existing_support_count.support_count
)
UPDATE container_image_identity_scope_state AS state
SET active_set_id = resolved_set.set_id,
    last_set_id = resolved_set.set_id,
    last_set_hash = $4,
    published_claim_epoch = $7,
    source_system = $9,
    collector_kind = $10,
    source_confidence = $11,
    source_fact_key = $12,
    cause = $13,
    observed_at = $14,
    ingested_at = $15,
    fencing_token = $16,
    updated_at = clock_timestamp()
FROM resolved_set
JOIN current_claim ON TRUE
JOIN written_support_count ON written_support_count.support_count = $5
JOIN legacy_cleanup ON TRUE
WHERE state.scope_id = $2
  AND state.active_generation_id = $3
  AND state.activation_epoch = $8
RETURNING legacy_cleanup.deleted_count
`
