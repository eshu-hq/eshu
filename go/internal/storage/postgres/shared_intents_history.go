// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/lib/pq"
)

const hasCompletedAcceptanceUnitDomainIntentsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM shared_projection_intents
    WHERE scope_id = $1
      AND acceptance_unit_id = $2
      AND projection_domain = $3
      AND completed_at IS NOT NULL
    LIMIT 1
)
`

const hasCompletedAcceptanceUnitSourceRunDomainIntentsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM shared_projection_intents
    WHERE scope_id = $1
      AND acceptance_unit_id = $2
      AND source_run_id = $3
      AND projection_domain = $4
      AND completed_at IS NOT NULL
    LIMIT 1
)
`

const hasCompletedAcceptanceUnitSourceRunPartitionDomainIntentsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM shared_projection_intents
    WHERE scope_id = $1
      AND acceptance_unit_id = $2
      AND source_run_id = $3
      AND partition_key = $4
      AND projection_domain = $5
      AND completed_at IS NOT NULL
    LIMIT 1
)
`

const hasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntentsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM shared_projection_intents
    WHERE scope_id = $1
      AND acceptance_unit_id = $2
      AND source_run_id = $3
      AND generation_id = $4
      AND partition_key = $5
      AND projection_domain = $6
      AND completed_at IS NOT NULL
    LIMIT 1
)
`

const hasCompletedAcceptanceUnitSourceRunRefreshDomainIntentsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM shared_projection_intents
    WHERE scope_id = $1
      AND acceptance_unit_id = $2
      AND source_run_id = $3
      AND projection_domain = $4
      AND completed_at IS NOT NULL
      AND payload->>'intent_type' = 'repo_refresh'
      AND jsonb_typeof(payload->'delta_file_paths') = 'array'
      AND NOT EXISTS (
          SELECT 1
          FROM unnest($5::text[]) AS selected(path)
          WHERE NOT EXISTS (
              SELECT 1
              FROM jsonb_array_elements_text(payload->'delta_file_paths') AS refresh(path)
              WHERE refresh.path = selected.path
          )
      )
    LIMIT 1
)
`

// codeCallProjectionWholeFenceSQL fences a non-file code-call intent behind the
// earlier intents for the same repository acceptance unit. "Earlier" MUST use
// the same ordering as the batch query and the in-memory fence —
// (is_refresh_intent DESC, created_at ASC, intent_id ASC) — not raw
// (created_at, intent_id). A refresh intent precedes every non-refresh edge, so:
//   - an edge is correctly fenced behind the repo refresh, and
//   - the refresh is NEVER fenced behind its own (older) edges.
//
// The raw (created_at, intent_id) comparison was the #3865 deadlock: when a
// repo's refresh intent and an older-created edge landed in the same projection
// partition, the DB fence blocked the refresh behind the edge (created_at order)
// while the in-memory fence blocked the edge behind the refresh (refresh-first
// list order), so neither ever ran and the intents were held at terminal.
const codeCallProjectionWholeFenceSQL = `
WITH selected AS (
    SELECT is_refresh_intent
    FROM shared_projection_intents
    WHERE intent_id = $1
      AND scope_id = $2
      AND acceptance_unit_id = $3
      AND source_run_id = $4
      AND projection_domain = $5
      AND completed_at IS NULL
    LIMIT 1
),
blocked AS (
    SELECT 1
    FROM shared_projection_intents AS candidate, selected
    WHERE candidate.scope_id = $2
      AND candidate.acceptance_unit_id = $3
      AND candidate.source_run_id = $4
      AND candidate.projection_domain = $5
      AND candidate.repository_id = $6
      AND candidate.completed_at IS NULL
      AND candidate.intent_id <> $1
      AND (
          (candidate.is_refresh_intent AND NOT selected.is_refresh_intent)
          OR (
              candidate.is_refresh_intent = selected.is_refresh_intent
              AND (candidate.created_at, candidate.intent_id) < ($7, $1)
          )
      )
    LIMIT 1
)
SELECT
    EXISTS (SELECT 1 FROM selected) AS selected_exists,
    EXISTS (SELECT 1 FROM blocked) AS blocked_by_fence
