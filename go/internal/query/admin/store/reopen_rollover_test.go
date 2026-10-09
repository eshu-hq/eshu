// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestReopenCompletedWorkSeesRolloverBetweenResolveAndCommit is the
// deterministic rollover proof for #7734: the pre-transaction resolve sees
// generation A, then a rollover commits, so the in-transaction fence sees
// generation B. The reopen must act on B — the generation that is active
// when the reopen transaction locks the scope — and never on the stale A.
// On the unfenced code the result carries A and no fence runs (RED).
func TestReopenCompletedWorkSeesRolloverBetweenResolveAndCommit(t *testing.T) {
	t.Parallel()

	const scopeID, generationA, generationB = "scope-x", "generation-a", "generation-b"
	database := &rolloverDatabase{scopeID: scopeID, generationID: generationA}
	tx := &rolloverTx{generationID: generationB}
	s := &postgresStore{
		database: database,
		beginner: &rolloverBeginner{tx: tx},
		now:      func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	result, err := s.ReopenCompletedWork(context.Background(), admin.ReopenFilter{
		Domain:  admin.ReopenDomainWorkloadMaterialization,
		ScopeID: scopeID,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("ReopenCompletedWork() error = %v", err)
	}
	if !tx.fenceRan {
		t.Fatal("reopen transaction must lock the scope row and re-resolve the generation (fence did not run)")
	}
	if result.GenerationID != generationB {
		t.Fatalf("result generation = %q, want %q (the generation active at fence time)", result.GenerationID, generationB)
	}
	if tx.candidatesGeneration != generationB {
		t.Fatalf("candidates generation = %q, want %q (updates must target the fenced generation)", tx.candidatesGeneration, generationB)
	}
	if !tx.committed {
		t.Fatal("fenced reopen with no candidates must still commit")
	}
}

// TestReopenCompletedWorkFenceAgreesWithoutRollover is the control: with no
// rollover the fence re-resolves the same generation and the reopen acts
// on it.
func TestReopenCompletedWorkFenceAgreesWithoutRollover(t *testing.T) {
	t.Parallel()

	const scopeID, generationA = "scope-x", "generation-a"
	database := &rolloverDatabase{scopeID: scopeID, generationID: generationA}
	tx := &rolloverTx{generationID: generationA}
	s := &postgresStore{
		database: database,
		beginner: &rolloverBeginner{tx: tx},
		now:      func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	result, err := s.ReopenCompletedWork(context.Background(), admin.ReopenFilter{
		Domain:  admin.ReopenDomainWorkloadMaterialization,
		ScopeID: scopeID,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("ReopenCompletedWork() error = %v", err)
	}
	if !tx.fenceRan {
		t.Fatal("fence must run on every reopen, with or without a rollover")
	}
	if result.GenerationID != generationA {
		t.Fatalf("result generation = %q, want %q", result.GenerationID, generationA)
	}
}

// rolloverDatabase answers the pre-transaction scope and generation
// resolves: the world before the planted rollover.
type rolloverDatabase struct {
	scopeID      string
	generationID string
}

func (database *rolloverDatabase) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	switch {
	case query == reopenActiveGenerationQuery:
		return &rolloverRows{cells: [][]any{{sql.NullString{String: database.generationID, Valid: true}}}}, nil
	case strings.Contains(query, "FROM ingestion_scopes"):
		return &rolloverRows{cells: [][]any{{database.scopeID}}}, nil
	default:
		return nil, fmt.Errorf("unexpected pre-transaction query: %q", query)
	}
}

func (*rolloverDatabase) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected pre-transaction exec")
}

// rolloverBeginner hands out the scripted transaction: the world after the
// planted rollover committed.
type rolloverBeginner struct {
	tx *rolloverTx
}

func (beginner *rolloverBeginner) Begin(context.Context) (db.Transaction, error) {
	return beginner.tx, nil
}

// rolloverTx serves the in-transaction statements. The generation resolve
// answers the post-rollover generation; candidates come back empty so the
// test pins the fenced generation without row fixtures.
type rolloverTx struct {
	generationID         string
	fenceRan             bool
	committed            bool
	candidatesGeneration string
}

func (tx *rolloverTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "FOR UPDATE") && strings.Contains(query, "FROM ingestion_scopes"):
		tx.fenceRan = true
		return &rolloverRows{cells: [][]any{{"scope-x"}}}, nil
	case query == reopenActiveGenerationQuery:
		return &rolloverRows{cells: [][]any{{sql.NullString{String: tx.generationID, Valid: true}}}}, nil
	case query == reopenReducerCandidatesQuery:
		if len(args) >= 2 {
			if generation, ok := args[1].(string); ok {
				tx.candidatesGeneration = generation
			}
		}
		return &rolloverRows{}, nil
	default:
		return nil, fmt.Errorf("unexpected transaction query: %q", query)
	}
}

func (*rolloverTx) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return rolloverResult{}, nil
}

func (tx *rolloverTx) Commit() error {
	tx.committed = true
	return nil
}

func (*rolloverTx) Rollback() error { return nil }

// rolloverRows yields fixed single-column cells, supporting the *string and
// *sql.NullString scans the resolve path uses.
type rolloverRows struct {
	cells [][]any
	pos   int
}

func (rows *rolloverRows) Next() bool {
	return rows.pos < len(rows.cells)
}

func (rows *rolloverRows) Scan(dest ...any) error {
	if len(dest) != 1 || len(rows.cells[rows.pos]) != 1 {
		return fmt.Errorf("rolloverRows scans one column, got %d destinations", len(dest))
	}
	switch out := dest[0].(type) {
	case *string:
		value, ok := rows.cells[rows.pos][0].(string)
		if !ok {
			return fmt.Errorf("rolloverRows cell is %T, want string", rows.cells[rows.pos][0])
		}
		*out = value
	case *sql.NullString:
		value, ok := rows.cells[rows.pos][0].(sql.NullString)
		if !ok {
			return fmt.Errorf("rolloverRows cell is %T, want sql.NullString", rows.cells[rows.pos][0])
		}
		*out = value
	default:
		return fmt.Errorf("rolloverRows cannot scan into %T", dest[0])
	}
	rows.pos++
	return nil
}

func (*rolloverRows) Err() error   { return nil }
func (*rolloverRows) Close() error { return nil }

// rolloverResult is a no-op sql.Result for SET LOCAL and other execs.
type rolloverResult struct{}

func (rolloverResult) LastInsertId() (int64, error) { return 0, nil }
func (rolloverResult) RowsAffected() (int64, error) { return 0, nil }
