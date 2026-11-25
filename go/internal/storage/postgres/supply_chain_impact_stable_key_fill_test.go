// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// fillSupport is one fake supports row; key is nil while the support has none.
type fillSupport struct {
	scope, generation, fact string
	key                     *string
}

// fillFakeDB is an in-memory supports table, fact twin keys and fill marker. It
// applies each statement the fill issues, so a test can drive several steps and read
// the outcome, and it records the order of statements and the transaction each ran
// in.
type fillFakeDB struct {
	t        *testing.T
	supports []fillSupport
	twins    map[string]string

	markerSeen                  bool
	markerContractVersion       int
	cursor                      [3]string
	sweeps                      int64
	complete                    bool
	verifyCursor                [3]string
	verifySet                   bool
	verifySweep                 int64
	sliceSize                   int
	txSeq, curTx                int
	log                         []fillLogEntry
	markerNames                 map[string]bool
	stagedClear, stagedComplete bool
	commitErr                   error
}

type fillLogEntry struct {
	tx    int
	query string
	args  []any
}

func str(s string) *string { return &s }

func (f *fillFakeDB) Begin(context.Context) (db.Transaction, error) {
	f.txSeq++
	return &fillFakeTx{f: f, id: f.txSeq}, nil
}

type fillFakeTx struct {
	f  *fillFakeDB
	id int
}

func (tx *fillFakeTx) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	tx.f.curTx = tx.id
	defer func() { tx.f.curTx = 0 }()
	return tx.f.ExecContext(ctx, q, a...)
}

func (tx *fillFakeTx) QueryContext(ctx context.Context, q string, a ...any) (db.Rows, error) {
	tx.f.curTx = tx.id
	defer func() { tx.f.curTx = 0 }()
	return tx.f.QueryContext(ctx, q, a...)
}

func (tx *fillFakeTx) Commit() error {
	if tx.f.commitErr != nil {
		return tx.f.commitErr
	}
	if tx.f.stagedClear {
		tx.f.verifyCursor, tx.f.verifySet = [3]string{}, false
	}
	if tx.f.stagedComplete {
		tx.f.complete = true
	}
	tx.f.stagedClear, tx.f.stagedComplete = false, false
	return nil
}

func (tx *fillFakeTx) Rollback() error {
	tx.f.stagedClear, tx.f.stagedComplete = false, false
	return nil
}

func (f *fillFakeDB) sorted() []fillSupport {
	out := append([]fillSupport(nil), f.supports...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.scope != b.scope {
			return a.scope < b.scope
		}
		if a.generation != b.generation {
			return a.generation < b.generation
		}
		return a.fact < b.fact
	})
	return out
}

func (f *fillFakeDB) noteMarker(name any) {
	if f.markerNames == nil {
		f.markerNames = map[string]bool{}
	}
	f.markerNames[fmt.Sprint(name)] = true
}

func (f *fillFakeDB) ExecContext(_ context.Context, q string, a ...any) (sql.Result, error) {
	f.log = append(f.log, fillLogEntry{tx: f.curTx, query: q, args: a})
	switch q {
	case supplyChainImpactSupportsBackfillEnsureMarkerSQL:
		f.noteMarker(a[0])
		f.markerSeen, f.markerContractVersion = true, a[1].(int)
		return verifyFakeResult(0), nil
	case supplychaincore.SupplyChainImpactConflictDomainLockQuery:
		return verifyFakeResult(1), nil
	case supplyChainImpactStableKeyFillUpdateSQL:
		ids := map[string]bool{}
		for _, id := range fillIDs(a[0]) {
			ids[id] = true
		}
		var n int64
		for i := range f.supports {
			s := &f.supports[i]
			if !ids[s.fact] || s.key != nil {
				continue
			}
			if twin, ok := f.twins[s.fact]; ok && twin != "" {
				s.key = str(twin)
				n++
			}
		}
		return verifyFakeResult(n), nil
	case supplyChainImpactSupportsBackfillAdvanceSQL:
		f.noteMarker(a[0])
		f.cursor = [3]string{a[1].(string), a[2].(string), a[3].(string)}
		return verifyFakeResult(1), nil
	case supplyChainImpactSupportsBackfillRestartSQL:
		f.noteMarker(a[0])
		f.cursor, f.sweeps = [3]string{}, f.sweeps+1
		f.verifyCursor, f.verifySet = [3]string{}, false
		return verifyFakeResult(1), nil
	case supplyChainImpactSupportsBackfillVerifyAdvanceSQL:
		f.noteMarker(a[0])
		f.verifyCursor = [3]string{a[1].(string), a[2].(string), a[3].(string)}
		f.verifySet, f.verifySweep = true, f.sweeps
		return verifyFakeResult(1), nil
	case supplyChainImpactSupportsBackfillVerifyClearSQL:
		f.noteMarker(a[0])
		f.stagedClear = true
		return verifyFakeResult(1), nil
	case supplyChainImpactStableKeyFillCompleteSQL:
		f.noteMarker(a[0])
		f.stagedComplete = true
		return verifyFakeResult(1), nil
	}
	f.t.Fatalf("unexpected exec: %s", q)
	return nil, nil
}

