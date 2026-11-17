// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// script answers the census with fixed id lists and every batch statement with
// the happy path, unless override answers first.
type script struct {
	evidence, candidates, generations []string
	// rows is what one batch's deletes report: evidence, candidates, generations.
	rows     [3]int64
	override func(c call) (reply, bool)
}

func (s script) handle(c call) reply {
	if s.override != nil {
		if r, ok := s.override(c); ok {
			return r
		}
	}
	switch c.query {
	case censusEvidenceQuery:
		return reply{rows: idRows(truncate(s.evidence, c)...)}
	case censusCandidatesQuery:
		return reply{rows: idRows(truncate(s.candidates, c)...)}
	case censusGenerationsQuery:
		return reply{rows: idRows(truncate(s.generations, c)...)}
	case tryLockQuery:
		rows := [][]any{}
		for _, id := range idsArg(c) {
			rows = append(rows, []any{id, true})
		}
		return reply{rows: rows}
	case classifyQuery:
		rows := [][]any{}
		for _, id := range idsArg(c) {
			rows = append(rows, []any{id, true, true})
		}
		return reply{rows: rows}
	case deleteEvidenceQuery:
		return reply{affected: s.rows[0]}
	case deleteCandidatesQuery:
		return reply{affected: s.rows[1]}
	case deleteGenerationsQuery:
		return reply{affected: s.rows[2]}
	case settleQuery:
		// Nothing left behind unless a test says which claimed ids are.
		return reply{}
	}
	return reply{}
}

// truncate applies the census LIMIT the statement was called with.
func truncate(ids []string, c call) []string {
	if limit, ok := c.args[0].(int); ok && len(ids) > limit {
		return ids[:limit]
	}
	return ids
}

func ids(prefix string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%s-%04d", prefix, i))
	}
	return out
}

func sweepWith(t *testing.T, s script, opts Options) (Result, *scriptedDB, error) {
	t.Helper()
	database := &scriptedDB{handler: s.handle}
	result, err := Sweep(context.Background(), database, opts)
	return result, database, err
}

// TestBatchRunsStatementsInFixedOrder pins the sweep shape the arbiter ruled:
// one transaction per batch, lock_timeout first, per-generation try lock,
// classification with the rg row lock, then evidence, candidates and the
// generation row last (the #10820 purge deletes them in the same order), a
// settle read of what was left behind, one commit.
func TestBatchRunsStatementsInFixedOrder(t *testing.T) {
	t.Parallel()
	s := script{evidence: []string{"g1", "g2"}, candidates: []string{"g2", "g3"}, rows: [3]int64{7, 3, 3}}
	result, database, err := sweepWith(t, s, Options{Deletes: true})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	// Transaction 1 is the census, transaction 2 the only batch.
	wantBatch := []string{
		setLockTimeoutQuery, tryLockQuery, classifyQuery,
		deleteEvidenceQuery, deleteCandidatesQuery, deleteGenerationsQuery, settleQuery,
	}
	if got := database.queriesIn(2); !reflect.DeepEqual(got, wantBatch) {
		t.Fatalf("batch statements out of order:\n got %v\nwant %v", got, wantBatch)
	}
	first := database.callsFor(setLockTimeoutQuery)[0]
	if first.args[0] != "500ms" {
		t.Fatalf("lock_timeout argument = %v, want 500ms", first.args[0])
	}
	if database.begins != 2 || database.commits != 1 || database.rollbacks != 1 {
		t.Fatalf("begun %d committed %d rolled back %d, want the census (rolled back, it only reads) and one committed batch",
			database.begins, database.commits, database.rollbacks)
	}
	if result.GenerationsSwept != 3 || result.Batches != 1 {
		t.Fatalf("swept %d generations in %d batches, want 3 in 1", result.GenerationsSwept, result.Batches)
	}
	wantRows := map[string]int64{EvidenceTable: 7, CandidatesTable: 3, GenerationsTable: 3}
	if !reflect.DeepEqual(result.RowsSwept, wantRows) {
		t.Fatalf("rows swept = %v, want %v", result.RowsSwept, wantRows)
	}
}

