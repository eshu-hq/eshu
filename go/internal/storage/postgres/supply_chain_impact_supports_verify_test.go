// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// verifyFakeSlice is one scripted answer of the verification slice statement.
type verifyFakeSlice struct {
	missing                       int64
	orphanScope, orphanGeneration string
	last                          bool
	bound                         [3]string
}

func (s verifyFakeSlice) row() []any {
	return []any{
		int64(2), int64(2), s.missing, s.orphanScope != "", s.orphanScope, s.orphanGeneration,
		s.last, s.bound[0], s.bound[1], s.bound[2],
	}
}

// verifyFakeDB answers the statements of a backfill whose sweep already passed
// the last pair: the marker row (with its verification cursor), an empty fence,
// an empty chunk and no next pair, then the scripted verification slices. It
// applies the cursor advance, the restart and the completion to its marker so
// a test can drive several steps.
type verifyFakeDB struct {
	t *testing.T

	sweeps           int64
	verifyCursor     [3]string
	verifySweep      int64
	verifyCursorSet  bool
	complete         bool
	fenceDirtyAtDone bool
	// bumpBeforeComplete models another runner restarting the sweep between
	// this step's marker read and its completion.
	bumpBeforeComplete bool
	// sliceErr fails the next slice statement (a statement timeout).
	sliceErr error
	// staged holds the completion transaction's writes until Commit.
	stagedClear, stagedComplete bool

	slices    []verifyFakeSlice
	sliceArgs [][]any
	execs     []verifyFakeExec
}

type verifyFakeExec struct {
	query string
	args  []any
}

func (f *verifyFakeDB) Begin(context.Context) (db.Transaction, error) { return verifyFakeTx{f}, nil }

func (f *verifyFakeDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, verifyFakeExec{query: query, args: args})
	switch query {
	case supplyChainImpactSupportsBackfillEnsureMarkerSQL:
		return verifyFakeResult(0), nil
	case supplyChainImpactSupportsBackfillVerifyAdvanceSQL:
		if args[4] != f.sweeps {
			return verifyFakeResult(0), nil
		}
		f.verifyCursor = [3]string{args[1].(string), args[2].(string), args[3].(string)}
		f.verifySweep, f.verifyCursorSet = f.sweeps, true
		return verifyFakeResult(1), nil
	case supplyChainImpactSupportsBackfillRestartSQL:
		f.sweeps++
		f.verifyCursorSet = false
		return verifyFakeResult(1), nil
	case supplyChainImpactSupportsBackfillVerifyClearSQL:
		if f.bumpBeforeComplete {
			f.sweeps++
			f.bumpBeforeComplete = false
		}
		if f.complete || args[1] != f.sweeps {
			return verifyFakeResult(0), nil
		}
		f.stagedClear = true
		return verifyFakeResult(1), nil
	case supplyChainImpactSupportsBackfillCompleteSQL:
		if f.fenceDirtyAtDone {
			return verifyFakeResult(0), nil
		}
		f.stagedComplete = true
		return verifyFakeResult(1), nil
	case supplychaincore.SupplyChainImpactConflictDomainLockQuery, deleteRetractedSupplyChainImpactSupportsSQL:
		return verifyFakeResult(1), nil
	}
	f.t.Fatalf("unexpected exec: %s", query)
	return nil, nil
}

func (f *verifyFakeDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	switch query {
	case supplyChainImpactSupportsBackfillReadMarkerSQL:
		cursor := f.verifyCursor
		valid := f.verifyCursorSet && f.verifySweep == f.sweeps
		return &fakeRows{rows: [][]any{{
			"scope-z", "gen-z", "fact-z", f.complete, f.sweeps,
			cursor[0], cursor[1], cursor[2], valid,
		}}}, nil
	case supplyChainImpactSupportsDirtyScopeSQL, supplyChainImpactSupportsBackfillNextPairSQL,
		supplyChainImpactSupportsBackfillChunkSQL:
		return &fakeRows{}, nil
	case supplyChainImpactSupportsDirtyCountSQL:
		return &fakeRows{rows: [][]any{{int64(0)}}}, nil
	case supplyChainImpactSupportsBackfillVerifySliceSQL:
		f.sliceArgs = append(f.sliceArgs, args)
		if f.sliceErr != nil {
			return nil, f.sliceErr
		}
		if len(f.slices) == 0 {
			f.t.Fatal("verification slice run with no scripted answer")
		}
		next := f.slices[0]
		f.slices = f.slices[1:]
		return &fakeRows{rows: [][]any{next.row()}}, nil
	}
	f.t.Fatalf("unexpected query: %s", query)
	return nil, nil
}

