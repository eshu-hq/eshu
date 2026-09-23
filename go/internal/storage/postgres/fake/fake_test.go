// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

func TestExecQueryerExecContextRecordsCallsAndDefaultsToOneRowAffected(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{}
	result, err := database.ExecContext(context.Background(), "UPDATE t SET x = $1", 1)
	if err != nil {
		t.Fatalf("ExecContext() error = %v, want nil", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		t.Fatalf("RowsAffected() = (%d, %v), want (1, nil)", affected, err)
	}
	if got, want := len(database.Execs), 1; got != want {
		t.Fatalf("len(Execs) = %d, want %d", got, want)
	}
	if got, want := database.Execs[0].Query, "UPDATE t SET x = $1"; got != want {
		t.Fatalf("Execs[0].Query = %q, want %q", got, want)
	}
}

func TestExecQueryerExecContextConsumesInjectedErrorsThenResultsFIFO(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	database := &fake.ExecQueryer{
		ExecErrors: []error{boom},
	}
	if _, err := database.ExecContext(context.Background(), "q1"); !errors.Is(err, boom) {
		t.Fatalf("ExecContext() error = %v, want %v", err, boom)
	}

	database = &fake.ExecQueryer{
		ExecResults: []sql.Result{fake.Result{}},
	}
	result, err := database.ExecContext(context.Background(), "q1")
	if err != nil {
		t.Fatalf("ExecContext() error = %v, want nil", err)
	}
	if result != (fake.Result{}) {
		t.Fatalf("ExecContext() result = %#v, want the staged fake.Result", result)
	}
	// The staged result queue is drained: a second call falls back to the
	// default one-row-affected result.
	result, err = database.ExecContext(context.Background(), "q2")
	if err != nil {
		t.Fatalf("ExecContext() error = %v, want nil", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Fatalf("second ExecContext() RowsAffected = %d, want 1", affected)
	}
}

func TestExecQueryerQueryContextServesQueryResponsesFIFO(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{
			{Data: [][]any{{"first"}}},
			{Data: [][]any{{"second"}}},
		},
	}

	var got string
	rows, err := database.QueryContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("QueryContext() error = %v, want nil", err)
	}
	if !rows.Next() {
		t.Fatal("Next() = false, want true")
	}
	if err := rows.Scan(&got); err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got != "first" {
		t.Fatalf("Scan() = %q, want %q", got, "first")
	}

	rows, err = database.QueryContext(context.Background(), "SELECT 2")
	if err != nil {
		t.Fatalf("QueryContext() error = %v, want nil", err)
	}
	rows.Next()
	rows.Scan(&got)
	if got != "second" {
		t.Fatalf("Scan() = %q, want %q", got, "second")
	}

	if got, want := len(database.Queries), 2; got != want {
		t.Fatalf("len(Queries) = %d, want %d", got, want)
	}
}

func TestExecQueryerQueryContextUnexpectedQueryNamesTheQuery(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{}
	_, err := database.QueryContext(context.Background(), "SELECT unexpected")
	if err == nil {
		t.Fatal("QueryContext() error = nil, want an unexpected-query error")
	}
	if !strings.Contains(err.Error(), "SELECT unexpected") {
		t.Fatalf("QueryContext() error = %v, want it to name the query", err)
	}
}

func TestExecQueryerQueryContextRoutePrecedesFIFOQueue(t *testing.T) {
	t.Parallel()

	routed := &fake.Rows{Data: [][]any{{"routed"}}}
	database := &fake.ExecQueryer{
		Routes: []fake.Route{
			func(query string, _ []any) (*fake.Rows, bool) {
				if query == "SELECT routed" {
					return routed, true
				}
				return nil, false
			},
		},
		QueryResponses: []fake.Rows{
			{Data: [][]any{{"fifo"}}},
		},
	}

	rows, err := database.QueryContext(context.Background(), "SELECT routed")
	if err != nil {
		t.Fatalf("QueryContext() error = %v, want nil", err)
	}
	var got string
	rows.Next()
	rows.Scan(&got)
	if got != "routed" {
		t.Fatalf("Scan() = %q, want %q", got, "routed")
	}
	// The FIFO queue was not consumed by the routed call.
	if got, want := len(database.QueryResponses), 1; got != want {
		t.Fatalf("len(QueryResponses) = %d, want %d (unconsumed)", got, want)
	}

	rows, err = database.QueryContext(context.Background(), "SELECT unrouted")
	if err != nil {
		t.Fatalf("QueryContext() error = %v, want nil", err)
	}
	rows.Next()
	rows.Scan(&got)
	if got != "fifo" {
		t.Fatalf("Scan() = %q, want %q", got, "fifo")
	}
}

