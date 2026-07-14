// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerInputInvalidFactSchemaSQL(t *testing.T) {
	t.Parallel()

	sqlStr := ReducerInputInvalidFactSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS reducer_input_invalid_facts",
		"REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE",
		"REFERENCES scope_generations(generation_id) ON DELETE CASCADE",
		"PRIMARY KEY (scope_id, generation_id, fact_id, missing_field, domain)",
		"reducer_input_invalid_facts_scope_generation_domain_idx",
	} {
		if !strings.Contains(sqlStr, want) {
			t.Fatalf("ReducerInputInvalidFactSchemaSQL() missing %q", want)
		}
	}
}

// recordingReducerInputInvalidFactDB is a minimal fake ExecQueryer that
// records every executed statement's SQL and args, without reimplementing
// Postgres' ON CONFLICT semantics — that guarantee is proven against a real
// database by TestReducerInputInvalidFactStoreLive. This fake only proves
// WriteQuarantinedFacts' batching and argument-marshaling: one statement per
// bounded batch, correct column order, and that the ON CONFLICT DO NOTHING
// clause text is actually present in the emitted SQL.
type recordingReducerInputInvalidFactDB struct {
	statements []string
	args       [][]any
}

func (db *recordingReducerInputInvalidFactDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.statements = append(db.statements, query)
	db.args = append(db.args, args)
	return driverResult{}, nil
}

func (db *recordingReducerInputInvalidFactDB) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	return nil, fmt.Errorf("QueryContext not used by ReducerInputInvalidFactStore.WriteQuarantinedFacts")
}

type driverResult struct{}

func (driverResult) LastInsertId() (int64, error) { return 0, nil }
func (driverResult) RowsAffected() (int64, error) { return 0, nil }

func TestReducerInputInvalidFactStoreWriteBatchesAndUsesConflictDoNothing(t *testing.T) {
	t.Parallel()

	db := &recordingReducerInputInvalidFactDB{}
	store := NewReducerInputInvalidFactStore(db)
	now := time.Now().UTC()

	// One row over the batch size forces two statements, proving the batching
	// loop actually splits work rather than building one unbounded statement.
	records := make([]reducer.QuarantinedFactRecord, reducerInputInvalidFactBatchSize+1)
	for i := range records {
		records[i] = reducer.QuarantinedFactRecord{
			FactID:       fmt.Sprintf("fact-%d", i),
			FactKind:     "aws_resource",
			MissingField: "account_id",
			FailureClass: "input_invalid",
			Domain:       "aws_resource_materialization",
			ScopeID:      "scope-a",
			GenerationID: "gen-a",
			DecidedAt:    now,
		}
	}

	if err := store.WriteQuarantinedFacts(context.Background(), records); err != nil {
		t.Fatalf("WriteQuarantinedFacts() error = %v", err)
	}

	if len(db.statements) != 2 {
		t.Fatalf("statement count = %d, want 2 batches for %d rows over batch size %d", len(db.statements), len(records), reducerInputInvalidFactBatchSize)
	}
	for i, stmt := range db.statements {
		if !strings.Contains(stmt, "INSERT INTO reducer_input_invalid_facts") {
			t.Fatalf("statement[%d] missing INSERT INTO reducer_input_invalid_facts: %s", i, stmt)
		}
		if !strings.Contains(stmt, "ON CONFLICT (scope_id, generation_id, fact_id, missing_field, domain) DO NOTHING") {
			t.Fatalf("statement[%d] missing the idempotent ON CONFLICT DO NOTHING clause: %s", i, stmt)
		}
	}
	if len(db.args[0]) != reducerInputInvalidFactBatchSize*reducerInputInvalidFactColumnsPerRow {
		t.Fatalf("first batch arg count = %d, want %d", len(db.args[0]), reducerInputInvalidFactBatchSize*reducerInputInvalidFactColumnsPerRow)
	}
	if len(db.args[1]) != reducerInputInvalidFactColumnsPerRow {
		t.Fatalf("second batch arg count = %d, want %d (one leftover row)", len(db.args[1]), reducerInputInvalidFactColumnsPerRow)
	}

	// Empty input must be a true no-op: no statements at all.
	if err := store.WriteQuarantinedFacts(context.Background(), nil); err != nil {
		t.Fatalf("WriteQuarantinedFacts(nil) error = %v", err)
	}
	if len(db.statements) != 2 {
		t.Fatalf("statement count after empty write = %d, want unchanged 2", len(db.statements))
	}
}