// execsOf returns the recorded execs of one statement.
func (f *verifyFakeDB) execsOf(query string) []verifyFakeExec {
	var out []verifyFakeExec
	for _, exec := range f.execs {
		if exec.query == query {
			out = append(out, exec)
		}
	}
	return out
}

type verifyFakeTx struct{ f *verifyFakeDB }

func (tx verifyFakeTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.f.ExecContext(ctx, query, args...)
}

func (tx verifyFakeTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return tx.f.QueryContext(ctx, query, args...)
}

func (tx verifyFakeTx) Commit() error {
	if tx.f.stagedClear {
		tx.f.verifyCursor, tx.f.verifyCursorSet = [3]string{}, false
	}
	if tx.f.stagedComplete {
		tx.f.complete = true
	}
	tx.f.stagedClear, tx.f.stagedComplete = false, false
	return nil
}

func (tx verifyFakeTx) Rollback() error {
	tx.f.stagedClear, tx.f.stagedComplete = false, false
	return nil
}

type verifyFakeResult int64

func (r verifyFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r verifyFakeResult) RowsAffected() (int64, error) { return int64(r), nil }

func verifyStep(t *testing.T, f *verifyFakeDB) SupplyChainImpactSupportsBackfillReport {
	t.Helper()
	report, err := SupplyChainImpactSupportsBackfill{DB: f, VerifySliceSize: 2}.RunStep(context.Background())
	if err != nil {
		t.Fatalf("RunStep: %v", err)
	}
	return report
}

func wantSliceCursor(t *testing.T, f *verifyFakeDB, slice int, want [3]string) {
	t.Helper()
	if len(f.sliceArgs) <= slice {
		t.Fatalf("slice %d did not run (%d slices ran)", slice, len(f.sliceArgs))
	}
	got := f.sliceArgs[slice]
	if fmt.Sprint(got) != fmt.Sprint([]any{want[0], want[1], want[2], 2}) {
		t.Fatalf("slice %d args = %v, want cursor %v and slice size 2", slice, got, want)
	}
}

// TestSupportsVerifyCatchesAnOrphanInALateSlice walks two clean slices, then
// plants an orphan in the third: the step removes that pair's retracted supports
// under the pair's conflict-domain lock, keeps the cursor, and the marker stays
// unset until the same slice reruns clean and is the last one.
func TestSupportsVerifyCatchesAnOrphanInALateSlice(t *testing.T) {
	t.Parallel()
	f := &verifyFakeDB{t: t, sweeps: 3, slices: []verifyFakeSlice{
		{bound: [3]string{"a", "g1", "f2"}},
		{bound: [3]string{"b", "g1", "f4"}},
		{orphanScope: "c", orphanGeneration: "g9", bound: [3]string{"c", "g9", "f6"}},
		{last: true},
	}}
	for step, want := range []string{
		telemetry.SupplyChainImpactSupportsVerifySliceAdvance,
		telemetry.SupplyChainImpactSupportsVerifySliceAdvance,
		telemetry.SupplyChainImpactSupportsVerifySliceOrphan,
	} {
		report := verifyStep(t, f)
		if report.VerifySlice != want || report.Complete {
			t.Fatalf("step %d = %+v, want slice outcome %q and no completion", step, report, want)
		}
	}
	wantSliceCursor(t, f, 0, [3]string{"", "", ""})
	wantSliceCursor(t, f, 1, [3]string{"a", "g1", "f2"})
	wantSliceCursor(t, f, 2, [3]string{"b", "g1", "f4"})
	deletes := f.execsOf(deleteRetractedSupplyChainImpactSupportsSQL)
	if len(deletes) != 1 || fmt.Sprint(deletes[0].args) != "[c g9]" {
		t.Fatalf("retracted-support deletes = %+v, want one for the orphan pair (c, g9)", deletes)
	}
	locks := f.execsOf(supplychaincore.SupplyChainImpactConflictDomainLockQuery)
	if len(locks) == 0 || locks[len(locks)-1].args[0] != supplychaincore.SupplyChainImpactConflictDomainKey("c", "g9") {
		t.Fatalf("locks = %+v, want the orphan pair's conflict-domain lock last", locks)
	}
	if f.complete {
		t.Fatal("marker set over a slice that held an orphan")
	}
	// The orphan's slice reruns from the same cursor and, clean and last, completes.
	report := verifyStep(t, f)
	wantSliceCursor(t, f, 3, [3]string{"b", "g1", "f4"})
	if !report.Complete || !report.Verified || report.VerifySlice != telemetry.SupplyChainImpactSupportsVerifySliceLast {
		t.Fatalf("last step = %+v, want the last slice to complete the marker", report)
	}
}

