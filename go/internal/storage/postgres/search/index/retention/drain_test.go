// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

var testGeneration = Generation{ScopeID: "scope-a", GenerationID: "gen-old"}

// scriptByTable answers the try lock with true and each table's batches from
// its own queue, so a test scripts the terms and documents walks apart.
func scriptByTable(terms, documents [][][]any) func(string, []any) ([][]any, error) {
	return func(query string, _ []any) ([][]any, error) {
		switch {
		case isTryLock(query):
			return [][]any{{true}}, nil
		case isTermsBatch(query):
			next := terms[0]
			terms = terms[1:]
			return next, nil
		case isDocumentsBatch(query):
			next := documents[0]
			documents = documents[1:]
			return next, nil
		}
		return nil, errors.New("unexpected statement: " + query)
	}
}

func TestDrainWalksKeysetThenSweepsUntilAFromStartPageIsEmpty(t *testing.T) {
	var cursors [][2]string
	terms := [][][]any{
		page(true, BatchRows, BatchRows, "k1", "d1"), // full page: advance to (k1, d1)
		page(true, 5, 5, "k9", "d9"),                 // short page: re-sweep from the start
		page(true, 0, 0, "", ""),                     // from-start page empty: drained
	}
	documents := [][][]any{page(true, 0, 0, "", "")}
	answer := scriptByTable(terms, documents)
	database := &scriptedDB{answer: func(query string, args []any) ([][]any, error) {
		if isTermsBatch(query) {
			cursors = append(cursors, [2]string{args[2].(string), args[3].(string)})
		}
		return answer(query, args)
	}}
	budget := &Budget{RowCap: 1 << 40}
	report, err := Drain(context.Background(), database, testGeneration, budget)
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if !report.Drained() || report.TermsDeleted != BatchRows+5 || report.DocumentsDeleted != 0 {
		t.Fatalf("report = %+v, want drained with %d terms deleted", report, BatchRows+5)
	}
	want := [][2]string{{"", ""}, {"k1", "d1"}, {"", ""}}
	if len(cursors) != len(want) {
		t.Fatalf("terms cursors = %v, want %v", cursors, want)
	}
	for i := range want {
		if cursors[i] != want[i] {
			t.Fatalf("terms cursor %d = %v, want %v (keyset, then a from-start re-sweep)", i, cursors[i], want[i])
		}
	}
	if report.Batches != 4 || budget.Batches() != 4 || database.commits != 4 {
		t.Fatalf("batches = %d, budget = %d, commits = %d, want 4 each", report.Batches, budget.Batches(), database.commits)
	}
}

// TestDrainResweepRemovesARowInsertedBehindTheCursor: a row that lands behind
// the keyset cursor is found by the from-start re-sweep, which repeats until
// a from-start page comes back empty.
func TestDrainResweepRemovesARowInsertedBehindTheCursor(t *testing.T) {
	terms := [][][]any{
		page(true, 3, 3, "k3", "d3"), // short first page
		page(true, 1, 1, "k0", "d0"), // re-sweep finds the late row
		page(true, 0, 0, "", ""),     // confirmed empty
	}
	database := &scriptedDB{answer: scriptByTable(terms, [][][]any{page(true, 0, 0, "", "")})}
	report, err := Drain(context.Background(), database, testGeneration, &Budget{RowCap: 1 << 40})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if !report.Drained() || report.TermsDeleted != 4 {
		t.Fatalf("report = %+v, want drained with the late row deleted", report)
	}
	if report.SweepDuration <= 0 {
		t.Fatalf("SweepDuration = %v, want the re-sweep timed apart from the keyset walk", report.SweepDuration)
	}
}

func TestDrainEndsOnABusyLockWithoutRunningABatch(t *testing.T) {
	database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
		if isTryLock(query) {
			return [][]any{{false}}, nil
		}
		return nil, errors.New("batch must not run without the lock: " + query)
	}}
	report, err := Drain(context.Background(), database, testGeneration, &Budget{RowCap: 1 << 40})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if report.Outcome != OutcomeLockBusy || report.Drained() || database.commits != 0 || database.rollbacks != 1 {
		t.Fatalf("report = %+v commits = %d rollbacks = %d, want lock busy, rolled back", report, database.commits, database.rollbacks)
	}
}

