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

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

// TestBuildMutatingWorkItemsQuery_ReplayReturnsPriorFailureClass pins the
// #7387 contract: only the replay carries the selection-time failure_class as
// a trailing column, because its update clears the class. The dead-letter
// mutation keeps the post-update class and must not gain the column.
func TestBuildMutatingWorkItemsQuery_ReplayReturnsPriorFailureClass(t *testing.T) {
	t.Parallel()

	replay, replayArgs := buildMutatingWorkItemsQuery(nil, "", "reducer", "", 10, 0, true, true, "SET status = 'pending'\n")
	if !strings.Contains(replay, "selected.failure_class AS replayed_failure_class") {
		t.Fatalf("replay query lacks the prior-class column:\n%s", replay)
	}
	if got, want := maxPlaceholderIndex(replay), len(replayArgs); got != want {
		t.Fatalf("replay max placeholder index = %d, want %d; query = %s", got, want, replay)
	}

	deadLetter, deadArgs := buildMutatingWorkItemsQuery(nil, "", "reducer", "", 10, 0, false, false, "SET status = 'dead_letter'\n")
	if strings.Contains(deadLetter, "replayed_failure_class") {
		t.Fatalf("dead-letter query must not carry the replay audit column:\n%s", deadLetter)
	}
	if got, want := maxPlaceholderIndex(deadLetter), len(deadArgs); got != want {
		t.Fatalf("dead-letter max placeholder index = %d, want %d; query = %s", got, want, deadLetter)
	}
}

// TestReplayFailedWorkItems_RecordsPriorFailureClassInEvent proves the replay
// event carries the class the row had when replayed, while the returned item
// keeps post-update truth (pending, no class). The scripted row mimics the
// fixed statement: cleared work columns plus a populated trailing
// replayed_failure_class.
func TestReplayFailedWorkItems_RecordsPriorFailureClassInEvent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	database := &replayClassQueryer{
		rows: &replayClassRows{values: []any{
			"wi-1", "scope-1", "gen-1", "reducer", "domain-1", "pending", 3,
			nil, nil, nil, now, now, nil, nil, "projection_bug",
		}},
	}
	store := &postgresStore{database: database, now: func() time.Time { return now }}

	items, err := store.ReplayFailedWorkItems(context.Background(), admin.ReplayWorkItemFilter{
		WorkItemIDs: []string{"wi-1"}, OperatorNote: "retry", Limit: 1,
	})
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}
	if len(items) != 1 {
		t.Fatalf("replayed %d items, want one", len(items))
	}
	if items[0].Status != "pending" || items[0].FailureClass != nil {
		t.Fatalf("replayed item = status %q class %+v, want pending with nil class",
			items[0].Status, items[0].FailureClass)
	}
	if !strings.Contains(database.query, "replayed_failure_class") {
		t.Fatalf("replay statement lacks the prior-class column:\n%s", database.query)
	}
	if len(database.execs) != 1 {
		t.Fatalf("replay event inserts = %d, want one", len(database.execs))
	}
	event := database.execs[0]
	if len(event) != 7 {
		t.Fatalf("replay event args = %d, want seven", len(event))
	}
	if got, want := event[4], "projection_bug"; got != want {
		t.Fatalf("replay event failure_class = %#v, want %#v", got, want)
	}
	if got, want := event[5], "retry"; got != want {
		t.Fatalf("replay event operator_note = %#v, want %#v", got, want)
	}
}

type replayClassQueryer struct {
	query     string
	queryArgs []any
	rows      db.Rows
	execs     [][]any
}

func (database *replayClassQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	database.query = query
	database.queryArgs = append([]any(nil), args...)
	return database.rows, nil
}

func (database *replayClassQueryer) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	database.execs = append(database.execs, append([]any(nil), args...))
	return replayClassResult{}, nil
}

type replayClassResult struct{}

func (replayClassResult) LastInsertId() (int64, error) { return 0, nil }
func (replayClassResult) RowsAffected() (int64, error) { return 1, nil }

// replayClassRows scripts one replay row with native driver values, including
// NULLs, which the shared string-only script helper cannot express.
type replayClassRows struct {
	values []any
	done   bool
}

func (r *replayClassRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *replayClassRows) Err() error   { return nil }
func (r *replayClassRows) Close() error { return nil }

func (r *replayClassRows) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan arity %d != %d", len(dest), len(r.values))
	}
	for i, d := range dest {
		switch target := d.(type) {
		case *string:
			value, ok := r.values[i].(string)
			if !ok {
				return fmt.Errorf("column %d = %#v, want string", i, r.values[i])
			}
			*target = value
		case *int:
			value, ok := r.values[i].(int)
			if !ok {
				return fmt.Errorf("column %d = %#v, want int", i, r.values[i])
			}
			*target = value
		case *time.Time:
			value, ok := r.values[i].(time.Time)
			if !ok {
				return fmt.Errorf("column %d = %#v, want time", i, r.values[i])
			}
			*target = value
		case *sql.NullString:
			if r.values[i] == nil {
				*target = sql.NullString{}
				continue
			}
			value, ok := r.values[i].(string)
			if !ok {
				return fmt.Errorf("column %d = %#v, want string or nil", i, r.values[i])
			}
			*target = sql.NullString{String: value, Valid: true}
		case *sql.NullTime:
			if r.values[i] == nil {
				*target = sql.NullTime{}
				continue
			}
			value, ok := r.values[i].(time.Time)
			if !ok {
				return fmt.Errorf("column %d = %#v, want time or nil", i, r.values[i])
			}
			*target = sql.NullTime{Time: value, Valid: true}
		default:
			return fmt.Errorf("unsupported scan target %T", d)
		}
	}
	return nil
}