`

// codeCallProjectionFileFenceSQL fences a file-scoped code-call intent. The
// non-file branch (the OR arm below) ranks non-file candidates by the same
// is_refresh_intent-first order as the whole-fence (#3865): a non-file refresh
// (the repo whole-refresh) precedes a non-refresh file row, but a non-refresh
// non-file edge must NOT fence a file repo_refresh behind it — otherwise a file
// repo_refresh and an older non-file edge deadlock exactly as the whole-fence
// case did.
const codeCallProjectionFileFenceSQL = `
WITH selected AS (
    SELECT is_refresh_intent
    FROM shared_projection_intents
    WHERE intent_id = $1
      AND scope_id = $2
      AND acceptance_unit_id = $3
      AND source_run_id = $4
      AND projection_domain = $5
      AND completed_at IS NULL
    LIMIT 1
),
blocked AS (
    SELECT 1
    FROM shared_projection_intents AS candidate, selected
    WHERE candidate.scope_id = $2
      AND candidate.acceptance_unit_id = $3
      AND candidate.source_run_id = $4
      AND candidate.projection_domain = $5
      AND candidate.repository_id = $6
      AND candidate.completed_at IS NULL
      AND candidate.intent_id <> $1
      AND (
          (
              $10::boolean
              AND candidate.partition_key LIKE $9
              AND candidate.payload->>'intent_type' = 'repo_refresh'
              AND (
                  candidate.partition_key = $8::text
                  OR (
                      $11::boolean
                      AND NOT EXISTS (
                          SELECT 1
                          FROM unnest($12::text[]) AS selected_file(path)
                          WHERE NOT EXISTS (
                              SELECT 1
                              FROM jsonb_array_elements_text(
                                  CASE
                                      WHEN jsonb_typeof(candidate.payload->'delta_file_paths') = 'array'
                                      THEN candidate.payload->'delta_file_paths'
                                      ELSE '[]'::jsonb
                                  END
                              ) AS refresh_file(path)
                              WHERE refresh_file.path = selected_file.path
                          )
                      )
                  )
              )
          )
          OR (
              candidate.partition_key NOT LIKE $9
              AND (
                  (candidate.is_refresh_intent AND NOT selected.is_refresh_intent)
                  OR (
                      candidate.is_refresh_intent = selected.is_refresh_intent
                      AND (candidate.created_at, candidate.intent_id) < ($7, $1)
                  )
              )
          )
      )
    LIMIT 1
)
SELECT
    EXISTS (SELECT 1 FROM selected) AS selected_exists,
    EXISTS (SELECT 1 FROM blocked) AS blocked_by_fence