func fillIDs(arg any) []string {
	ids, ok := arg.(*array.StringArray)
	if !ok {
		panic(fmt.Sprintf("fill update bound %T, want *array.StringArray", arg))
	}
	return []string(*ids)
}

func (f *fillFakeDB) QueryContext(_ context.Context, q string, a ...any) (db.Rows, error) {
	f.log = append(f.log, fillLogEntry{tx: f.curTx, query: q, args: a})
	switch q {
	case supplyChainImpactSupportsBackfillReadMarkerSQL:
		f.noteMarker(a[0])
		valid := f.verifySet && f.verifySweep == f.sweeps
		return &fakeRows{rows: [][]any{{
			f.cursor[0], f.cursor[1], f.cursor[2], f.complete, f.sweeps,
			f.verifyCursor[0], f.verifyCursor[1], f.verifyCursor[2], valid,
		}}}, nil
	case supplyChainImpactStableKeyFillNextPairSQL:
		for _, s := range f.sorted() {
			if s.scope > a[0].(string) || (s.scope == a[0].(string) && s.generation > a[1].(string)) {
				return &fakeRows{rows: [][]any{{s.scope, s.generation}}}, nil
			}
		}
		return &fakeRows{}, nil
	case supplyChainImpactStableKeyFillChunkSQL:
		limit := a[3].(int)
		var rows [][]any
		for _, s := range f.sorted() {
			if s.scope == a[0] && s.generation == a[1] && s.key == nil && s.fact > a[2].(string) {
				rows = append(rows, []any{s.fact})
				if len(rows) == limit {
					break
				}
			}
		}
		return &fakeRows{rows: rows}, nil
	case supplyChainImpactStableKeyFillVerifySliceSQL:
		size := a[3].(int)
		f.sliceSize = size
		after := [3]string{a[0].(string), a[1].(string), a[2].(string)}
		var slice []fillSupport
		for _, s := range f.sorted() {
			k := [3]string{s.scope, s.generation, s.fact}
			if lessKey(after, k) {
				slice = append(slice, s)
				if len(slice) == size {
					break
				}
			}
		}
		var missing int64
		for _, s := range slice {
			if twin, ok := f.twins[s.fact]; s.key == nil && ok && twin != "" {
				missing++
			}
		}
		last := ""
		var bound [3]string
		if n := len(slice); n > 0 {
			bound = [3]string{slice[n-1].scope, slice[n-1].generation, slice[n-1].fact}
		}
		isLast := len(slice) < size
		_ = last
		return &fakeRows{rows: [][]any{{int64(len(slice)), missing, isLast, bound[0], bound[1], bound[2]}}}, nil
	}
	f.t.Fatalf("unexpected query: %s", q)
	return nil, nil
}

func lessKey(a, b [3]string) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func newFillDB(t *testing.T, supports []fillSupport, twins map[string]string) *fillFakeDB {
	return &fillFakeDB{t: t, supports: supports, twins: twins}
}

func runFillToCompletion(t *testing.T, f *fillFakeDB, chunk int) (steps int, filled int64) {
	t.Helper()
	fill := SupplyChainImpactStableKeyFill{DB: f, ChunkSize: chunk, VerifySliceSize: 3}
	for ; steps < 200; steps++ {
		report, err := fill.RunStep(context.Background())
		if err != nil {
			t.Fatalf("RunStep: %v", err)
		}
		filled += report.Filled
		if report.Complete {
			return steps + 1, filled
		}
	}
	t.Fatal("the fill did not complete in 200 steps")
	return 0, 0
}

