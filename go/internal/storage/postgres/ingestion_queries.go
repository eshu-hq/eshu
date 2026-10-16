// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scalars"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope"
)

const listLatestRelationshipFactRecordsQuery = backfill.LatestGenerationCTE + `
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
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
  AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

const upsertIngestionScopeQuery = `
INSERT INTO ingestion_scopes (
    scope_id,
    scope_kind,
    source_system,
    source_key,
    parent_scope_id,
    collector_kind,
    partition_key,
    observed_at,
    ingested_at,
    status,
    active_generation_id,
    payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb
)
ON CONFLICT (scope_id) DO UPDATE SET
    scope_kind = EXCLUDED.scope_kind,
    source_system = EXCLUDED.source_system,
    source_key = EXCLUDED.source_key,
    parent_scope_id = EXCLUDED.parent_scope_id,
    collector_kind = EXCLUDED.collector_kind,
    partition_key = EXCLUDED.partition_key,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    status = CASE
        WHEN ingestion_scopes.active_generation_id IS NOT NULL
            AND EXCLUDED.active_generation_id IS NULL
            AND EXCLUDED.status = 'pending'
        THEN ingestion_scopes.status
        ELSE EXCLUDED.status
    END,
    active_generation_id = CASE
        WHEN EXCLUDED.active_generation_id IS NOT NULL THEN EXCLUDED.active_generation_id
        ELSE ingestion_scopes.active_generation_id
    END,
    payload = EXCLUDED.payload
`

const upsertScopeGenerationQuery = `
INSERT INTO scope_generations (
    generation_id,
    scope_id,
    trigger_kind,
    freshness_hint,
    source_commit_sha,
    is_delta,
    observed_at,
    ingested_at,
    status,
    activated_at,
    superseded_at,
    payload,
    delta_baseline_commit_sha
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL, $12::jsonb, $11
)
ON CONFLICT (generation_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    trigger_kind = EXCLUDED.trigger_kind,
    freshness_hint = EXCLUDED.freshness_hint,
    source_commit_sha = EXCLUDED.source_commit_sha,
    is_delta = EXCLUDED.is_delta,
    delta_baseline_commit_sha = EXCLUDED.delta_baseline_commit_sha,
    -- A generation ID fixes its original chronology. A retry may refresh a
    -- pending generation's metadata and facts, but must not make it newer
    -- than a successor or reopen a published/terminal generation.
    status = EXCLUDED.status,
    activated_at = EXCLUDED.activated_at,
    payload = EXCLUDED.payload
WHERE scope_generations.status = 'pending'
`

const activeGenerationFreshnessQuery = `
SELECT generation.generation_id, COALESCE(generation.freshness_hint, '')
FROM scope_generations AS generation
WHERE generation.scope_id = $1
  AND generation.status IN ('pending', 'active')
  AND COALESCE(generation.freshness_hint, '') <> ''
ORDER BY generation.ingested_at DESC, generation.generation_id DESC
LIMIT 1
`

// Backfill-owned generation queries live in the backfill package:
// backfill.ActiveRepositoryGenerationsQuery and
// backfill.ActiveScopeGenerationPartitionsQuery (issue #7794).

// Whole-pass reopen SQL is owned with the deferred-backfill engine.
const (
	listSucceededDeploymentMappingWorkItemsQuery  = backfill.ListSucceededDeploymentMappingWorkItemsQuery
	listSucceededCodeImportRepoEdgeWorkItemsQuery = backfill.ListSucceededCodeImportRepoEdgeWorkItemsQuery
)

func upsertIngestionScope(
	ctx context.Context,
	database db.ExecQueryer,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
) error {
	payloadJSON, err := payloadstore.MarshalPayload(scalars.StringMapToAny(scopeValue.MetadataCopy()))
	if err != nil {
		return fmt.Errorf("marshal scope payload: %w", err)
	}

	_, err = database.ExecContext(
		ctx,
		upsertIngestionScopeQuery,
		scopeValue.ScopeID,
		string(scopeValue.ScopeKind),
		scopeValue.SourceSystem,
		scopestore.SourceKey(scopeValue),
		payloadstore.EmptyToNil(scopeValue.ParentScopeID),
		string(scopeValue.CollectorKind),
		scopeValue.PartitionKey,
		generation.ObservedAt.UTC(),
		generation.IngestedAt.UTC(),
		string(generation.Status),
		activeGenerationID(generation),
		payloadJSON,
	)
	if err != nil {
		return err
	}

	return nil
}

var errGenerationAlreadyFinalized = errors.New("generation already published or terminal")

func upsertScopeGeneration(
	ctx context.Context,
	database db.ExecQueryer,
	generation scope.ScopeGeneration,
) error {
	result, err := database.ExecContext(
		ctx,
		upsertScopeGenerationQuery,
		generation.GenerationID,
		generation.ScopeID,
		string(generation.TriggerKind),
		payloadstore.EmptyToNil(generation.FreshnessHint),
		payloadstore.EmptyToNil(generation.SourceCommitSHA),
		generation.IsDelta,
		generation.ObservedAt.UTC(),
		generation.IngestedAt.UTC(),
		string(generation.Status),
		activeTimestamp(generation),
		payloadstore.EmptyToNil(strings.TrimSpace(generation.DeltaBaselineCommitSHA)),
		scopeGenerationPayload(generation),
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("generation upsert rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return errGenerationAlreadyFinalized
	}
	if rowsAffected != 1 {
		return fmt.Errorf("generation upsert affected %d rows, want 1", rowsAffected)
	}

	return nil
}

func shouldDiscoverStreamingRelationshipEvidence(scopeValue scope.IngestionScope) bool {
	return scopeValue.ScopeKind == scope.KindRepository
}

func activeGenerationID(generation scope.ScopeGeneration) any {
	if generation.Status == scope.GenerationStatusActive {
		return generation.GenerationID
	}

	return nil
}

func activeTimestamp(generation scope.ScopeGeneration) any {
	if generation.Status == scope.GenerationStatusActive {
		return generation.IngestedAt.UTC()
	}

	return nil
}

// scopeGenerationPayload returns the scope_generations.payload JSON a stored
// generation carries (#7978): reconcile_reason for a forced reconciliation
// snapshot and default_branch_changed for a #7678 default-branch observation,
// so an operator can tell why a generation was stored; "{}" otherwise.
func scopeGenerationPayload(generation scope.ScopeGeneration) string {
	payload := make(map[string]any, 2)
	if reason := strings.TrimSpace(generation.ReconcileReason); reason != "" {
		payload["reconcile_reason"] = reason
	}
	if generation.DefaultBranchChanged {
		payload["default_branch_changed"] = true
	}
	if identity := strings.TrimSpace(generation.PreparseIdentity); identity != "" {
		payload["preparse_identity"] = identity
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