// TestDeletesReceiveOnlyTheClassifiedIDs pins that the deletes take the
// classification statement's output, not the batch input: a generation whose
// generation row another session holds (E8) still satisfies every Rule A
// predicate, so passing it on would delete its children under someone else's
// lock. A generation whose advisory lock is held never reaches classification.
func TestDeletesReceiveOnlyTheClassifiedIDs(t *testing.T) {
	t.Parallel()
	s := script{
		evidence: []string{"busy", "held", "ineligible", "ok-1", "ok-2"},
		rows:     [3]int64{1, 1, 1},
		override: func(c call) (reply, bool) {
			switch c.query {
			case tryLockQuery:
				return reply{rows: [][]any{
					{"busy", false}, {"held", true}, {"ineligible", true}, {"ok-1", true}, {"ok-2", true},
				}}, true
			case classifyQuery:
				// held: the predicates hold but another session has the row;
				// ineligible: a scope_generations row exists.
				return reply{rows: [][]any{
					{"held", true, false}, {"ineligible", false, false}, {"ok-1", true, true}, {"ok-2", true, true},
				}}, true
			}
			return reply{}, false
		},
	}
	result, database, err := sweepWith(t, s, Options{Deletes: true})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	classifyArgs := idsArg(database.callsFor(classifyQuery)[0])
	if want := []string{"held", "ineligible", "ok-1", "ok-2"}; !slices.Equal(classifyArgs, want) {
		t.Fatalf("classify received %v, want only the try-locked ids %v", classifyArgs, want)
	}
	for _, query := range []string{deleteEvidenceQuery, deleteCandidatesQuery, deleteGenerationsQuery, settleQuery} {
		got := idsArg(database.callsFor(query)[0])
		if want := []string{"ok-1", "ok-2"}; !slices.Equal(got, want) {
			t.Errorf("a delete or the settle read received %v, want only the classified ids %v", got, want)
		}
	}
	wantSkipped := map[string]int{SkipLockBusy: 2, SkipIneligible: 1, SkipLockTimeout: 0}
	if !reflect.DeepEqual(result.Skipped, wantSkipped) {
		t.Fatalf("skipped = %v, want %v (advisory busy + held row = lock_busy)", result.Skipped, wantSkipped)
	}
	if result.GenerationsSwept != 2 {
		t.Fatalf("swept %d generations, want 2", result.GenerationsSwept)
	}
}

// TestBatchWithNothingEligibleOpensNoDeletes pins that a batch whose
// generations are all held or ineligible rolls back without a delete.
func TestBatchWithNothingEligibleOpensNoDeletes(t *testing.T) {
	t.Parallel()
	s := script{
		evidence: []string{"a"},
		override: func(c call) (reply, bool) {
			if c.query == tryLockQuery {
				return reply{rows: [][]any{{"a", false}}}, true
			}
			return reply{}, false
		},
	}
	result, database, err := sweepWith(t, s, Options{Deletes: true})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	for _, query := range []string{classifyQuery, deleteEvidenceQuery, deleteCandidatesQuery, deleteGenerationsQuery, settleQuery} {
		if n := len(database.callsFor(query)); n != 0 {
			t.Errorf("statement ran %d times for a batch with nothing locked: %.40s", n, query)
		}
	}
	if result.Skipped[SkipLockBusy] != 1 || result.Batches != 0 {
		t.Fatalf("skipped %v batches %d, want one lock_busy and no committed batch", result.Skipped, result.Batches)
	}
}

// TestBatchFaultRollsBackEverything pins the atomic batch: a failure after the
// evidence delete (the fault hook stands in for a crash between statements)
// rolls the whole transaction back, commits nothing and surfaces the error.
func TestBatchFaultRollsBackEverything(t *testing.T) {
	t.Parallel()
	database := &scriptedDB{handler: script{evidence: []string{"g1"}, rows: [3]int64{5, 2, 1}}.handle}
	fault := errors.New("injected crash after evidence")
	opts := Options{Deletes: true}
	opts.afterStatement = func(stage string) error {
		if stage == stageDeleteEvidence {
			return fault
		}
		return nil
	}
	result, err := Sweep(context.Background(), database, opts)
	if !errors.Is(err, fault) {
		t.Fatalf("Sweep error = %v, want the injected fault", err)
	}
	if database.commits != 0 {
		t.Fatalf("commits = %d, want none: the census only reads and the faulted batch must not commit", database.commits)
	}
	if database.rollbacks != 2 {
		t.Fatalf("rollbacks = %d, want the census and the faulted batch", database.rollbacks)
	}
	if len(database.callsFor(deleteCandidatesQuery))+len(database.callsFor(deleteGenerationsQuery)) != 0 {
		t.Fatalf("statements ran after the fault")
	}
	if result.Batches != 0 || result.RowsSwept[EvidenceTable] != 0 || result.GenerationsSwept != 0 {
		t.Fatalf("a rolled back batch was counted: %+v", result)
	}
}