func fillKeys(f *fillFakeDB) map[string]string {
	out := map[string]string{}
	for _, s := range f.supports {
		if s.key == nil {
			out[s.fact] = "<nil>"
		} else {
			out[s.fact] = *s.key
		}
	}
	return out
}

// TestStableKeyFillFillsOnlyKeylessSupportsFromTheirTwin proves the fill gives a
// keyless support its twin's key, never overwrites a key a rewrite already filled,
// and leaves a support whose twin has no key (or no twin) keyless without blocking
// completion.
func TestStableKeyFillFillsOnlyKeylessSupportsFromTheirTwin(t *testing.T) {
	t.Parallel()

	f := newFillDB(t, []fillSupport{
		{"scope-a", "gen-1", "f1", nil},
		{"scope-a", "gen-1", "f2", str("kept")},
		{"scope-a", "gen-1", "f3", nil},
		{"scope-a", "gen-1", "f4", nil},
		{"scope-b", "gen-1", "f5", nil},
	}, map[string]string{"f1": "k1", "f2": "twin-of-f2", "f3": "", "f5": "k5"})
	runFillToCompletion(t, f, 2)

	want := map[string]string{"f1": "k1", "f2": "kept", "f3": "<nil>", "f4": "<nil>", "f5": "k5"}
	got := fillKeys(f)
	for fact, key := range want {
		if got[fact] != key {
			t.Errorf("support %s key = %q, want %q", fact, got[fact], key)
		}
	}
	if !f.complete {
		t.Fatal("the marker was not set after a clean verification")
	}
}

