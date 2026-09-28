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
	// missingKeyIndexes answers the key-index precondition query: the names
	// of the #7279 key indexes the fake database reports missing or invalid.
	missingKeyIndexes []string
	// recountRows, when set, answers every row-count query after the first:
	// the recount the store issues after a row-limit skip.
	recountRows [][]any
	countCalls  int
	// ledgerCountRows answers the changed-since ledger row-count statement
	// (generation_id, table, count); ledgerDeleted is the ledger prune's
	// result row (links, deltas, buckets, activations). Both default to none.
	ledgerCountRows [][]any
	ledgerDeleted   []any
	// ledgerRecountRows, when set, answers every ledger count after the
	// first, as recountRows does for the main count.
	ledgerRecountRows [][]any
	ledgerCountCalls  int
	// ledgerCountScripts, when set, answers ledger count call i with entry i
	// (the last entry repeats), for fixtures whose recounts differ per round.
	ledgerCountScripts [][][]any
	// targetedLockMiss makes the targeted candidate lock query return no row,
	// as when another session holds the candidate after the selection's
	// savepoint rollback.
	targetedLockMiss bool
	execResults      []sql.Result
	queries          []fakeQueryCall
	execs            []fakeExecCall
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
	case strings.Contains(query, "generation_retention_key_indexes"):
		rows := make([][]any, 0, len(tx.database.missingKeyIndexes))
		for _, name := range tx.database.missingKeyIndexes {
			rows = append(rows, []any{name})
		}
		return &queueFakeRows{rows: rows}, nil
	case strings.Contains(query, "ranked_superseded_generations"):
		return &queueFakeRows{rows: generationRetentionCandidateFakeRows(tx.database.candidateRows, args)}, nil
	case strings.Contains(query, "generation_retention_row_counts"):
		tx.database.countCalls++
		rows := tx.database.countRows
		if tx.database.countCalls > 1 && tx.database.recountRows != nil {
			rows = tx.database.recountRows
		}
		return &queueFakeRows{rows: generationRetentionCountFakeRows(rows, args)}, nil
	case strings.Contains(query, "retention: targeted candidate lock"):
		if tx.database.targetedLockMiss || len(args) < 5 {
			return &queueFakeRows{}, nil
		}
		generationID, _ := args[4].(string)
		for _, row := range tx.database.candidateRows {
			if id, _ := row[1].(string); id == generationID {
				return &queueFakeRows{rows: [][]any{row}}, nil
			}
		}
		return &queueFakeRows{}, nil
	case strings.Contains(query, "del_activations"):
		deleted := tx.database.ledgerDeleted
		if deleted == nil {
			deleted = []any{int64(0), int64(0), int64(0), int64(0)}
		}
		// The expected counts equal the deleted ones: no concurrent writer.
		row := append(append([]any{}, deleted...), deleted[0], deleted[1], deleted[2])
		return &queueFakeRows{rows: [][]any{row}}, nil
	case strings.Contains(query, "doomed AS MATERIALIZED"):
		// The ledger count takes (scope ids, generation ids); filter on the
		// generation ids.
		if len(args) < 2 {
			return nil, sql.ErrNoRows
		}
		tx.database.ledgerCountCalls++
		rows := tx.database.ledgerCountRows
		if tx.database.ledgerCountCalls > 1 && tx.database.ledgerRecountRows != nil {
			rows = tx.database.ledgerRecountRows
		}
		if scripts := tx.database.ledgerCountScripts; len(scripts) > 0 {
			rows = scripts[min(tx.database.ledgerCountCalls, len(scripts))-1]
		}
		return &queueFakeRows{rows: generationRetentionCountFakeRows(rows, args[1:])}, nil
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
	// Transaction-local settings and savepoints are recorded in statements
	// but skip the positional exec scripts, which follow the deletes.
	for _, prefix := range []string{"SET LOCAL ", "SAVEPOINT ", "RELEASE SAVEPOINT ", "ROLLBACK TO SAVEPOINT "} {
		if strings.HasPrefix(query, prefix) {
			return fakeResult{}, nil
		}
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
