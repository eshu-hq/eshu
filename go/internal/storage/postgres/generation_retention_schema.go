// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const generationRetentionEventSchemaSQL = `
CREATE TABLE IF NOT EXISTS generation_retention_events (
    event_id TEXT PRIMARY KEY,
    scope_id_hash TEXT NOT NULL,
    generation_id_hash TEXT NOT NULL,
    scope_class TEXT NOT NULL,
    policy_scope TEXT NOT NULL,
    policy_revision TEXT NOT NULL,
    generation_observed_at TIMESTAMPTZ NULL,
    generation_superseded_at TIMESTAMPTZ NULL,
    reason TEXT NOT NULL,
    row_counts JSONB NOT NULL DEFAULT '{}'::jsonb,
    pruned_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS generation_retention_events_scope_idx
    ON generation_retention_events (scope_id_hash, generation_observed_at DESC);

CREATE INDEX IF NOT EXISTS generation_retention_events_generation_idx
    ON generation_retention_events (scope_id_hash, generation_id_hash);

CREATE INDEX IF NOT EXISTS generation_retention_events_reason_idx
    ON generation_retention_events (reason, pruned_at DESC);
`

// ErrGenerationRetentionKeyIndexUnavailable reports that a retention cycle was
// refused because fact_records_content_entity_key_idx or
// fact_records_file_key_idx (migrations 146 and 147) is missing, invalid, or
// not the expected shape. The retention statements probe those indexes once per
// candidate key; without them each probe scans fact_records while the batch
// holds its scope locks (the #6809 cliff), so the store refuses rather than run
// that shape. The cycle is retried on the runner's next poll.
var ErrGenerationRetentionKeyIndexUnavailable = errors.New("generation retention key index unavailable")

// checkGenerationRetentionKeyIndexes returns an error wrapping
// ErrGenerationRetentionKeyIndexUnavailable that names each unusable key index,
// or nil when both are valid.
func checkGenerationRetentionKeyIndexes(ctx context.Context, tx db.Transaction) error {
	rows, err := tx.QueryContext(ctx, generationRetentionKeyIndexesQuery)
	if err != nil {
		return fmt.Errorf("generation retention: check key indexes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var missing []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("generation retention: scan key index check: %w", err)
		}
		missing = append(missing, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("generation retention: check key indexes: %w", err)
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s missing, invalid, or not the expected shape",
			ErrGenerationRetentionKeyIndexUnavailable, strings.Join(missing, ", "))
	}
	return nil
}

func (s GenerationRetentionStore) recordRetentionEvent(
	ctx context.Context,
	tx db.Transaction,
	candidate generationRetentionCandidate,
	policy GenerationRetentionPolicy,
	rowCounts map[string]int64,
	now time.Time,
) error {
	if rowCounts == nil {
		rowCounts = map[string]int64{}
	}
	rowCountsJSON, err := json.Marshal(rowCounts)
	if err != nil {
		return fmt.Errorf("generation retention: marshal row counts: %w", err)
	}
	scopeHash := retentionHashID("scope", candidate.scopeID)
	generationHash := retentionHashID("generation", candidate.generationID)
	eventID := retentionHashID("event", scopeHash+"\x00"+generationHash+"\x00"+policy.PolicyRevision)
	if _, err := tx.ExecContext(
		ctx,
		insertGenerationRetentionEventQuery,
		eventID,
		scopeHash,
		generationHash,
		candidate.scopeKind,
		policy.PolicyScope,
		policy.PolicyRevision,
		candidate.observedAt,
		candidate.supersededAt,
		"superseded_window_expired",
		rowCountsJSON,
		now,
	); err != nil {
		return fmt.Errorf("generation retention: record event: %w", err)
	}
	return nil
}

func retentionHashID(kind string, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + value))
	return hex.EncodeToString(sum[:])
}
