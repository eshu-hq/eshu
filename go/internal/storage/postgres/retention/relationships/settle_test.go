// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// leave answers the settle read with the given ids that the batch claimed: the
// claimed generations that still have a row after the batch's deletes.
func leave(ids ...string) func(c call) (reply, bool) {
	return func(c call) (reply, bool) {
		if c.query != settleQuery {
			return reply{}, false
		}
		var left []string
		for _, id := range idsArg(c) {
			if slices.Contains(ids, id) {
				left = append(left, id)
			}
		}
		return reply{rows: idRows(left...)}, true
	}
}

// TestSettleLeavesAGenerationAnotherSessionHeldAsLockBusy pins the accounting of
// a batch that could not finish one of its claimed generations: the three
// deletes ran and the settle read names g2 as still having rows (a purge held
// them). The batch commits what it deleted, counts only the other generations as
// swept, and counts g2 as lock_busy so a later pass retries it.
func TestSettleLeavesAGenerationAnotherSessionHeldAsLockBusy(t *testing.T) {
	t.Parallel()
	s := script{evidence: []string{"g1", "g2", "g3"}, rows: [3]int64{6, 4, 2}, override: leave("g2")}
	result, database, err := sweepWith(t, s, Options{Deletes: true})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := idsArg(database.callsFor(settleQuery)[0]); !slices.Equal(got, []string{"g1", "g2", "g3"}) {
		t.Fatalf("settle received %v, want the claimed ids", got)
	}
	if result.GenerationsSwept != 2 || result.Batches != 1 || database.commits != 1 {
		t.Fatalf("swept %d in %d batches (%d commits), want 2 swept in one committed batch", result.GenerationsSwept, result.Batches, database.commits)
	}
	wantSkipped := map[string]int{SkipLockBusy: 1, SkipLockTimeout: 0, SkipIneligible: 0}
	if !reflect.DeepEqual(result.Skipped, wantSkipped) {
		t.Fatalf("skipped = %v, want %v", result.Skipped, wantSkipped)
	}
	wantRows := map[string]int64{EvidenceTable: 6, CandidatesTable: 4, GenerationsTable: 2}
	if !reflect.DeepEqual(result.RowsSwept, wantRows) {
		t.Fatalf("rows swept = %v, want %v", result.RowsSwept, wantRows)
	}
}

// TestBatchWhereAnotherSessionHoldsEverythingCommitsNothing pins the shape of the
// purge-in-flight batch: every claimed generation still has rows (the purge holds
// them) and the deletes took none, so there is nothing to commit. The
// transaction ends without a commit, the pass does not count a batch, and every
// generation is lock_busy.
func TestBatchWhereAnotherSessionHoldsEverythingCommitsNothing(t *testing.T) {
	t.Parallel()
	s := script{evidence: []string{"g1", "g2"}, rows: [3]int64{0, 0, 0}, override: leave("g1", "g2")}
	result, database, err := sweepWith(t, s, Options{Deletes: true})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if database.commits != 0 {
		t.Fatalf("commits = %d, want none for a batch that deleted nothing and finished nothing", database.commits)
	}
	if result.Batches != 0 || result.GenerationsSwept != 0 || result.Skipped[SkipLockBusy] != 2 {
		t.Fatalf("pass = %d batches, swept %d, skipped %v; want 2 lock_busy and no batch", result.Batches, result.GenerationsSwept, result.Skipped)
	}
}