// TestLockTimeoutCountsTheWholeBatchAndTheSweepContinues pins the 55P03
// granularity: the error aborts the batch transaction, so every generation in
// that batch is a lock_timeout skip, and later batches still run (other
// generations do not wait behind the contended one).
func TestLockTimeoutCountsTheWholeBatchAndTheSweepContinues(t *testing.T) {
	t.Parallel()
	timedOut := false
	s := script{
		evidence: ids("g", 5),
		rows:     [3]int64{1, 1, 1},
		override: func(c call) (reply, bool) {
			if c.query == deleteEvidenceQuery && !timedOut {
				timedOut = true
				return reply{err: &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}}, true
			}
			return reply{}, false
		},
	}
	result, database, err := sweepWith(t, s, Options{Deletes: true, BatchGenerations: 2})
	if err != nil {
		t.Fatalf("Sweep: %v (a lock timeout is a skip, not a failure)", err)
	}
	if result.Skipped[SkipLockTimeout] != 2 {
		t.Fatalf("lock_timeout skips = %d, want the 2 generations of the aborted batch", result.Skipped[SkipLockTimeout])
	}
	if result.Batches != 2 || result.GenerationsSwept != 3 {
		t.Fatalf("committed %d batches / %d generations, want the 2 later batches and 3 generations", result.Batches, result.GenerationsSwept)
	}
	if database.rollbacks < 1 {
		t.Fatalf("the timed out batch was not rolled back")
	}
	if result.Capped != "" {
		t.Fatalf("Capped = %q after a lock timeout on a fully walked list, want none", result.Capped)
	}
}

// TestDeadlockIsAFailureNotASkip pins that a 40P01 on the sweeper is returned
// as an error (counted and logged as a failure), never absorbed as a skip: the
// rollout gate is zero deadlocks, and the sweeper's 500 ms lock_timeout is
// meant to make it unreachable.
func TestDeadlockIsAFailureNotASkip(t *testing.T) {
	t.Parallel()
	s := script{
		evidence: []string{"g1"},
		override: func(c call) (reply, bool) {
			if c.query == deleteEvidenceQuery {
				return reply{err: &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}}, true
			}
			return reply{}, false
		},
	}
	result, _, err := sweepWith(t, s, Options{Deletes: true})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40P01" {
		t.Fatalf("Sweep error = %v, want the 40P01 surfaced", err)
	}
	if result.Skipped[SkipLockTimeout] != 0 {
		t.Fatalf("a deadlock was counted as a lock timeout skip: %v", result.Skipped)
	}
}

// TestSweepStopsAfterTheBatchThatCrossesTheRowCap pins the row cap: the pass
// stops before the next batch once the committed rows reach it, and reports
// Capped=rows because orphans are left. A batch bound that is missing would
// run every listed generation in one transaction (the cap test of the
// mutant table).
func TestSweepStopsAfterTheBatchThatCrossesTheRowCap(t *testing.T) {
	t.Parallel()
	s := script{evidence: ids("g", 10), rows: [3]int64{10, 5, 5}} // 20 rows per batch
	result, database, err := sweepWith(t, s, Options{Deletes: true, RowCap: 45, BatchGenerations: 2})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.Batches != 3 { // 20, 40, 60: the third crosses 45
		t.Fatalf("batches = %d, want 3 (the batch that crosses the cap is the last)", result.Batches)
	}
	if result.Capped != CapRows {
		t.Fatalf("Capped = %q, want %q", result.Capped, CapRows)
	}
	for _, c := range database.callsFor(tryLockQuery) {
		if n := len(idsArg(c)); n > 2 {
			t.Fatalf("a batch locked %d generations, want at most BatchGenerations=2", n)
		}
	}
	if result.GenerationsSwept != 6 {
		t.Fatalf("swept %d generations, want 6 of the 10 listed", result.GenerationsSwept)
	}
}