`

// HasCompletedAcceptanceUnitDomainIntents reports whether any prior intent for
// the bounded acceptance unit and domain completed. It intentionally ignores
// source_run_id so new generations can see older completed graph projection
// state for the same accepted unit.
func (s *SharedIntentStore) HasCompletedAcceptanceUnitDomainIntents(
	ctx context.Context,
	key reducer.SharedProjectionAcceptanceKey,
	domain string,
) (bool, error) {
	sqlRows, err := s.db.QueryContext(
		ctx,
		hasCompletedAcceptanceUnitDomainIntentsSQL,
		key.ScopeID,
		key.AcceptanceUnitID,
		domain,
	)
	if err != nil {
		return false, fmt.Errorf("query completed shared projection history: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	if !sqlRows.Next() {
		return false, nil
	}
	var exists bool
	if err := sqlRows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan completed shared projection history: %w", err)
	}
	return exists, sqlRows.Err()
}

// HasCompletedAcceptanceUnitSourceRunDomainIntents reports whether the current
// source run already completed at least one chunk for the bounded acceptance
// unit and domain. Chunked code-call projection uses this to avoid retracting
// edges between chunks from the same accepted source run.
func (s *SharedIntentStore) HasCompletedAcceptanceUnitSourceRunDomainIntents(
	ctx context.Context,
	key reducer.SharedProjectionAcceptanceKey,
	domain string,
) (bool, error) {
	sqlRows, err := s.db.QueryContext(
		ctx,
		hasCompletedAcceptanceUnitSourceRunDomainIntentsSQL,
		key.ScopeID,
		key.AcceptanceUnitID,
		key.SourceRunID,
		domain,
	)
	if err != nil {
		return false, fmt.Errorf("query completed source-run shared projection history: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	if !sqlRows.Next() {
		return false, nil
	}
	var exists bool
	if err := sqlRows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan completed source-run shared projection history: %w", err)
	}
	return exists, sqlRows.Err()
}

// HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents reports whether
// the selected source-run partition already completed for the bounded
// acceptance unit and domain.
func (s *SharedIntentStore) HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(
	ctx context.Context,
	key reducer.SharedProjectionAcceptanceKey,
	partitionKey string,
	domain string,
) (bool, error) {
	sqlRows, err := s.db.QueryContext(
		ctx,
		hasCompletedAcceptanceUnitSourceRunPartitionDomainIntentsSQL,
		key.ScopeID,
		key.AcceptanceUnitID,
		key.SourceRunID,
		partitionKey,
		domain,
	)
	if err != nil {
		return false, fmt.Errorf("query completed source-run partition shared projection history: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	if !sqlRows.Next() {
		return false, nil
	}
	var exists bool
	if err := sqlRows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan completed source-run partition shared projection history: %w", err)
	}
	return exists, sqlRows.Err()
}

// HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents requires
// an exact-generation completion. Exact same-generation redelivery reuses the
// deterministic intent ID and preserves completed_at during upsert, while the
// generation predicate prevents a reused source run from opening a later
// generation's fence (#5554).
func (s *SharedIntentStore) HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents(
	ctx context.Context,
	key reducer.SharedProjectionAcceptanceKey,
	generationID string,
	partitionKey string,
	domain string,
) (bool, error) {
	sqlRows, err := s.db.QueryContext(
		ctx,
		hasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntentsSQL,
		key.ScopeID,
		key.AcceptanceUnitID,
		key.SourceRunID,
		generationID,
		partitionKey,
		domain,
	)
	if err != nil {
		return false, fmt.Errorf("query completed generation refresh fence history: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	if !sqlRows.Next() {
		return false, nil
	}
	var ready bool
	if err := sqlRows.Scan(&ready); err != nil {
		return false, fmt.Errorf("scan completed generation refresh fence history: %w", err)
	}
	return ready, sqlRows.Err()
}

// HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents reports whether a
// completed current-run repo refresh covers every selected file path.
func (s *SharedIntentStore) HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(
	ctx context.Context,
	key reducer.SharedProjectionAcceptanceKey,
	filePaths []string,
	domain string,
) (bool, error) {
	if len(filePaths) == 0 {
		return false, nil
	}

	sqlRows, err := s.db.QueryContext(
		ctx,
		hasCompletedAcceptanceUnitSourceRunRefreshDomainIntentsSQL,
		key.ScopeID,
		key.AcceptanceUnitID,
		key.SourceRunID,
		domain,
		pq.Array(filePaths),
	)
	if err != nil {
		return false, fmt.Errorf("query completed source-run refresh shared projection history: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	if !sqlRows.Next() {
		return false, nil
	}
	var exists bool
	if err := sqlRows.Scan(&exists); err != nil {
		return false, fmt.Errorf("scan completed source-run refresh shared projection history: %w", err)
	}
	return exists, sqlRows.Err()
}

// CodeCallProjectionRowBlockedByRepoFence reports whether a pending code-call
// row is fenced by another pending row in the same acceptance unit. It uses an
// existence query so full-repo code-call projection does not load every pending
// row into the reducer heap just to preserve refresh ordering.
func (s *SharedIntentStore) CodeCallProjectionRowBlockedByRepoFence(
	ctx context.Context,
	key reducer.SharedProjectionAcceptanceKey,
	row reducer.SharedProjectionIntentRow,
	domain string,
) (bool, error) {
	repositoryID := strings.TrimSpace(row.RepositoryID)
	if repositoryID == "" {
		repositoryID = sharedIntentPayloadString(row.Payload, "repo_id")
	}
	if repositoryID == "" {
		return false, nil
	}
	rowFiles := sharedIntentPayloadStringSlice(row.Payload, "delta_file_paths")
	filePartitionPrefix := reducer.CodeCallProjectionFilePartitionKeyPrefix()
	if !strings.HasPrefix(row.PartitionKey, filePartitionPrefix) {
		return s.queryCodeCallProjectionFence(
			ctx,
			codeCallProjectionWholeFenceSQL,
			row.IntentID,
			key.ScopeID,
			key.AcceptanceUnitID,
			key.SourceRunID,
			domain,
			repositoryID,
			row.CreatedAt.UTC(),
		)
	}

	rowCanBeCoveredByFileRefresh := sharedIntentPayloadString(row.Payload, "intent_type") != "repo_refresh"
	rowCanBeCoveredByFileRefreshByPath := rowCanBeCoveredByFileRefresh && len(rowFiles) > 0
	return s.queryCodeCallProjectionFence(
		ctx,
		codeCallProjectionFileFenceSQL,
		row.IntentID,
		key.ScopeID,
		key.AcceptanceUnitID,
		key.SourceRunID,
		domain,
		repositoryID,
		row.CreatedAt.UTC(),
		row.PartitionKey,
		filePartitionPrefix+"%",
		rowCanBeCoveredByFileRefresh,
		rowCanBeCoveredByFileRefreshByPath,
		pq.Array(rowFiles),
	)
}

func (s *SharedIntentStore) queryCodeCallProjectionFence(
	ctx context.Context,
	query string,
	args ...any,
) (bool, error) {
	sqlRows, err := s.db.QueryContext(
		ctx,
		query,
		args...,
	)
	if err != nil {
		return false, fmt.Errorf("query code call projection refresh fence: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	if !sqlRows.Next() {
		return true, sqlRows.Err()
	}
	var selectedExists bool
	var blocked bool
	if err := sqlRows.Scan(&selectedExists, &blocked); err != nil {
		return false, fmt.Errorf("scan code call projection refresh fence: %w", err)
	}
	if err := sqlRows.Err(); err != nil {
		return false, err
	}
	return !selectedExists || blocked, nil
}

func sharedIntentPayloadStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}