// TestBatchCommitsTheRowsItDeletedEvenWhenNoGenerationFinished pins that rows a
// batch did delete are never dropped from the accounting: a purge held part of
// the only generation (its candidate rows), the batch deleted the rest of the
// evidence, and the transaction must commit so those deletes stand, with the row
// counts reaching the pass and the row cap while the generation stays lock_busy.
func TestBatchCommitsTheRowsItDeletedEvenWhenNoGenerationFinished(t *testing.T) {
	t.Parallel()
	s := script{evidence: ids("g", 4), rows: [3]int64{5, 0, 0}, override: leave(ids("g", 4)...)}
	result, database, err := sweepWith(t, s, Options{Deletes: true, RowCap: 5, BatchGenerations: 2})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if database.commits != 1 || result.Batches != 1 {
		t.Fatalf("commits = %d, batches = %d, want the one batch that deleted rows committed (the cap of 5 rows stopped the second)", database.commits, result.Batches)
	}
	if result.GenerationsSwept != 0 || result.RowsSwept[EvidenceTable] != 5 || result.Skipped[SkipLockBusy] != 2 {
		t.Fatalf("swept %d, rows %v, skipped %v; want no generation swept, 5 evidence rows counted and 2 lock_busy", result.GenerationsSwept, result.RowsSwept, result.Skipped)
	}
	if result.Capped != CapRows {
		t.Fatalf("Capped = %q, want %q: the rows a batch deleted count toward the cap", result.Capped, CapRows)
	}
}

// TestAnAlreadyEmptyGenerationCountsAsSwept pins what swept means for a census
// that went stale: another replica removed every row of g1 after the census
// listed it, so the batch claims it (no generation row), deletes nothing, finds
// nothing left, and counts it swept. The count is "left with nothing behind", not
// "rows this pass deleted"; the doc on Result.GenerationsSwept says so.
func TestAnAlreadyEmptyGenerationCountsAsSwept(t *testing.T) {
	t.Parallel()
	result, database, err := sweepWith(t, script{evidence: []string{"g1"}, rows: [3]int64{0, 0, 0}}, Options{Deletes: true})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.GenerationsSwept != 1 || result.Batches != 1 || database.commits != 1 {
		t.Fatalf("swept %d in %d batches (%d commits), want the empty generation counted swept", result.GenerationsSwept, result.Batches, database.commits)
	}
	if result.RowsSwept[EvidenceTable]+result.RowsSwept[CandidatesTable]+result.RowsSwept[GenerationsTable] != 0 {
		t.Fatalf("rows swept = %v, want none", result.RowsSwept)
	}
}

// TestFaultAfterTheSettleRollsEverythingBack pins that the settle read is part of
// the atomic batch: a failure after it (the fault hook stands in for a crash
// before the commit) rolls the transaction back and surfaces the error, and a
// 55P03 on the settle (a table lock) counts the whole batch as lock_timeout.
func TestFaultAfterTheSettleRollsEverythingBack(t *testing.T) {
	t.Parallel()
	fault := errors.New("injected crash after the settle read")
	database := &scriptedDB{handler: script{evidence: []string{"g1"}, rows: [3]int64{5, 2, 1}}.handle}
	opts := Options{Deletes: true}
	opts.afterStatement = func(stage string) error {
		if stage == stageSettle {
			return fault
		}
		return nil
	}
	result, err := Sweep(context.Background(), database, opts)
	if !errors.Is(err, fault) {
		t.Fatalf("Sweep error = %v, want the injected fault", err)
	}
	if database.commits != 0 || result.Batches != 0 || result.GenerationsSwept != 0 || result.RowsSwept[EvidenceTable] != 0 {
		t.Fatalf("a faulted batch was counted or committed: commits %d, %+v", database.commits, result)
	}

	timedOut := script{evidence: []string{"g1", "g2"}, override: func(c call) (reply, bool) {
		if c.query == settleQuery {
			return reply{err: &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}}, true
		}
		return reply{}, false
	}}
	result, _, err = sweepWith(t, timedOut, Options{Deletes: true})
	if err != nil {
		t.Fatalf("Sweep: %v (a lock timeout is a skip)", err)
	}
	if result.Skipped[SkipLockTimeout] != 2 || result.Batches != 0 || result.GenerationsSwept != 0 {
		t.Fatalf("pass = %d batches, swept %d, skipped %v; want both generations lock_timeout", result.Batches, result.GenerationsSwept, result.Skipped)
	}
}