// TestSupportsVerifyRestartsOnAMissingSupportInALateSlice proves a slice that
// finds a live finding without a support restarts the sweep, exactly as the
// unsliced verification did, and never sets the marker; the restart also drops
// the verification cursor.
func TestSupportsVerifyRestartsOnAMissingSupportInALateSlice(t *testing.T) {
	t.Parallel()
	f := &verifyFakeDB{t: t, sweeps: 0, slices: []verifyFakeSlice{
		{bound: [3]string{"a", "g1", "f2"}},
		{bound: [3]string{"b", "g1", "f4"}},
		{missing: 1, bound: [3]string{"c", "g1", "f6"}},
	}}
	verifyStep(t, f)
	verifyStep(t, f)
	report := verifyStep(t, f)
	if !report.Verified || report.Missing != 1 || report.Complete ||
		report.VerifySlice != telemetry.SupplyChainImpactSupportsVerifySliceGap {
		t.Fatalf("gap step = %+v, want verified with a miss, a gap outcome and no completion", report)
	}
	if got := len(f.execsOf(supplyChainImpactSupportsBackfillRestartSQL)); got != 1 {
		t.Fatalf("restarts = %d, want 1", got)
	}
	if len(f.execsOf(supplyChainImpactSupportsBackfillCompleteSQL)) != 0 || f.complete {
		t.Fatal("the completion statement ran over a gap")
	}
	if f.verifyCursorSet && f.verifySweep == f.sweeps {
		t.Fatal("the verification cursor survived the restart")
	}
}

// TestSupportsVerifyResumesFromTheStoredCursor proves a restarted process
// resumes the verification at the cursor stored in the marker row, and starts
// over when the cursor belongs to an earlier sweep (an older binary restarted
// the sweep without clearing it).
func TestSupportsVerifyResumesFromTheStoredCursor(t *testing.T) {
	t.Parallel()
	resumed := &verifyFakeDB{
		t: t, sweeps: 2, verifySweep: 2, verifyCursorSet: true,
		verifyCursor: [3]string{"m", "g5", "f9"}, slices: []verifyFakeSlice{{bound: [3]string{"n", "g1", "f1"}}},
	}
	verifyStep(t, resumed)
	wantSliceCursor(t, resumed, 0, [3]string{"m", "g5", "f9"})
	advances := resumed.execsOf(supplyChainImpactSupportsBackfillVerifyAdvanceSQL)
	if len(advances) != 1 || fmt.Sprint(advances[0].args) != fmt.Sprint([]any{SupplyChainImpactSupportsBackfillMarker, "n", "g1", "f1", int64(2)}) {
		t.Fatalf("advance = %+v, want the slice bound guarded by the sweep it read", advances)
	}

	stale := &verifyFakeDB{
		t: t, sweeps: 3, verifySweep: 2, verifyCursorSet: true,
		verifyCursor: [3]string{"m", "g5", "f9"}, slices: []verifyFakeSlice{{bound: [3]string{"a", "g1", "f1"}}},
	}
	verifyStep(t, stale)
	wantSliceCursor(t, stale, 0, [3]string{"", "", ""})
}

// TestSupportsVerifyLastSliceKeepsTheFenceRecheck proves the last clean slice
// runs the completion statement with its fence recheck, and a dirty fence at
// that moment leaves the marker unset.
func TestSupportsVerifyLastSliceKeepsTheFenceRecheck(t *testing.T) {
	t.Parallel()
	f := &verifyFakeDB{t: t, fenceDirtyAtDone: true, slices: []verifyFakeSlice{{last: true}}}
	report := verifyStep(t, f)
	if report.Complete || !report.Verified || report.VerifySlice != telemetry.SupplyChainImpactSupportsVerifySliceLast {
		t.Fatalf("step = %+v, want the last slice verified but the marker unset under a dirty fence", report)
	}
	if got := len(f.execsOf(supplyChainImpactSupportsBackfillCompleteSQL)); got != 1 {
		t.Fatalf("completion statements = %d, want 1", got)
	}
}

