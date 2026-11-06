// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

var fenceTestNow = time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)

type fakeResult int64

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return int64(r), nil }

// fakeRows serves a holder read, and records whether it was closed before the next
// statement, because a pgx transaction cannot run a statement over open rows.
type fakeRows struct {
	scopes []string
	index  int
	closed *bool
}

func (r *fakeRows) Next() bool { r.index++; return r.index <= len(r.scopes) }
func (r *fakeRows) Err() error { return nil }
func (r *fakeRows) Close() error {
	*r.closed = true
	return nil
}

func (r *fakeRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.scopes[r.index-1]
	return nil
}

// fakeFenceDB records every transaction's statements in order. detach and supersede
// answer those statements per fence in order; failOn makes the named statement fail.
type fakeFenceDB struct {
	detach       []int64
	supersede    []int64
	holders      []string
	failOn       string
	statements   []string
	args         [][]any
	commits      int
	rollbacks    int
	rowsClosed   bool
	reopened     []string
	reopenErr    error
	openOnReopen bool
}

func (f *fakeFenceDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("unexpected query outside a transaction")
}

func (f *fakeFenceDB) Begin(context.Context) (db.Transaction, error) {
	return &fakeFenceTx{db: f}, nil
}

func (f *fakeFenceDB) reopen(
	_ context.Context,
	_ db.ExecQueryer,
	holderScopeID, sourceScopeID, sourceGenerationID string,
	now time.Time,
) error {
	f.statements = append(f.statements, "reopen")
	f.reopened = append(f.reopened, holderScopeID+"<-"+sourceScopeID+"/"+sourceGenerationID)
	f.openOnReopen = f.openOnReopen || !f.rowsClosed
	if !now.Equal(fenceTestNow) {
		return errors.New("reopen did not receive the observed time")
	}
	return f.reopenErr
}

type fakeFenceTx struct {
	db   *fakeFenceDB
	done bool
}

func (t *fakeFenceTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	t.db.statements = append(t.db.statements, query)
	t.db.args = append(t.db.args, args)
	if query == t.db.failOn {
		return nil, errors.New("statement failed")
	}
	pop := func(queue *[]int64) fakeResult {
		if len(*queue) == 0 {
			return fakeResult(1)
		}
		affected := (*queue)[0]
		*queue = (*queue)[1:]
		return fakeResult(affected)
	}
	switch query {
	case fenceDetachPointerQuery:
		return pop(&t.db.detach), nil
	case fenceSupersedeGeneration:
		return pop(&t.db.supersede), nil
	}
	return fakeResult(1), nil
}

func (t *fakeFenceTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	t.db.statements = append(t.db.statements, query)
	t.db.args = append(t.db.args, args)
	if query == t.db.failOn {
		return nil, errors.New("statement failed")
	}
	t.db.rowsClosed = false
	return &fakeRows{scopes: t.db.holders, closed: &t.db.rowsClosed}, nil
}

func (t *fakeFenceTx) Commit() error {
	t.done = true
	t.db.commits++
	return nil
}

func (t *fakeFenceTx) Rollback() error {
	if !t.done {
		t.db.rollbacks++
	}
	t.done = true
	return nil
}

func (f *fakeFenceDB) store() Store {
	return NewStore(f).WithHolderReopener(f.reopen)
}

var testFences = []scannercomponent.StaleEvidenceFence{
	{ScopeID: "go-reachability://repository:r_00000001", GenerationID: "gen-1"},
	{ScopeID: "go-reachability://repository:r_00000002", GenerationID: "gen-2"},
}

