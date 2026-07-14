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
	// reducerInputInvalidFactBatchSize bounds one insert statement's row count,
	// matching the batching convention graphProjectionPhaseStateBatchSize uses
	// for the sibling phase-state table.
	reducerInputInvalidFactBatchSize     = 250
	reducerInputInvalidFactColumnsPerRow = 8
)

const reducerInputInvalidFactSchemaSQL = `
CREATE TABLE IF NOT EXISTS reducer_input_invalid_facts (
    fact_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL,
    missing_field TEXT NOT NULL,
    failure_class TEXT NOT NULL,
    domain TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    decided_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, fact_id, missing_field, domain)
);

CREATE INDEX IF NOT EXISTS reducer_input_invalid_facts_scope_generation_domain_idx
    ON reducer_input_invalid_facts (scope_id, generation_id, domain, fact_kind, decided_at DESC);
`

const insertReducerInputInvalidFactBatchPrefix = `
INSERT INTO reducer_input_invalid_facts (
    fact_id, fact_kind, missing_field, failure_class, domain, scope_id, generation_id, decided_at
) VALUES `

// insertReducerInputInvalidFactBatchSuffix intentionally does nothing on a
// natural-key collision: reduction replay (a retried intent, or a
// re-projected generation) may re-quarantine the same fact/field within the
// SAME domain, and the first-recorded decided_at should win rather than being
// overwritten on every replay. This is what makes
// ReducerInputInvalidFactStore.WriteQuarantinedFacts idempotent under
// at-least-once reduction (issue #4630).
//
// domain is part of the conflict target (matching the table's primary key)
// because more than one reducer domain can decode and quarantine the SAME
// fact independently — for example aws_resource is decoded both by AWS
// resource materialization and by the relationship/IAM/security-group join
// paths, each its own domain. Without domain in the key, the second domain's
// insert would collide with and be silently dropped by the first domain's
// row, and a domain-filtered read would falsely return empty for the second
// domain's quarantine even though it observed the fault. Keying on domain
// preserves per-domain quarantine truth while still deduping replay within a
// single domain (codex review on PR #5252, issue #4630).
const insertReducerInputInvalidFactBatchSuffix = `
ON CONFLICT (scope_id, generation_id, fact_id, missing_field, domain) DO NOTHING
`

// ReducerInputInvalidFactStore persists durable reducer_input_invalid_facts
// rows and implements reducer.QuarantinedFactWriter directly (mirroring
// GraphProjectionPhaseStateStore's direct implementation of
// reducer.GraphProjectionPhasePublisher): the reducer package's DTOs
// (reducer.QuarantinedFactRecord) are used as-is, with no separate cmd/reducer
// adapter required.
type ReducerInputInvalidFactStore struct {
	db ExecQueryer
}

// NewReducerInputInvalidFactStore constructs a store backed by db.
func NewReducerInputInvalidFactStore(db ExecQueryer) *ReducerInputInvalidFactStore {
	return &ReducerInputInvalidFactStore{db: db}
}

// ReducerInputInvalidFactSchemaSQL returns the DDL for the durable
// input_invalid quarantine table.
func ReducerInputInvalidFactSchemaSQL() string {
	return reducerInputInvalidFactSchemaSQL
}

// EnsureSchema applies the reducer_input_invalid_facts DDL.
func (s *ReducerInputInvalidFactStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, reducerInputInvalidFactSchemaSQL)
	return err
}

// WriteQuarantinedFacts implements reducer.QuarantinedFactWriter. It writes
// records in bounded batches, each batch one INSERT statement (one round trip
// per up-to-reducerInputInvalidFactBatchSize rows), with ON CONFLICT DO
// NOTHING on the natural key so replaying the same quarantine is a no-op
// rather than a duplicate row or an error.
func (s *ReducerInputInvalidFactStore) WriteQuarantinedFacts(
	ctx context.Context,
	records []reducer.QuarantinedFactRecord,
) error {
	if len(records) == 0 {
		return nil
	}
	for i := 0; i < len(records); i += reducerInputInvalidFactBatchSize {
		end := i + reducerInputInvalidFactBatchSize
		if end > len(records) {
			end = len(records)
		}
		if err := insertReducerInputInvalidFactBatch(ctx, s.db, records[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func insertReducerInputInvalidFactBatch(
	ctx context.Context,
	db ExecQueryer,
	batch []reducer.QuarantinedFactRecord,
) error {
	if len(batch) == 0 {
		return nil
	}

	args := make([]any, 0, len(batch)*reducerInputInvalidFactColumnsPerRow)
	var values strings.Builder

	for i, row := range batch {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * reducerInputInvalidFactColumnsPerRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6, offset+7, offset+8,
		)
		args = append(
			args,
			strings.TrimSpace(row.FactID),
			strings.TrimSpace(row.FactKind),
			strings.TrimSpace(row.MissingField),
			strings.TrimSpace(row.FailureClass),
			strings.TrimSpace(row.Domain),
			strings.TrimSpace(row.ScopeID),
			strings.TrimSpace(row.GenerationID),
			row.DecidedAt.UTC(),
		)
	}

	query := insertReducerInputInvalidFactBatchPrefix + values.String() + insertReducerInputInvalidFactBatchSuffix
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert reducer input_invalid fact batch (%d rows): %w", len(batch), err)
	}
	return nil
}
