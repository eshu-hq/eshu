// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestFenceHolderStatementIsTheOnlyOneCarryingReadinessAfterTheDetach pins the
// one-snapshot shape (#11197): the stand-alone readiness check runs once, before the
// detach; after the detach the holder statement is the only statement that carries
// the predicate, so there is no second check between the holder read and the
// reopens for a drain to slip through.
func TestFenceHolderStatementIsTheOnlyOneCarryingReadinessAfterTheDetach(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{holders: []string{"holder-a", "holder-b"}}
	result, err := database.store().FenceStaleGoEvidence(t.Context(), testFences[:1], fenceTestNow)
	if err != nil || result.Fenced != 1 || result.HoldersReopened != 2 {
		t.Fatalf("result = %+v, err = %v, want 1 fenced and 2 holders", result, err)
	}
	detach := slices.Index(database.statements, fenceDetachPointerQuery)
	if detach < 0 {
		t.Fatalf("statements = %q, want the detach", database.statements)
	}
	for i, statement := range database.statements {
		switch {
		case statement == fenceTestReadySQL && i > detach:
			t.Fatalf("statement %d is a stand-alone readiness check after the detach: %q", i, database.statements)
		case strings.Contains(statement, fenceTestReadySQL) && statement != fenceTestReadySQL && statement != fenceTestHolderStatement:
			t.Fatalf("statement %d carries the readiness text outside the holder statement: %q", i, statement)
		}
	}
	if got, want := slices.Index(database.statements, fenceTestHolderStatement), detach+2; got != want {
		t.Fatalf("holder statement at %d, want %d (detach, supersede, then the holder statement): %q", got, want, database.statements)
	}
	if !slices.Equal(database.observed, []bool{true, true}) {
		t.Fatalf("observed = %v, want one verdict from the check and one from the holder statement", database.observed)
	}
}

// TestHolderStatementEmbedsTheReadinessTextOnce pins the composer: the injected text
// appears exactly once, in parentheses, as the first FROM item, and the holder
// parameters follow the predicate's two.
func TestHolderStatementEmbedsTheReadinessTextOnce(t *testing.T) {
	t.Parallel()

	const readiness = "SELECT EXISTS (SELECT 1) /* $1 $2 */"
	statement := HolderStatement(readiness)
	if got := strings.Count(statement, "("+readiness+")"); got != 1 {
		t.Fatalf("readiness embedded %d times in parentheses, want 1: %s", got, statement)
	}
	if !strings.HasPrefix(statement, "SELECT ready.ok, holder.scope_id\n\t\tFROM ("+readiness+") AS ready(ok)") {
		t.Fatalf("statement does not start with the readiness FROM item: %s", statement)
	}
	for _, parameter := range []string{"$3", "$4"} {
		if !strings.Contains(statement, parameter) {
			t.Fatalf("statement lacks holder parameter %s: %s", parameter, statement)
		}
	}
	if !strings.Contains(statement, `ORDER BY holder.lock_key COLLATE "C" NULLS LAST, holder.scope_id COLLATE "C"`) {
		t.Fatalf("statement lost the #11147 lock order: %s", statement)
	}
}

// TestFenceRollsBackWhenTheHolderStatementIsNotReadyEvenWithHolders pins the verdict
// read: ok = false in the holder snapshot refuses the fence although holder rows came
// back, with no reopen and no commit, and Observe sees the verdict once for the
// statement.
func TestFenceRollsBackWhenTheHolderStatementIsNotReadyEvenWithHolders(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{holders: []string{"holder-a"}, holdersNotReady: true}
	result, err := database.store().FenceStaleGoEvidence(t.Context(), testFences, fenceTestNow)
	if !errors.Is(err, errTestNotReady) {
		t.Fatalf("err = %v, want the not-ready refusal", err)
	}
	if result.Fenced != 0 || result.HoldersReopened != 0 || database.commits != 0 || database.rollbacks != 1 || len(database.reopened) != 0 {
		t.Fatalf("result = %+v, commits = %d, rollbacks = %d, reopened = %q, want nothing committed or reopened",
			result, database.commits, database.rollbacks, database.reopened)
	}
	if !slices.Equal(database.observed, []bool{true, false}) {
		t.Fatalf("observed = %v, want check 1 ready, then the holder statement not ready", database.observed)
	}
}

// TestFenceTreatsAHolderStatementWithNoRowAsAnError pins the shape guard: the
// predicate always yields one row, so no row means the statement is not the one the
// fence expects, and the fence rolls back with the probe failure, not the typed
// not-ready refusal.
func TestFenceTreatsAHolderStatementWithNoRowAsAnError(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{noHolderRow: true}
	_, err := database.store().FenceStaleGoEvidence(t.Context(), testFences, fenceTestNow)
	if err == nil || errors.Is(err, errTestNotReady) {
		t.Fatalf("err = %v, want a statement error that is not the not-ready refusal", err)
	}
	if database.commits != 0 || database.rollbacks != 1 || len(database.reopened) != 0 {
		t.Fatalf("commits = %d, rollbacks = %d, reopened = %q, want a rollback and no reopen",
			database.commits, database.rollbacks, database.reopened)
	}
}

// TestFenceCommitsWithZeroHoldersWhenReady pins the (true, NULL) row: a ready table
// with no holder still commits the retraction, and the NULL scope id never reaches
// the reopener.
func TestFenceCommitsWithZeroHoldersWhenReady(t *testing.T) {
	t.Parallel()

	database := &fakeFenceDB{}
	result, err := database.store().FenceStaleGoEvidence(t.Context(), testFences[:1], fenceTestNow)
	if err != nil || result.Fenced != 1 || result.HoldersReopened != 0 {
		t.Fatalf("result = %+v, err = %v, want 1 fenced and 0 holders", result, err)
	}
	if database.commits != 1 || database.rollbacks != 0 || len(database.reopened) != 0 {
		t.Fatalf("commits = %d, rollbacks = %d, reopened = %q, want one commit and no reopen",
			database.commits, database.rollbacks, database.reopened)
	}
}

// TestSupportsReadinessFailsClosedWhenUnbound pins the binding guard: a fence with no
// predicate text or no observer would run a holder read it cannot classify.
func TestSupportsReadinessFailsClosedWhenUnbound(t *testing.T) {
	t.Parallel()

	observe := func(context.Context, bool, error) error { return nil }
	for name, binding := range map[string]SupportsReadiness{
		"empty":       {},
		"no sql":      {Observe: observe},
		"no observer": {SQL: fenceTestReadySQL, Args: []any{"marker", 1}},
		"no args":     {SQL: fenceTestReadySQL, Observe: observe},
		"one arg":     {SQL: fenceTestReadySQL, Args: []any{"marker"}, Observe: observe},
		"three args":  {SQL: fenceTestReadySQL, Args: []any{"marker", 1, 2}, Observe: observe},
	} {
		database := &fakeFenceDB{}
		store := NewStore(database).WithHolderReopener(database.reopen).WithSupportsReadiness(binding)
		if _, err := store.FenceStaleGoEvidence(t.Context(), testFences, fenceTestNow); err == nil {
			t.Errorf("%s: err = nil, want a refusal", name)
		}
		if len(database.statements) != 0 {
			t.Errorf("%s: statements = %q, want none", name, database.statements)
		}
	}
}