// TestFirstBatchRunsPastAnExhaustedBudget pins the precedent rule shared with
// the search index sweeps: a pass always runs its first batch, so a tiny cap or
// a spent deadline cannot starve the backlog.
func TestFirstBatchRunsPastAnExhaustedBudget(t *testing.T) {
	t.Parallel()
	s := script{evidence: ids("g", 6), rows: [3]int64{3, 3, 3}}
	opts := Options{Deletes: true, RowCap: 1, BatchGenerations: 2, Deadline: time.Unix(1, 0)}
	result, _, err := sweepWith(t, s, opts)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.Batches != 1 || result.GenerationsSwept != 2 {
		t.Fatalf("ran %d batches / %d generations, want exactly the first batch", result.Batches, result.GenerationsSwept)
	}
	if result.Capped != CapRows && result.Capped != CapDeadline {
		t.Fatalf("Capped = %q, want a cap reason", result.Capped)
	}
}

// TestSweepStopsAtTheDeadline pins the wall budget with a controlled clock.
func TestSweepStopsAtTheDeadline(t *testing.T) {
	t.Parallel()
	clock := time.Unix(1_000, 0)
	s := script{
		evidence: ids("g", 8),
		rows:     [3]int64{1, 1, 1},
		override: func(c call) (reply, bool) {
			if c.query == deleteGenerationsQuery {
				clock = clock.Add(40 * time.Second) // each batch takes 40 s
			}
			return reply{}, false
		},
	}
	opts := Options{Deletes: true, BatchGenerations: 2, Deadline: time.Unix(1_000, 0).Add(time.Minute)}
	opts.now = func() time.Time { return clock }
	result, _, err := sweepWith(t, s, opts)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.Batches != 2 || result.Capped != CapDeadline {
		t.Fatalf("batches %d capped %q, want 2 batches then deadline", result.Batches, result.Capped)
	}
}

// TestCleanPassIsNotCapped pins that a fully walked, unsaturated list is clean:
// the runner never accelerates after it.
func TestCleanPassIsNotCapped(t *testing.T) {
	t.Parallel()
	s := script{evidence: ids("g", 5), rows: [3]int64{1, 1, 1}}
	result, _, err := sweepWith(t, s, Options{Deletes: true, BatchGenerations: 2})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.Capped != "" || result.GenerationsSwept != 5 {
		t.Fatalf("capped %q swept %d, want a clean pass over 5", result.Capped, result.GenerationsSwept)
	}
}

// TestSaturatedListCapsAFullyWalkedList pins that when the census found more
// orphans than it lists, finishing the list is still a capped pass: orphans
// remain, so the runner may shorten its next wait.
func TestSaturatedListCapsAFullyWalkedList(t *testing.T) {
	t.Parallel()
	s := script{evidence: ids("g", CensusListLimit+10), rows: [3]int64{1, 1, 1}}
	result, _, err := sweepWith(t, s, Options{Deletes: true, RowCap: 1 << 40})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !result.Census.Saturated {
		t.Fatalf("census not saturated over %d ids", CensusListLimit+10)
	}
	if len(result.Census.Generations) != CensusListLimit || result.GenerationsSwept != CensusListLimit {
		t.Fatalf("listed %d swept %d, want the list limit %d", len(result.Census.Generations), result.GenerationsSwept, CensusListLimit)
	}
	if result.Capped != CapList {
		t.Fatalf("Capped = %q, want %q", result.Capped, CapList)
	}
}

