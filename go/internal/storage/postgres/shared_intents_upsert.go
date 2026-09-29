// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	// sharedIntentBatchSize is the number of rows per multi-row INSERT batch.
	// 2000 rows * 12 columns = 24000 parameters per query, well under the
	// Postgres limit of 65535 while reducing code-call intent round trips.
	sharedIntentBatchSize = 2000

	// columnsPerSharedIntent is the number of columns in the
	// shared_projection_intents INSERT.
	columnsPerSharedIntent = 12
)

// preparedSharedIntentRow holds marshaled data for one shared intent row before batching.
type preparedSharedIntentRow struct {
	intentID         string
	projectionDomain string
	partitionKey     string
	scopeID          string
	acceptanceUnitID string
	repositoryID     string
	sourceRunID      string
	generationID     string
	partitionHash    string
	payloadBytes     []byte
	createdAt        time.Time
	completedAt      any
}

const upsertSharedIntentBatchPrefix = `
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id,
    repository_id, source_run_id, generation_id, partition_hash, payload,
    created_at, completed_at
) VALUES `

// upsertSharedIntentBatchSuffix is the ON CONFLICT clause of the shared
// projection intent upsert. Its overwrite and advance-only semantics are
// intended (#7323) and pinned by
// TestSharedIntentUpsertCreatedAtLastWriterWinsLive and
// TestRepoDependencyProjectionRunnerReplaysFencedWhenCompletedRunsOnCreatedAtMoves.
//
// Overwritten (last writer wins): every column, including payload and
// created_at. intent_id is derived without created_at
// (sharedintent.Build hashes the identity fields through
// sharedintent.StableIntentID), so a re-emitted intent conflicts on the same
// row and refreshes it in place.
//
// Advance-only: completed_at. COALESCE keeps an existing completed_at, so a
// completed intent stays completed after a retry, while a not-yet-completed row
// takes EXCLUDED.completed_at when the writer supplies one. The upsert never
// reopens (clears) completed_at; reopening stays the explicit act of
// rebuild/reset (storage/postgres/rebuild/reset), and a retry is not allowed to
// trigger a repo-wide retract and rewrite.
//
// Why created_at moves: it is the acceptance epoch of the RUNS_ON workload
// readiness fence (repoDependencyRunsOnFenceRequests in the reducer hashes each
// active row's intent_id and created_at). Only pending intents select an
// acceptance unit (listPendingDomainIntentsSQL filters completed_at IS NULL),
// so moving created_at on a completed row is inert while no pending sibling
// exists. When one does, the runner computes a new fence token, requests one
// fenced workload replay, the workload handler publishes that token, and the
// next cycle rewrites the active rows idempotently. It converges and never
// stalls.
//
// Why created_at is not frozen for completed rows: an older epoch would let a
// token published before the re-upsert satisfy the fence, weakening the
// WorkloadInstance prerequisite for RUNS_ON, and would corrupt created_at
// ordering of pending rows after a rebuild reset reopens them. Why the upsert
// does not reopen: see above. Both alternatives were rejected in the #7323
// ruling; do not change this clause without revisiting it.
const upsertSharedIntentBatchSuffix = `
ON CONFLICT (intent_id) DO UPDATE
SET projection_domain = EXCLUDED.projection_domain,
    partition_key = EXCLUDED.partition_key,
    scope_id = EXCLUDED.scope_id,
    acceptance_unit_id = EXCLUDED.acceptance_unit_id,
    repository_id = EXCLUDED.repository_id,
    source_run_id = EXCLUDED.source_run_id,
    generation_id = EXCLUDED.generation_id,
    partition_hash = EXCLUDED.partition_hash,
    payload = EXCLUDED.payload,
    created_at = EXCLUDED.created_at,
    completed_at = COALESCE(
        shared_projection_intents.completed_at,
        EXCLUDED.completed_at
    )
`

// UpsertIntents inserts or updates shared projection intents using batched
// multi-row INSERT statements. Each batch inserts up to sharedIntentBatchSize
// rows in a single query, reducing round trips to the database.
func (s *SharedIntentStore) UpsertIntents(ctx context.Context, rows []reducer.SharedProjectionIntentRow) error {
	if len(rows) == 0 {
		return nil
	}

	rows = deduplicateSharedIntentRows(rows)

	prepared := make([]preparedSharedIntentRow, 0, len(rows))
	for _, r := range rows {
		payloadBytes, err := json.Marshal(r.Payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}

		var completedAt any
		if r.CompletedAt != nil {
			completedAt = *r.CompletedAt
		}

		prepared = append(prepared, preparedSharedIntentRow{
			intentID:         r.IntentID,
			projectionDomain: r.ProjectionDomain,
			partitionKey:     r.PartitionKey,
			scopeID:          sharedIntentScopeID(r),
			acceptanceUnitID: sharedIntentAcceptanceUnitID(r),
			repositoryID:     r.RepositoryID,
			sourceRunID:      r.SourceRunID,
			generationID:     r.GenerationID,
			partitionHash:    strconv.FormatUint(reducer.PartitionHashForKey(r.PartitionKey), 10),
			payloadBytes:     payloadBytes,
			createdAt:        r.CreatedAt,
			completedAt:      completedAt,
		})
	}

	for i := 0; i < len(prepared); i += sharedIntentBatchSize {
		end := i + sharedIntentBatchSize
		if end > len(prepared) {
			end = len(prepared)
		}
		if err := upsertSharedIntentBatch(ctx, s.database, prepared[i:end]); err != nil {
			return err
		}
	}

	return nil
}

func deduplicateSharedIntentRows(rows []reducer.SharedProjectionIntentRow) []reducer.SharedProjectionIntentRow {
	if len(rows) < 2 {
		return rows
	}

	seen := make(map[string]struct{}, len(rows))
	deduplicated := make([]reducer.SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if _, exists := seen[row.IntentID]; exists {
			continue
		}
		seen[row.IntentID] = struct{}{}
		deduplicated = append(deduplicated, row)
	}

	return deduplicated
}

// upsertSharedIntentBatch inserts one batch of shared intents in one statement.
func upsertSharedIntentBatch(ctx context.Context, database db.ExecQueryer, batch []preparedSharedIntentRow) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*columnsPerSharedIntent)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerSharedIntent
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::numeric, $%d::jsonb, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10, offset+11, offset+12,
		)

		args = append(
			args,
			row.intentID,
			row.projectionDomain,
			row.partitionKey,
			row.scopeID,
			row.acceptanceUnitID,
			row.repositoryID,
			row.sourceRunID,
			row.generationID,
			row.partitionHash,
			row.payloadBytes,
			row.createdAt,
			row.completedAt,
		)
	}

	query := upsertSharedIntentBatchPrefix + values.String() + upsertSharedIntentBatchSuffix

	if _, err := database.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert shared intent batch (%d intents): %w", len(batch), err)
	}

	return nil
}
