// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

// ClaimReplayIdempotency atomically claims an idempotency key for a replay
// request. The admin_replay_requests primary key serializes concurrent
// duplicate delivery: the INSERT ... ON CONFLICT DO NOTHING returns a row only
// to the request that wins the claim; losers read the existing row's status,
// fingerprint, and recorded outcome.
func (s *postgresStore) ClaimReplayIdempotency(
	ctx context.Context,
	key, fingerprint string,
	now time.Time,
) (admin.ReplayIdempotencyClaim, error) {
	const insert = `
INSERT INTO admin_replay_requests (idempotency_key, request_fingerprint, status, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key
`
	insertRows, err := s.db.QueryContext(ctx, insert, key, fingerprint, admin.ReplayRequestStatusInProgress, now.UTC())
	if err != nil {
		return admin.ReplayIdempotencyClaim{}, fmt.Errorf("claim replay idempotency: %w", err)
	}
	claimed := insertRows.Next()
	if cerr := insertRows.Err(); cerr != nil {
		_ = insertRows.Close()
		return admin.ReplayIdempotencyClaim{}, fmt.Errorf("claim replay idempotency: %w", cerr)
	}
	if cerr := insertRows.Close(); cerr != nil {
		return admin.ReplayIdempotencyClaim{}, fmt.Errorf("claim replay idempotency close: %w", cerr)
	}
	if claimed {
		return admin.ReplayIdempotencyClaim{Claimed: true}, nil
	}

	const selectExisting = `
SELECT request_fingerprint, status, replayed_count, work_item_ids
FROM admin_replay_requests
WHERE idempotency_key = $1
`
	rows, err := s.db.QueryContext(ctx, selectExisting, key)
	if err != nil {
		return admin.ReplayIdempotencyClaim{}, fmt.Errorf("load replay idempotency: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if rerr := rows.Err(); rerr != nil {
			return admin.ReplayIdempotencyClaim{}, fmt.Errorf("load replay idempotency: %w", rerr)
		}
		// The row vanished between the conflicting INSERT and this SELECT
		// (e.g. a concurrent prune). Treat it as not claimed without a prior
		// outcome so the caller fails closed rather than double-replaying.
		return admin.ReplayIdempotencyClaim{}, nil
	}

	var fingerprintOut, status string
	var count int
	var idsJSON []byte
	if err := rows.Scan(&fingerprintOut, &status, &count, &idsJSON); err != nil {
		return admin.ReplayIdempotencyClaim{}, fmt.Errorf("scan replay idempotency: %w", err)
	}
	var ids []string
	if len(idsJSON) > 0 {
		if err := json.Unmarshal(idsJSON, &ids); err != nil {
			return admin.ReplayIdempotencyClaim{}, fmt.Errorf("decode replay idempotency ids: %w", err)
		}
	}
	return admin.ReplayIdempotencyClaim{
		Claimed:       false,
		Status:        status,
		Fingerprint:   fingerprintOut,
		ReplayedCount: count,
		WorkItemIDs:   ids,
	}, nil
}

// CompleteReplayIdempotency records the outcome of a claimed replay so a later
// duplicate request with the same key returns the same result without
// re-running the replay. It only advances a row still in progress.
func (s *postgresStore) CompleteReplayIdempotency(
	ctx context.Context,
	key string,
	count int,
	workItemIDs []string,
	now time.Time,
) error {
	ids := workItemIDs
	if ids == nil {
		ids = []string{}
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("encode replay idempotency ids: %w", err)
	}
	const update = `
UPDATE admin_replay_requests
SET status = $2,
    replayed_count = $3,
    work_item_ids = $4,
    completed_at = $5
WHERE idempotency_key = $1
  AND status = $6
`
	if _, err := s.db.ExecContext(
		ctx,
		update,
		key,
		admin.ReplayRequestStatusCompleted,
		count,
		encoded,
		now.UTC(),
		admin.ReplayRequestStatusInProgress,
	); err != nil {
		return fmt.Errorf("complete replay idempotency: %w", err)
	}
	return nil
}