// TestDeletesOffRunsTheCensusOnly pins the default: with deletes disabled the
// pass takes the census and begins no batch transaction and sends no DELETE.
func TestDeletesOffRunsTheCensusOnly(t *testing.T) {
	t.Parallel()
	s := script{evidence: ids("g", 3), candidates: ids("c", 2), generations: ids("r", 1), rows: [3]int64{1, 1, 1}}
	result, database, err := sweepWith(t, s, Options{})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if database.begins != 1 {
		t.Fatalf("transactions begun = %d, want only the census", database.begins)
	}
	for name, statement := range allStatements() {
		if len(database.callsFor(statement)) > 0 && (name == "try lock" || name == "classify" || len(name) > 6 && name[:6] == "delete") {
			t.Errorf("%s ran with deletes off", name)
		}
	}
	if result.Capped != "" || result.GenerationsSwept != 0 || result.Batches != 0 {
		t.Fatalf("census-only pass reported sweep work: %+v", result)
	}
	if got := result.Census.ByTable[EvidenceTable]; got != 3 {
		t.Fatalf("census evidence orphans = %d, want 3", got)
	}
}

// TestCensusCountsDistinctGenerationsPerTableAndSaturates pins the gauge
// inputs: distinct generations per table (a generation present in two tables
// counts once in each), the union list de-duplicated and sorted, and
// saturation at CensusListLimit.
func TestCensusCountsDistinctGenerationsPerTableAndSaturates(t *testing.T) {
	t.Parallel()
	s := script{evidence: []string{"g1", "g2"}, candidates: []string{"g2", "g3"}, generations: []string{"g1"}}
	database := &scriptedDB{handler: s.handle}
	census, err := TakeCensus(context.Background(), database)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	if want := []string{"g1", "g2", "g3"}; !slices.Equal(census.Generations, want) {
		t.Fatalf("union list = %v, want %v", census.Generations, want)
	}
	want := map[string]int64{EvidenceTable: 2, CandidatesTable: 2, GenerationsTable: 1}
	if !reflect.DeepEqual(census.ByTable, want) {
		t.Fatalf("per table = %v, want %v", census.ByTable, want)
	}
	if census.Saturated {
		t.Fatalf("a small census reports saturation")
	}

	big := &scriptedDB{handler: script{evidence: ids("e", CensusListLimit+50)}.handle}
	census, err = TakeCensus(context.Background(), big)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	if !census.Saturated || census.ByTable[EvidenceTable] != int64(CensusListLimit) {
		t.Fatalf("saturated %v evidence %d, want a count capped at the list limit %d", census.Saturated, census.ByTable[EvidenceTable], CensusListLimit)
	}
	for _, c := range big.callsFor(censusEvidenceQuery) {
		if c.args[0] != CensusListLimit+1 {
			t.Fatalf("census LIMIT = %v, want one past the list limit so saturation is detectable", c.args[0])
		}
	}
}

// TestCensusRunsUnderALocalStatementTimeout pins that the census statements run
// in one transaction whose first statement bounds their duration, so a census
// that scans a large table cannot hold a connection open indefinitely.
func TestCensusRunsUnderALocalStatementTimeout(t *testing.T) {
	t.Parallel()
	database := &scriptedDB{handler: script{}.handle}
	if _, err := TakeCensus(context.Background(), database); err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	queries := database.queriesIn(1)
	if len(queries) == 0 || queries[0] != setStatementTimeoutQuery {
		t.Fatalf("census statements = %v, want the local statement_timeout first", queries)
	}
	if got := database.callsFor(setStatementTimeoutQuery)[0].args[0]; got != censusStatementTimeoutSetting {
		t.Fatalf("statement_timeout = %v, want %s", got, censusStatementTimeoutSetting)
	}
	if database.begins != 1 || database.commits+database.rollbacks != 1 {
		t.Fatalf("census used %d transactions and %d endings, want one of each", database.begins, database.commits+database.rollbacks)
	}
}

// TestCensusFailureSurfaces pins that a failed census statement returns an
// error naming the table, so the runner records a failure instead of a zero.
func TestCensusFailureSurfaces(t *testing.T) {
	t.Parallel()
	boom := errors.New("statement timeout")
	database := &scriptedDB{handler: script{override: func(c call) (reply, bool) {
		if c.query == censusCandidatesQuery {
			return reply{err: boom}, true
		}
		return reply{}, false
	}}.handle}
	_, err := TakeCensus(context.Background(), database)
	if !errors.Is(err, boom) {
		t.Fatalf("TakeCensus error = %v, want the statement error", err)
	}
}
