// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	// sharedProjectionUnroutableIntentBatchSize bounds one insert statement's
	// row count, matching reducerInputInvalidFactBatchSize's convention.
	sharedProjectionUnroutableIntentBatchSize     = 250
	sharedProjectionUnroutableIntentColumnsPerRow = 9
)

// sharedProjectionUnroutableIntentSchemaSQL is a SIBLING of
// reducer_input_invalid_facts, not a reuse of it (#5984).
//
// The two record different things and reusing the fact table would misreport
// what was quarantined: reducer_input_invalid_facts is keyed on a fact
// identity (fact_id, fact_kind, missing_field) that a shared-projection intent
// row simply does not have. Stuffing an intent id into fact_id would also
// pollute eshu_dp_reducer_input_invalid_facts_total and the
// list_reducer_input_invalid_facts read surface the corpus gate already
// asserts against.
//
// intent_id alone is the primary key: BuildSharedProjectionIntent derives it
// as a hash over scope, generation, domain, partition key, repository and
// source run, so it already encodes the natural key. That makes ON CONFLICT
// DO NOTHING a first-writer-wins replay guard for free.
//
// Deliberately NO foreign keys, unlike the fact table. shared_projection_intents
// declares scope_id as TEXT NOT NULL DEFAULT ” (migration 008), so legacy rows
// can carry an empty scope; an FK to ingestion_scopes would then reject the
// insert exactly when it matters most — when a malformed row is being recorded.
// The rows are reaped by an explicit statement in generation retention instead.
const sharedProjectionUnroutableIntentSchemaSQL = `
CREATE TABLE IF NOT EXISTS shared_projection_unroutable_intents (
    intent_id TEXT PRIMARY KEY,
    projection_domain TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    evidence_source TEXT NOT NULL,
    reason TEXT NOT NULL,
    decided_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS shared_projection_unroutable_intents_scope_generation_idx
    ON shared_projection_unroutable_intents (scope_id, generation_id, projection_domain, decided_at DESC);
`

const insertSharedProjectionUnroutableIntentBatchPrefix = `
INSERT INTO shared_projection_unroutable_intents (
    intent_id, projection_domain, partition_key, repository_id,
    scope_id, generation_id, evidence_source, reason, decided_at
) VALUES `

// insertSharedProjectionUnroutableIntentBatchSuffix keeps the first decision.
// The owning cycle re-runs whole on any failure after the write (a crash
// between this insert and MarkIntentsCompleted), so the same intent can be
// recorded twice; first-decided-at-wins is the honest reading, since the row
// records when the writer first found it unroutable.
const insertSharedProjectionUnroutableIntentBatchSuffix = `
ON CONFLICT (intent_id) DO NOTHING
`

// SharedProjectionUnroutableIntentStore persists durable rows for shared
// projection intents a canonical edge write could not route, implementing
// reducer.SharedProjectionUnroutableWriter directly.
//
// Unlike ReducerInputInvalidFactStore, a write failure here is NOT best-effort:
// the caller completes the intent immediately afterwards, so this row is the
// only lasting record that the intent produced no edge. See the interface doc
// for why that inversion is deliberate.
type SharedProjectionUnroutableIntentStore struct {
	db ExecQueryer
}

// NewSharedProjectionUnroutableIntentStore constructs a store backed by db.
func NewSharedProjectionUnroutableIntentStore(db ExecQueryer) *SharedProjectionUnroutableIntentStore {
	return &SharedProjectionUnroutableIntentStore{db: db}
}

// SharedProjectionUnroutableIntentSchemaSQL returns the DDL for the durable
// unroutable-intent table.
func SharedProjectionUnroutableIntentSchemaSQL() string {
	return sharedProjectionUnroutableIntentSchemaSQL
}

// EnsureSchema applies the shared_projection_unroutable_intents DDL.
func (s *SharedProjectionUnroutableIntentStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, sharedProjectionUnroutableIntentSchemaSQL)
	return err
}

// WriteUnroutableIntents implements reducer.SharedProjectionUnroutableWriter,
// writing in bounded batches with ON CONFLICT DO NOTHING so a replayed cycle
// converges on one row per intent rather than duplicating or erroring.
func (s *SharedProjectionUnroutableIntentStore) WriteUnroutableIntents(
	ctx context.Context,
	rows []reducer.SharedProjectionUnroutableRow,
) error {
	if len(rows) == 0 {
		return nil
	}
	for i := 0; i < len(rows); i += sharedProjectionUnroutableIntentBatchSize {
		end := i + sharedProjectionUnroutableIntentBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := insertSharedProjectionUnroutableIntentBatch(ctx, s.db, rows[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func insertSharedProjectionUnroutableIntentBatch(
	ctx context.Context,
	db ExecQueryer,
	batch []reducer.SharedProjectionUnroutableRow,
) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*sharedProjectionUnroutableIntentColumnsPerRow)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * sharedProjectionUnroutableIntentColumnsPerRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9,
		)
		args = append(
			args,
			row.IntentID,
			row.ProjectionDomain,
			row.PartitionKey,
			row.RepositoryID,
			row.ScopeID,
			row.GenerationID,
			row.EvidenceSource,
			row.Reason,
			row.DecidedAt.UTC(),
		)
	}

	query := insertSharedProjectionUnroutableIntentBatchPrefix +
		values.String() +
		insertSharedProjectionUnroutableIntentBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert shared projection unroutable intents: %w", err)
	}
	return nil
}