// TestFenceOrderIsScopeThenGenerationThenHolders pins Amendment 1's lock order:
// ingestion_scopes, then scope_generations, then each holder's work items, all in one
// transaction. Every blocking path, Ack included, takes the scope row first, so taking
// the generation first can deadlock against an Ack.
func TestFenceOrderIsScopeThenGenerationThenHolders(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{holders: []string{"holder-a", "holder-b"}}
	result, err := database.store().FenceStaleGoEvidence(t.Context(), testFences[:1], fenceTestNow)
	if err != nil || result.Fenced != 1 || result.HoldersReopened != 2 {
		t.Fatalf("result = %+v, err = %v, want 1 fenced and 2 holders", result, err)
	}
	want := []string{
		fenceTimeoutsQuery, fenceDetachPointerQuery, fenceSupersedeGeneration, fenceFindHoldersQuery, "reopen", "reopen",
	}
	if !slices.Equal(database.statements, want) {
		t.Fatalf("statements = %q, want %q", database.statements, want)
	}
	if database.commits != 1 || database.rollbacks != 0 {
		t.Fatalf("commits = %d, rollbacks = %d, want 1 and 0", database.commits, database.rollbacks)
	}
	scope, generation := testFences[0].ScopeID, testFences[0].GenerationID
	if !slices.Equal(database.args[1], []any{scope, generation}) {
		t.Fatalf("detach args = %v, want the read scope and generation", database.args[1])
	}
	if !slices.Equal(database.args[2], []any{scope, generation, fenceTestNow}) {
		t.Fatalf("supersede args = %v, want the read scope and generation and the observed time", database.args[2])
	}
	if !slices.Equal(database.args[3], []any{"repository:r_00000001", scope}) {
		t.Fatalf("holder read args = %v, want the repository and the fenced scope", database.args[3])
	}
	wantReopened := []string{"holder-a<-" + scope + "/" + generation, "holder-b<-" + scope + "/" + generation}
	if !slices.Equal(database.reopened, wantReopened) {
		t.Fatalf("reopened = %q, want %q", database.reopened, wantReopened)
	}
	if database.openOnReopen {
		t.Fatal("a holder was reopened with the holder read still open on the transaction")
	}
}

// TestFenceThatLostTheRaceChangesNothing proves a pointer that already moved stops
// the fence: no generation or holder is touched, nothing commits, and nothing is
// counted. The fences after it still run.
func TestFenceThatLostTheRaceChangesNothing(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{detach: []int64{0, 1}, holders: []string{"holder-a"}}
	result, err := database.store().FenceStaleGoEvidence(t.Context(), testFences, fenceTestNow)
	if err != nil || result.Fenced != 1 || result.HoldersReopened != 1 {
		t.Fatalf("result = %+v, err = %v, want 1 fenced and 1 holder", result, err)
	}
	supersedes := 0
	for index, statement := range database.statements {
		if statement == fenceSupersedeGeneration {
			supersedes++
			if database.args[index][1] != "gen-2" {
				t.Fatalf("superseded %v, want only gen-2", database.args[index])
			}
		}
	}
	if supersedes != 1 || database.commits != 1 || database.rollbacks != 1 {
		t.Fatalf("supersedes = %d, commits = %d, rollbacks = %d, want 1, 1, 1", supersedes, database.commits, database.rollbacks)
	}
}

// TestFenceRefusesAPointerToAGenerationNotActive is the state error of A1.1: the
// pointer named the generation but it was not active. It rolls back and says so
// rather than leave a NULL pointer over a state nobody understands.
func TestFenceRefusesAPointerToAGenerationNotActive(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{supersede: []int64{0}, holders: []string{"holder-a"}}
	result, err := database.store().FenceStaleGoEvidence(t.Context(), testFences, fenceTestNow)
	if err == nil {
		t.Fatal("err = nil, want a state error")
	}
	if result.Fenced != 0 || database.commits != 0 || database.rollbacks != 1 || len(database.reopened) != 0 {
		t.Fatalf("result = %+v, commits = %d, rollbacks = %d, reopened = %q, want nothing changed",
			result, database.commits, database.rollbacks, database.reopened)
	}
}