// TestSupportsVerifyCompletionDropsTheCursor proves completion clears the
// verification cursor in the same transaction: a marker whose completed_at is
// reset without a sweep bump (as the fence and fleet-gate live tests do) starts
// its next verification at the start of the key space, never at the old cursor.
func TestSupportsVerifyCompletionDropsTheCursor(t *testing.T) {
	t.Parallel()
	f := &verifyFakeDB{
		t: t, sweeps: 4, verifySweep: 4, verifyCursorSet: true,
		verifyCursor: [3]string{"m", "g5", "f9"}, slices: []verifyFakeSlice{{last: true}, {bound: [3]string{"a", "g1", "f1"}}},
	}
	if report := verifyStep(t, f); !report.Complete {
		t.Fatalf("last slice = %+v, want the marker set", report)
	}
	if f.verifyCursorSet {
		t.Fatalf("cursor %v survived completion", f.verifyCursor)
	}
	clears := f.execsOf(supplyChainImpactSupportsBackfillVerifyClearSQL)
	if len(clears) != 1 || fmt.Sprint(clears[0].args) != fmt.Sprint([]any{SupplyChainImpactSupportsBackfillMarker, int64(4)}) {
		t.Fatalf("cursor clears = %+v, want one guarded by the sweep read", clears)
	}
	f.complete = false // completed_at reset with no sweep bump
	verifyStep(t, f)
	wantSliceCursor(t, f, 1, [3]string{"", "", ""})
}

// TestSupportsVerifyCompletionIsSweepGuarded proves the last slice cannot set
// the marker when the sweep changed after the step read the marker (a second
// runner restarted it), and that a fence-blocked completion keeps the cursor.
func TestSupportsVerifyCompletionIsSweepGuarded(t *testing.T) {
	t.Parallel()
	raced := &verifyFakeDB{t: t, sweeps: 1, bumpBeforeComplete: true, slices: []verifyFakeSlice{{last: true}}}
	if report := verifyStep(t, raced); report.Complete || raced.complete {
		t.Fatalf("last slice after a concurrent restart = %+v, want no marker", report)
	}
	if got := len(raced.execsOf(supplyChainImpactSupportsBackfillCompleteSQL)); got != 0 {
		t.Fatalf("completion statements after a lost sweep guard = %d, want 0", got)
	}

	fenced := &verifyFakeDB{
		t: t, sweeps: 2, verifySweep: 2, verifyCursorSet: true, fenceDirtyAtDone: true,
		verifyCursor: [3]string{"m", "g5", "f9"}, slices: []verifyFakeSlice{{last: true}},
	}
	if report := verifyStep(t, fenced); report.Complete {
		t.Fatalf("fence-blocked last slice = %+v, want no marker", report)
	}
	if !fenced.verifyCursorSet || fenced.verifyCursor != [3]string{"m", "g5", "f9"} {
		t.Fatalf("fence-blocked completion dropped the cursor (%v, set %v)", fenced.verifyCursor, fenced.verifyCursorSet)
	}
}

// TestSupportsVerifyReportsAFailedSlice proves a slice statement that fails (a
// statement timeout) returns the error with the slice reported as failed and its
// duration measured, so the slice histogram sees the slow tail.
func TestSupportsVerifyReportsAFailedSlice(t *testing.T) {
	t.Parallel()
	timeout := errors.New("canceling statement due to statement timeout")
	f := &verifyFakeDB{t: t, sliceErr: timeout}
	report, err := SupplyChainImpactSupportsBackfill{DB: f, VerifySliceSize: 2}.RunStep(context.Background())
	if !errors.Is(err, timeout) {
		t.Fatalf("error = %v, want the slice statement's error", err)
	}
	if report.VerifySlice != telemetry.SupplyChainImpactSupportsVerifySliceFailed || report.VerifySliceDuration <= 0 || report.Complete {
		t.Fatalf("failed slice report = %+v, want outcome failed with a duration and no marker", report)
	}
}