// TestStableKeyFillRunsEachChunkUnderThePairLockAndAdvancesTheCursorInIt proves
// the chunk shape of the ruling: at most the chunk size per transaction, the pair's
// conflict-domain lock first, the update, then the cursor advance, all in one
// transaction, and the ceiling of 500 holds whatever size is asked for.
func TestStableKeyFillRunsEachChunkUnderThePairLockAndAdvancesTheCursorInIt(t *testing.T) {
	t.Parallel()

	var supports []fillSupport
	twins := map[string]string{}
	for i := 0; i < 1200; i++ {
		id := fmt.Sprintf("f%05d", i)
		supports = append(supports, fillSupport{"scope-a", "gen-1", id, nil})
		twins[id] = "k-" + id
	}
	f := newFillDB(t, supports, twins)
	fill := SupplyChainImpactStableKeyFill{DB: f, ChunkSize: 100000, VerifySliceSize: 1000}
	for i := 0; i < 3 && !f.complete; i++ {
		if _, err := fill.RunStep(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var updates []fillLogEntry
	for _, e := range f.log {
		if e.query == supplyChainImpactStableKeyFillUpdateSQL {
			updates = append(updates, e)
			if n := len(fillIDs(e.args[0])); n > 500 {
				t.Fatalf("a fill chunk updated %d supports, want at most 500", n)
			}
		}
	}
	if len(updates) != 3 {
		t.Fatalf("1,200 supports filled in %d chunks, want 3 (500, 500, 200)", len(updates))
	}
	for _, update := range updates {
		var sawLock, sawAdvance bool
		for _, e := range f.log {
			if e.tx != update.tx || e.tx == 0 {
				continue
			}
			switch e.query {
			case supplychaincore.SupplyChainImpactConflictDomainLockQuery:
				if sawAdvance || sawLock {
					t.Fatal("the pair lock was taken twice or after the advance")
				}
				if want := supplychaincore.SupplyChainImpactConflictDomainKey("scope-a", "gen-1"); fmt.Sprint(e.args) != fmt.Sprint([]any{want}) {
					t.Fatalf("lock key = %v, want the pair's conflict-domain key %v", e.args, want)
				}
				sawLock = true
			case supplyChainImpactStableKeyFillUpdateSQL:
				if !sawLock {
					t.Fatal("the update ran before the pair lock")
				}
			case supplyChainImpactSupportsBackfillAdvanceSQL:
				sawAdvance = true
			}
		}
		if !sawLock || !sawAdvance {
			t.Fatalf("a chunk transaction lacked the lock (%v) or the cursor advance (%v)", sawLock, sawAdvance)
		}
	}
	if f.cursor[2] == "" {
		t.Fatal("the cursor never advanced")
	}
}

// TestStableKeyFillResumesFromTheStoredCursor proves a restart continues after the
// cursor rather than from the start: fact f1 is before the cursor and stays
// keyless; the fill resumes at f2.
func TestStableKeyFillResumesFromTheStoredCursor(t *testing.T) {
	t.Parallel()

	f := newFillDB(t, []fillSupport{
		{"scope-a", "gen-1", "f1", nil},
		{"scope-a", "gen-1", "f2", nil},
	}, map[string]string{"f1": "k1", "f2": "k2"})
	f.cursor = [3]string{"scope-a", "gen-1", "f1"}
	fill := SupplyChainImpactStableKeyFill{DB: f, ChunkSize: 10, VerifySliceSize: 10}
	if _, err := fill.RunStep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := fillKeys(f)
	if got["f2"] != "k2" || got["f1"] != "<nil>" {
		t.Fatalf("keys after resuming = %v, want f2 filled and f1 (before the cursor) untouched", got)
	}
}

// TestStableKeyFillCompletesOnlyAfterACleanVerification proves a keyless support
// whose twin has a key that appears after the sweep passed it (a gap) restarts the
// sweep instead of completing, and the marker is set only by the clean last slice.
func TestStableKeyFillCompletesOnlyAfterACleanVerification(t *testing.T) {
	t.Parallel()

	f := newFillDB(t, []fillSupport{
		{"scope-a", "gen-1", "f1", nil},
		{"scope-a", "gen-1", "f2", nil},
	}, map[string]string{"f1": "k1", "f2": "k2"})
	fill := SupplyChainImpactStableKeyFill{DB: f, ChunkSize: 10, VerifySliceSize: 10}
	// One step fills both supports; the sweep has not yet run its verification.
	if _, err := fill.RunStep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.complete {
		t.Fatal("the marker was set before the verification slice ran")
	}
	// A support another writer inserted, keyless, after the sweep passed its pair.
	f.supports = append(f.supports, fillSupport{"scope-a", "gen-1", "f0", nil})
	f.twins["f0"] = "k0"
	before := f.sweeps
	report, err := fill.RunStep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Verified || report.Missing != 1 || f.complete {
		t.Fatalf("report = %+v complete=%v, want a verified gap of 1 and no completion", report, f.complete)
	}
	if f.sweeps != before+1 {
		t.Fatalf("sweeps = %d, want a restart (%d)", f.sweeps, before+1)
	}
	runFillToCompletion(t, f, 10)
	if got := fillKeys(f)["f0"]; got != "k0" {
		t.Fatalf("the restarted sweep did not fill f0: %q", got)
	}
}

// TestStableKeyFillCompleteMarkerIsInert proves a complete marker makes the step a
// no-op: no update, no lock, no verification.
func TestStableKeyFillCompleteMarkerIsInert(t *testing.T) {
	t.Parallel()

	f := newFillDB(t, []fillSupport{{"scope-a", "gen-1", "f1", nil}}, map[string]string{"f1": "k1"})
	f.complete = true
	report, err := SupplyChainImpactStableKeyFill{DB: f}.RunStep(context.Background())
	if err != nil || !report.Complete {
		t.Fatalf("RunStep() = %+v, %v, want complete", report, err)
	}
	for _, e := range f.log {
		if e.query == supplyChainImpactStableKeyFillUpdateSQL || e.query == supplychaincore.SupplyChainImpactConflictDomainLockQuery {
			t.Fatalf("a complete marker still ran %q", e.query)
		}
	}
	if fillKeys(f)["f1"] != "<nil>" {
		t.Fatal("a complete marker still filled a support")
	}
}

// TestStableKeyFillMarkerNeverExceedsTheSupportsContractVersionOrTouchesItsRow is the
// reader-503 hazard pin (supports backfill, "contract_version > $2"): the fill's
// marker row carries a contract_version at most the supports contract version, is a
// different row from the supports marker, and every marker statement the fill issues
// binds only its own marker name.
func TestStableKeyFillMarkerNeverExceedsTheSupportsContractVersionOrTouchesItsRow(t *testing.T) {
	t.Parallel()

	if SupplyChainImpactStableKeyFillContractVersion > SupplyChainImpactSupportsContractVersion {
		t.Fatalf("the fill marker's contract_version %d exceeds the supports contract version %d: every supports reader would answer 503",
			SupplyChainImpactStableKeyFillContractVersion, SupplyChainImpactSupportsContractVersion)
	}
	if SupplyChainImpactStableKeyFillMarker == SupplyChainImpactSupportsBackfillMarker {
		t.Fatal("the fill shares the supports backfill's marker row")
	}
	f := newFillDB(t, []fillSupport{{"scope-a", "gen-1", "f1", nil}}, map[string]string{"f1": "k1"})
	runFillToCompletion(t, f, 10)
	if !f.markerSeen || f.markerContractVersion != SupplyChainImpactStableKeyFillContractVersion {
		t.Fatalf("the fill ensured its marker at contract version %d (seen %v), want %d", f.markerContractVersion, f.markerSeen, SupplyChainImpactStableKeyFillContractVersion)
	}
	if len(f.markerNames) != 1 || !f.markerNames[SupplyChainImpactStableKeyFillMarker] {
		t.Fatalf("the fill touched marker rows %v, want only %q", f.markerNames, SupplyChainImpactStableKeyFillMarker)
	}
}

// TestStableKeyFillStatementsTouchOnlyTheKeyColumnOfKeylessSupports pins the shape
// of the update and the verification.
func TestStableKeyFillStatementsTouchOnlyTheKeyColumnOfKeylessSupports(t *testing.T) {
	t.Parallel()

	update := supplyChainImpactStableKeyFillUpdateSQL
	for name, want := range map[string]string{
		"sets only the key":       "SET stable_fact_key = f.stable_fact_key",
		"never overwrites":        "s.stable_fact_key IS NULL",
		"by fact id list":         "s.fact_id = ANY($1::text[])",
		"twin by primary key":     "f.fact_id = s.fact_id",
		"skips an empty twin key": "f.stable_fact_key <> ''",
	} {
		if !strings.Contains(update, want) {
			t.Errorf("the fill update %s: missing %q", name, want)
		}
	}
	if strings.Count(update, "SET ") != 1 || strings.Contains(update, "payload") || strings.Contains(update, "updated_at") {
		t.Errorf("the fill update sets more than the key:\n%s", update)
	}
	slice := supplyChainImpactStableKeyFillVerifySliceSQL
	for name, want := range map[string]string{
		"counts keyless supports with a keyed twin": "x.stable_fact_key IS NULL",
		"twin key nonempty":                         "f.stable_fact_key <> ''",
		"bounded by the slice size":                 "LIMIT $4::bigint",
	} {
		if !strings.Contains(slice, want) {
			t.Errorf("the verify slice %s: missing %q", name, want)
		}
	}
	if !strings.Contains(supplyChainImpactStableKeyFillChunkSQL, "stable_fact_key IS NULL") ||
		!strings.Contains(supplyChainImpactStableKeyFillChunkSQL, "LIMIT $4") {
		t.Error("the chunk statement does not list only keyless supports with a limit")
	}
}

// TestStableKeyFillVerificationAdvancesInSlicesAndCompletesOnTheLast proves the
// sliced verify: five supports with a slice size of two take three slices, the first
// two advance the verification cursor and leave the marker unset, and only the last
// (short) slice completes it.
func TestStableKeyFillVerificationAdvancesInSlicesAndCompletesOnTheLast(t *testing.T) {
	t.Parallel()

	var supports []fillSupport
	twins := map[string]string{}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("f%d", i)
		supports = append(supports, fillSupport{"scope-a", "gen-1", id, nil})
		twins[id] = "k" + id
	}
	f := newFillDB(t, supports, twins)
	fill := SupplyChainImpactStableKeyFill{DB: f, ChunkSize: 10, VerifySliceSize: 2}
	if _, err := fill.RunStep(context.Background()); err != nil { // the fill
		t.Fatal(err)
	}
	for slice := 1; slice <= 3; slice++ {
		report, err := fill.RunStep(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !report.Verified {
			t.Fatalf("step %d ran no verification slice", slice)
		}
		if wantComplete := slice == 3; report.Complete != wantComplete || f.complete != wantComplete {
			t.Fatalf("slice %d: complete = %v (marker %v), want %v", slice, report.Complete, f.complete, wantComplete)
		}
		if slice < 3 && !f.verifySet {
			t.Fatalf("slice %d did not advance the verification cursor", slice)
		}
	}
}