func TestDrainEndsOnAnIneligibleGeneration(t *testing.T) {
	database := &scriptedDB{answer: scriptByTable([][][]any{page(false, 0, 0, "", "")}, nil)}
	report, err := Drain(context.Background(), database, testGeneration, &Budget{RowCap: 1 << 40})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if report.Outcome != OutcomeIneligible || database.commits != 0 {
		t.Fatalf("report = %+v commits = %d, want ineligible and nothing committed", report, database.commits)
	}
}

func TestDrainYieldsOnLockTimeoutAndDeadlock(t *testing.T) {
	for _, code := range []string{"55P03", "40P01"} {
		database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
			if isTryLock(query) {
				return [][]any{{true}}, nil
			}
			return nil, &pgconn.PgError{Code: code}
		}}
		report, err := Drain(context.Background(), database, testGeneration, &Budget{RowCap: 1 << 40})
		if err != nil {
			t.Fatalf("%s: Drain() error = %v, want a yield outcome", code, err)
		}
		if report.Outcome != OutcomeLockYield || database.commits != 0 {
			t.Fatalf("%s: report = %+v commits = %d, want lock yield", code, report, database.commits)
		}
	}
}

func TestDrainReturnsOtherBatchErrors(t *testing.T) {
	database := &scriptedDB{answer: func(query string, _ []any) ([][]any, error) {
		if isTryLock(query) {
			return [][]any{{true}}, nil
		}
		return nil, &pgconn.PgError{Code: "42P01"}
	}}
	if _, err := Drain(context.Background(), database, testGeneration, &Budget{RowCap: 1 << 40}); err == nil {
		t.Fatal("Drain() error = nil, want the batch error")
	}
}

func TestDrainStopsWhenAPageDeletesNothing(t *testing.T) {
	database := &scriptedDB{answer: scriptByTable([][][]any{page(true, 3, 0, "k", "d")}, nil)}
	report, err := Drain(context.Background(), database, testGeneration, &Budget{RowCap: 1 << 40})
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if report.Outcome != OutcomeStalled {
		t.Fatalf("Outcome = %q, want %q", report.Outcome, OutcomeStalled)
	}
}

// TestDrainRunsOneBatchThenHonorsTheBudget: the first batch of a pass always
// runs, then the row cap or the deadline stops the drain undrained.
func TestDrainRunsOneBatchThenHonorsTheBudget(t *testing.T) {
	for name, budget := range map[string]*Budget{
		"row cap":  {RowCap: 1},
		"deadline": {RowCap: 1 << 40, Deadline: time.Now().Add(-time.Hour)},
	} {
		database := &scriptedDB{answer: scriptByTable([][][]any{page(true, BatchRows, BatchRows, "k", "d")}, nil)}
		report, err := Drain(context.Background(), database, testGeneration, budget)
		if err != nil {
			t.Fatalf("%s: Drain() error = %v", name, err)
		}
		if report.Outcome != OutcomeBudget || report.Batches != 1 || report.TermsDeleted != BatchRows {
			t.Fatalf("%s: report = %+v, want one batch then budget_exhausted", name, report)
		}
	}
}

// TestDrainHonorsCancellationBetweenBatches cancels after the first committed
// batch; the drain returns the error before starting another.
func TestDrainHonorsCancellationBetweenBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	database := &scriptedDB{answer: scriptByTable([][][]any{
		page(true, BatchRows, BatchRows, "k", "d"),
		page(true, BatchRows, BatchRows, "k2", "d2"),
	}, nil)}
	budget := &Budget{RowCap: 1 << 40, afterBatch: cancel}
	report, err := Drain(ctx, database, testGeneration, budget)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Drain() error = %v, want context.Canceled", err)
	}
	if report.Batches != 1 || report.TermsDeleted != BatchRows || report.Drained() {
		t.Fatalf("report = %+v, want one committed batch and not drained", report)
	}
}