// TestFenceRollsBackWithItsHolders proves a retraction and its reopen commit together:
// a failed reopen or holder read leaves the generation active, so the next pass retries
// the whole fence instead of leaving holders stale with no retry.
func TestFenceRollsBackWithItsHolders(t *testing.T) {
	t.Parallel()

	for name, database := range map[string]*fakeFenceDB{
		"reopen fails":      {holders: []string{"holder-a"}, reopenErr: errors.New("lock timeout")},
		"holder read fails": {holders: []string{"holder-a"}, failOn: fenceFindHoldersQuery},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := database.store().FenceStaleGoEvidence(t.Context(), testFences, fenceTestNow)
			if err == nil {
				t.Fatal("err = nil, want the failure")
			}
			if result.Fenced != 0 || result.HoldersReopened != 0 || database.commits != 0 || database.rollbacks != 1 {
				t.Fatalf("result = %+v, commits = %d, rollbacks = %d, want nothing committed",
					result, database.commits, database.rollbacks)
			}
		})
	}
}

// TestFenceStopsAtTheFirstError bounds one pass to one lock wait and keeps what
// already committed: the caller retries the page, and a retracted generation is no
// longer a candidate there.
func TestFenceStopsAtTheFirstError(t *testing.T) {
	t.Parallel()

	for _, failOn := range []string{fenceDetachPointerQuery, fenceSupersedeGeneration, fenceTimeoutsQuery} {
		database := &fakeFenceDB{failOn: failOn}
		result, err := database.store().FenceStaleGoEvidence(t.Context(), testFences, fenceTestNow)
		if err == nil {
			t.Fatalf("failOn %q: err = nil, want the statement error", failOn)
		}
		if result.Fenced != 0 || database.commits != 0 || database.rollbacks != 1 {
			t.Fatalf("failOn %q: result = %+v, commits = %d, rollbacks = %d, want 0, 0, 1",
				failOn, result, database.commits, database.rollbacks)
		}
	}
}

// TestFenceWithNothingToDoOpensNoTransaction keeps the common pass free of writes.
func TestFenceWithNothingToDoOpensNoTransaction(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{}
	result, err := database.store().FenceStaleGoEvidence(t.Context(), nil, fenceTestNow)
	if err != nil || result.Fenced != 0 || len(database.statements) != 0 {
		t.Fatalf("result = %+v, err = %v, statements = %q", result, err, database.statements)
	}
}

type queryOnlyDB struct{}

func (queryOnlyDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("unexpected query")
}

// TestFenceFailsClosedWithoutItsDependencies refuses a pass that could not finish the
// job. Without a transaction a pointer could go NULL with its generation active, and
// without a reopener holders would keep a stale verdict with no bound.
func TestFenceFailsClosedWithoutItsDependencies(t *testing.T) {
	t.Parallel()

	noop := func(context.Context, db.ExecQueryer, string, string, string, time.Time) error { return nil }
	for name, store := range map[string]Store{
		"no database":    {},
		"no transaction": NewStore(queryOnlyDB{}).WithHolderReopener(noop),
		"no reopener":    NewStore(&fakeFenceDB{}),
	} {
		if _, err := store.FenceStaleGoEvidence(t.Context(), testFences, fenceTestNow); err == nil {
			t.Errorf("%s: err = nil, want a refusal", name)
		}
	}
}

// TestFenceRefusesAScopeThatNamesNoRepository fails rather than reopen no holders:
// the rule only ever names canonical scopes, so this is a caller defect.
func TestFenceRefusesAScopeThatNamesNoRepository(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{}
	fence := []scannercomponent.StaleEvidenceFence{{ScopeID: "go-reachability://fleet", GenerationID: "gen-1"}}
	if _, err := database.store().FenceStaleGoEvidence(t.Context(), fence, fenceTestNow); err == nil {
		t.Fatal("err = nil, want a refusal")
	}
	if len(database.statements) != 0 {
		t.Fatalf("statements = %q, want none", database.statements)
	}
}
