// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type generationRetentionFakeDB struct {
	candidateRows [][]any
	countRows     [][]any
	// recountRows, when set, answers every row-count query after the first:
	// the recount the store issues after a row-limit skip.
	recountRows [][]any
	countCalls  int
	execResults []sql.Result
	queries     []fakeQueryCall
	execs       []fakeExecCall
	// statements records every statement in the order the transaction issued
	// it, reads and writes together, including the transaction-local setting
	// statement that execs and execResults deliberately skip so the positional
	// exec scripts stay aligned with the retention deletes.
	statements []string
}

func (database *generationRetentionFakeDB) Begin(context.Context) (db.Transaction, error) {
	return &generationRetentionFakeTx{database: database}, nil
}

func (database *generationRetentionFakeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, sql.ErrConnDone
}

func (database *generationRetentionFakeDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, sql.ErrConnDone
}

type generationRetentionFakeTx struct {
	database *generationRetentionFakeDB
}

func (tx *generationRetentionFakeTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	tx.database.queries = append(tx.database.queries, fakeQueryCall{query: query, args: args})
	tx.database.statements = append(tx.database.statements, query)
	switch {
	case strings.Contains(query, "ranked_superseded_generations"):
		return &queueFakeRows{rows: generationRetentionCandidateFakeRows(tx.database.candidateRows, args)}, nil
	case strings.Contains(query, "generation_retention_row_counts"):
		tx.database.countCalls++
		rows := tx.database.countRows
		if tx.database.countCalls > 1 && tx.database.recountRows != nil {
			rows = tx.database.recountRows
		}
		return &queueFakeRows{rows: generationRetentionCountFakeRows(rows, args)}, nil
	default:
		return nil, sql.ErrNoRows
	}
}

// generationRetentionCountFakeRows answers only for the generation ids the
// row-count query was given, as the real statement does.
func generationRetentionCountFakeRows(rows [][]any, args []any) [][]any {
	if len(args) == 0 {
		return rows
	}
	ids, ok := args[0].([]string)
	if !ok {
		return rows
	}
	filtered := make([][]any, 0, len(rows))
	for _, row := range rows {
		if generationID, _ := row[0].(string); slices.Contains(ids, generationID) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func generationRetentionCandidateFakeRows(rows [][]any, args []any) [][]any {
	limit := len(rows)
	if len(args) >= 3 {
		if queryLimit, ok := args[2].(int); ok && queryLimit >= 0 && queryLimit < limit {
			limit = queryLimit
		}
	}
	if len(args) < 4 {
		return rows[:limit]
	}
	excluded, ok := args[3].([]string)
	if !ok || len(excluded) == 0 {
		return rows[:limit]
	}
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, generationID := range excluded {
		excludedSet[generationID] = struct{}{}
	}
	filtered := make([][]any, 0, len(rows))
	for _, row := range rows {
		generationID, _ := row[1].(string)
		if _, ok := excludedSet[generationID]; ok {
			continue
		}
		filtered = append(filtered, row)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered
}

func (tx *generationRetentionFakeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	tx.database.statements = append(tx.database.statements, query)
	if strings.HasPrefix(query, "SET LOCAL ") {
		return fakeResult{}, nil
	}
	tx.database.execs = append(tx.database.execs, fakeExecCall{query: query, args: args})
	if len(tx.database.execResults) == 0 {
		return fakeResult{}, nil
	}
	result := tx.database.execResults[0]
	tx.database.execResults = tx.database.execResults[1:]
	return result, nil
}

func (tx *generationRetentionFakeTx) Commit() error { return nil }

func (tx *generationRetentionFakeTx) Rollback() error { return nil }

type fakeRowsAffected struct {
	n int64
}

func (r fakeRowsAffected) LastInsertId() (int64, error) { return 0, nil }

func (r fakeRowsAffected) RowsAffected() (int64, error) { return r.n, nil }