// TestPassDrainsOldestFirstAndLeavesTheRestUndrained: once the budget is
// spent, the pass attempts no further generation and none of them counts as
// drained.
func TestPassDrainsOldestFirstAndLeavesTheRestUndrained(t *testing.T) {
	database := &scriptedDB{answer: scriptByTable(
		[][][]any{page(true, BatchRows, BatchRows, "k", "d")}, nil)}
	pass := NewPass(1, time.Time{}, nil)
	older := Generation{ScopeID: "scope-a", GenerationID: "gen-older"}
	newer := Generation{ScopeID: "scope-a", GenerationID: "gen-newer"}
	if err := pass.Run(context.Background(), database, []Generation{older, newer}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !pass.Ran() || pass.Drained(older.GenerationID) || pass.Drained(newer.GenerationID) {
		t.Fatal("want the pass to run and neither generation drained")
	}
	if _, attempted := pass.Report(newer.GenerationID); attempted {
		t.Fatal("newer generation was attempted after the budget ran out")
	}
	terms, documents, batches, _, _ := pass.Totals()
	if terms != BatchRows || documents != 0 || batches != 1 {
		t.Fatalf("Totals() = %d terms, %d documents, %d batches, want %d, 0, 1", terms, documents, batches, BatchRows)
	}
}

func TestBudgetExhausted(t *testing.T) {
	now := time.Now()
	if (&Budget{}).Exhausted(now) {
		t.Fatal("a budget with no batch yet must allow the first batch")
	}
	spent := &Budget{RowCap: 10, rows: 10, batches: 1}
	if !spent.Exhausted(now) {
		t.Fatal("rows at the cap must exhaust the budget")
	}
	late := &Budget{RowCap: 10, batches: 1, Deadline: now}
	if !late.Exhausted(now) {
		t.Fatal("reaching the deadline must exhaust the budget")
	}
	open := &Budget{RowCap: 10, rows: 9, batches: 1, Deadline: now.Add(time.Minute)}
	if open.Exhausted(now) {
		t.Fatal("rows under the cap before the deadline must not exhaust the budget")
	}
}

// TestDrainCountsOnlyCommittedBatches pins #8276 review thread A: Batches
// (and so SearchIndexBatches and the search_index_batches histogram) counts
// the batch transactions that committed. A busy lock, an ineligible
// generation or a lock yield rolls back and is not counted, but it still
// spends the pass's first-batch allowance, so a pass past its deadline does
// not try one generation after another.
func TestDrainCountsOnlyCommittedBatches(t *testing.T) {
	busy := func(query string, _ []any) ([][]any, error) {
		if isTryLock(query) {
			return [][]any{{false}}, nil
		}
		return nil, errors.New("unexpected statement: " + query)
	}
	yield := func(query string, _ []any) ([][]any, error) {
		if isTryLock(query) {
			return [][]any{{true}}, nil
		}
		return nil, &pgconn.PgError{Code: "55P03"}
	}
	committedThenYield := func() func(string, []any) ([][]any, error) {
		terms := 0
		return func(query string, _ []any) ([][]any, error) {
			switch {
			case isTryLock(query):
				return [][]any{{true}}, nil
			case isTermsBatch(query) && terms == 0:
				terms++
				return page(true, BatchRows, BatchRows, "a", "b"), nil
			}
			return nil, &pgconn.PgError{Code: "40P01"}
		}
	}
	for _, tc := range []struct {
		name    string
		answer  func(string, []any) ([][]any, error)
		outcome Outcome
		want    int
	}{
		{"lock-busy", busy, OutcomeLockBusy, 0},
		{"ineligible", scriptByTable([][][]any{page(false, 0, 0, "", "")}, nil), OutcomeIneligible, 0},
		{"lock-yield", yield, OutcomeLockYield, 0},
		{"one-commit-then-yield", committedThenYield(), OutcomeLockYield, 1},
	} {
		database := &scriptedDB{answer: tc.answer}
		budget := &Budget{RowCap: 1 << 40}
		report, err := Drain(context.Background(), database, testGeneration, budget)
		if err != nil {
			t.Fatalf("%s: Drain() error = %v", tc.name, err)
		}
		if report.Outcome != tc.outcome || report.Batches != tc.want || budget.Batches() != tc.want || database.commits != tc.want {
			t.Fatalf("%s: outcome %s, report batches %d, budget batches %d, commits %d; want %s and %d committed",
				tc.name, report.Outcome, report.Batches, budget.Batches(), database.commits, tc.outcome, tc.want)
		}
	}

	late := &Budget{RowCap: 1 << 40, Deadline: time.Now().Add(-time.Second)}
	if report, err := Drain(context.Background(), &scriptedDB{answer: busy}, testGeneration, late); err != nil || report.Outcome != OutcomeLockBusy {
		t.Fatalf("first attempt past the deadline: report %+v err %v, want one busy attempt", report, err)
	}
	report, err := Drain(context.Background(), &scriptedDB{answer: busy}, Generation{ScopeID: "scope-b", GenerationID: "gen-b"}, late)
	if err != nil || report.Outcome != OutcomeBudget {
		t.Fatalf("second generation past the deadline: report %+v err %v, want budget exhausted by the rolled-back attempt", report, err)
	}
}