func TestExecQueryerQueryContextRouteCanFailTheCall(t *testing.T) {
	t.Parallel()

	boom := errors.New("route boom")
	database := &fake.ExecQueryer{
		Routes: []fake.Route{
			func(string, []any) (*fake.Rows, bool) {
				return &fake.Rows{FailWith: boom}, true
			},
		},
	}
	if _, err := database.QueryContext(context.Background(), "SELECT anything"); !errors.Is(err, boom) {
		t.Fatalf("QueryContext() error = %v, want %v", err, boom)
	}
}

func TestExecQueryerQueryContextRouteReturningNilRowsErrorsInsteadOfPanicking(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{
		Routes: []fake.Route{
			func(string, []any) (*fake.Rows, bool) {
				return nil, true
			},
		},
	}
	_, err := database.QueryContext(context.Background(), "SELECT nil rows")
	if err == nil {
		t.Fatal("QueryContext() error = nil, want an error naming the query")
	}
	if !strings.Contains(err.Error(), "SELECT nil rows") {
		t.Fatalf("QueryContext() error = %v, want it to name the query", err)
	}
}

func TestExecQueryerQueryContextRouteResponseIsCopiedNotSharedAcrossCalls(t *testing.T) {
	t.Parallel()

	shared := &fake.Rows{Data: [][]any{{"first"}, {"second"}}}
	database := &fake.ExecQueryer{
		Routes: []fake.Route{
			func(string, []any) (*fake.Rows, bool) {
				return shared, true
			},
		},
	}

	for i := 0; i < 2; i++ {
		rows, err := database.QueryContext(context.Background(), "SELECT repeated")
		if err != nil {
			t.Fatalf("QueryContext() call %d error = %v, want nil", i, err)
		}
		if !rows.Next() {
			t.Fatalf("call %d: Next() = false, want true", i)
		}
		var got string
		if err := rows.Scan(&got); err != nil {
			t.Fatalf("call %d: Scan() error = %v, want nil", i, err)
		}
		if got != "first" {
			t.Fatalf("call %d: Scan() = %q, want %q (route's shared Rows must not be consumed)", i, got, "first")
		}
	}
}

func TestExecQueryerQueryContextFIFOEntryCanFailTheCall(t *testing.T) {
	t.Parallel()

	boom := errors.New("fifo boom")
	database := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{FailWith: boom}},
	}
	if _, err := database.QueryContext(context.Background(), "SELECT anything"); !errors.Is(err, boom) {
		t.Fatalf("QueryContext() error = %v, want %v", err, boom)
	}
}

func TestExecQueryerIsSafeForConcurrentExecAndQuery(t *testing.T) {
	database := &fake.ExecQueryer{}
	const workers = 50

	var wg sync.WaitGroup
	wg.Add(workers * 2)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			database.ExecContext(context.Background(), "UPDATE t SET x = 1")
		}()
		go func() {
			defer wg.Done()
			database.QueryContext(context.Background(), "SELECT 1")
		}()
	}
	wg.Wait()

	if got, want := len(database.Execs), workers; got != want {
		t.Fatalf("len(Execs) = %d, want %d", got, want)
	}
	if got, want := len(database.Queries), workers; got != want {
		t.Fatalf("len(Queries) = %d, want %d", got, want)
	}
}
