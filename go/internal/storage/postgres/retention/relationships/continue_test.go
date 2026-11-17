// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// limitOf reads the row limit a picking delete was called with, and 0 when the
// call carries none, so a statement that lost its limit fails a test by
// assertion instead of an index panic.
func limitOf(c call) int {
	if len(c.args) < 2 {
		return 0
	}
	limit, _ := c.args[1].(int)
	return limit
}

// limitHit scripts the evidence delete of the n-th batch (1-based) to report
// exactly the row limit it was called with, which is how a batch learns the
// statement stopped at its bound, and every other batch to report fewer rows. The
// settle read answers with the ids stillLeft returns for that batch.
func limitHit(hit map[int]bool, rows int64, stillLeft func(batch int) []string) func(c call) (reply, bool) {
	var evidenceCalls int
	return func(c call) (reply, bool) {
		switch c.query {
		case deleteEvidenceQuery:
			evidenceCalls++
			if hit[evidenceCalls] {
				return reply{affected: int64(limitOf(c))}, true
			}
			return reply{affected: rows}, true
		case settleQuery:
			return reply{rows: idRows(stillLeft(evidenceCalls)...)}, true
		}
		return reply{}, false
	}
}

// TestPickedDeletesCarryTheRowLimit pins the bound that keeps one batch small
// when a generation is large: the two statements that pick rows take the limit
// as their second argument, the generation delete takes only the ids, and the
// limit is BatchRows unless a test lowers it.
func TestPickedDeletesCarryTheRowLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts Options
		want int
	}{
		{"default", Options{Deletes: true}, BatchRows},
		{"lowered by a test", Options{Deletes: true, BatchRows: 7}, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, database, err := sweepWith(t, script{evidence: []string{"g1"}, rows: [3]int64{1, 1, 1}}, tc.opts)
			if err != nil {
				t.Fatalf("Sweep: %v", err)
			}
			for _, query := range []string{deleteEvidenceQuery, deleteCandidatesQuery} {
				calls := database.callsFor(query)
				if len(calls) != 1 || len(calls[0].args) != 2 || calls[0].args[1] != tc.want {
					t.Fatalf("picked delete args = %v, want the ids and a row limit of %d", calls, tc.want)
				}
			}
			if calls := database.callsFor(deleteGenerationsQuery); len(calls) != 1 || len(calls[0].args) != 1 {
				t.Fatalf("generation delete args = %v, want only the ids: it is bounded by its ids", calls)
			}
		})
	}
}

// TestBatchThatHitsTheRowLimitContinuesTheGenerationsItLeft pins what a large
// generation costs: the evidence delete stops at the limit, the settle read
// names g2 as still having rows, and the pass runs g2 again in its own batch
// instead of calling it lock_busy (nothing holds it, the batch only ran out of
// row budget). Every generation ends up swept exactly once.
func TestBatchThatHitsTheRowLimitContinuesTheGenerationsItLeft(t *testing.T) {
	t.Parallel()
	s := script{
		evidence: []string{"g1", "g2", "g3"},
		override: limitHit(map[int]bool{1: true}, 2, func(batch int) []string {
			if batch == 1 {
				return []string{"g2"}
			}
			return nil
		}),
	}
	result, database, err := sweepWith(t, s, Options{Deletes: true, BatchRows: 4})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	evidence := database.callsFor(deleteEvidenceQuery)
	if len(evidence) != 2 {
		t.Fatalf("evidence deletes = %d, want a second batch for the generation the first left", len(evidence))
	}
	if got := idsArg(evidence[0]); !slices.Equal(got, []string{"g1", "g2", "g3"}) {
		t.Fatalf("first batch ids = %v, want all three", got)
	}
	if got := idsArg(evidence[1]); !slices.Equal(got, []string{"g2"}) {
		t.Fatalf("second batch ids = %v, want only the generation the first left", got)
	}
	if result.GenerationsSwept != 3 || result.Batches != 2 || database.commits != 2 {
		t.Fatalf("swept %d in %d batches (%d commits), want 3 swept, once each, in 2 committed batches", result.GenerationsSwept, result.Batches, database.commits)
	}
	want := map[string]int{SkipLockBusy: 0, SkipLockTimeout: 0, SkipIneligible: 0}
	if !reflect.DeepEqual(result.Skipped, want) {
		t.Fatalf("skipped = %v, want %v: a generation cut by the row limit is not held by anyone", result.Skipped, want)
	}
	if result.Capped != "" {
		t.Fatalf("Capped = %q, want a clean pass: the continuation finished inside the budget", result.Capped)
	}
}

// TestCandidatesDeleteThatHitsTheRowLimitContinuesToo pins that the bound
// applies to both picked statements: a generation with few evidence rows and many
// candidate rows is cut by the candidates statement, not the evidence one.
func TestCandidatesDeleteThatHitsTheRowLimitContinuesToo(t *testing.T) {
	t.Parallel()
	var candidateCalls int
	s := script{
		evidence: []string{"g1"},
		override: func(c call) (reply, bool) {
			switch c.query {
			case deleteCandidatesQuery:
				candidateCalls++
				if candidateCalls == 1 {
					return reply{affected: int64(limitOf(c))}, true
				}
				return reply{affected: 1}, true
			case settleQuery:
				if candidateCalls == 1 {
					return reply{rows: idRows("g1")}, true
				}
				return reply{}, true
			}
			return reply{}, false
		},
	}
	result, _, err := sweepWith(t, s, Options{Deletes: true, BatchRows: 3})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if candidateCalls != 2 || result.GenerationsSwept != 1 || result.Batches != 2 {
		t.Fatalf("candidate deletes %d, swept %d in %d batches, want 2 batches and g1 swept once", candidateCalls, result.GenerationsSwept, result.Batches)
	}
}

// TestRowsAnotherSessionHoldsAreBusyOnlyOnceTheBatchIsNotCut pins how the two
// causes of a leftover separate: the first batch stops at the limit (g1 is
// continued, not busy), and the continuation, which no longer reaches the limit,
// still finds g1's remaining rows held by a purge: only then is it lock_busy.
func TestRowsAnotherSessionHoldsAreBusyOnlyOnceTheBatchIsNotCut(t *testing.T) {
	t.Parallel()
	s := script{
		evidence: []string{"g1"},
		override: limitHit(map[int]bool{1: true}, 1, func(int) []string { return []string{"g1"} }),
	}
	result, database, err := sweepWith(t, s, Options{Deletes: true, BatchRows: 4})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.Batches != 2 || database.commits != 2 {
		t.Fatalf("batches = %d (%d commits), want the cut batch and one continuation, both committed (each deleted rows)", result.Batches, database.commits)
	}
	if result.GenerationsSwept != 0 || result.Skipped[SkipLockBusy] != 1 {
		t.Fatalf("swept %d, skipped %v; want g1 lock_busy exactly once, counted by the continuation", result.GenerationsSwept, result.Skipped)
	}
}

// TestContinuationStopsAtTheRowCap pins that a generation still being drained
// does not outrun the pass budget: the cut batch's rows reach the cap, the pass
// stops with orphans left and says so (CapRows), and the half-drained generation
// is neither swept nor counted as skipped. The next pass lists it again.
func TestContinuationStopsAtTheRowCap(t *testing.T) {
	t.Parallel()
	s := script{
		evidence: []string{"g1"},
		override: limitHit(map[int]bool{1: true, 2: true}, 0, func(int) []string { return []string{"g1"} }),
	}
	result, database, err := sweepWith(t, s, Options{Deletes: true, BatchRows: 4, RowCap: 4})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.Batches != 1 || len(database.callsFor(deleteEvidenceQuery)) != 1 {
		t.Fatalf("batches = %d, want the first batch only: its rows reached the cap", result.Batches)
	}
	if result.Capped != CapRows {
		t.Fatalf("Capped = %q, want %q so the runner shortens its next wait", result.Capped, CapRows)
	}
	if result.GenerationsSwept != 0 || result.Skipped[SkipLockBusy] != 0 {
		t.Fatalf("swept %d, skipped %v; want the half-drained generation left unclaimed by the accounting", result.GenerationsSwept, result.Skipped)
	}
}

// TestContinuationStopsAtTheDeadline pins the same for the wall budget.
func TestContinuationStopsAtTheDeadline(t *testing.T) {
	t.Parallel()
	clock := time.Unix(1_000, 0)
	base := limitHit(map[int]bool{1: true, 2: true, 3: true}, 0, func(int) []string { return []string{"g1"} })
	s := script{
		evidence: []string{"g1"},
		override: func(c call) (reply, bool) {
			if c.query == deleteGenerationsQuery {
				clock = clock.Add(40 * time.Second)
			}
			return base(c)
		},
	}
	opts := Options{Deletes: true, BatchRows: 4, RowCap: 1 << 40, Deadline: time.Unix(1_000, 0).Add(time.Minute)}
	opts.now = func() time.Time { return clock }
	result, _, err := sweepWith(t, s, opts)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.Batches != 2 || result.Capped != CapDeadline {
		t.Fatalf("batches %d capped %q, want 2 batches then the deadline", result.Batches, result.Capped)
	}
}

// TestContinuedGenerationsFinishBeforeTheNextSliceStarts pins the order: a slice
// of the census list is repeated until it is done, so one large generation never
// holds up a second slice's locks while it drains.
func TestContinuedGenerationsFinishBeforeTheNextSliceStarts(t *testing.T) {
	t.Parallel()
	s := script{
		evidence: []string{"g1", "g2"},
		override: limitHit(map[int]bool{1: true}, 1, func(batch int) []string {
			if batch == 1 {
				return []string{"g1"}
			}
			return nil
		}),
	}
	result, database, err := sweepWith(t, s, Options{Deletes: true, BatchRows: 4, BatchGenerations: 1})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	var got [][]string
	for _, c := range database.callsFor(tryLockQuery) {
		got = append(got, idsArg(c))
	}
	want := [][]string{{"g1"}, {"g1"}, {"g2"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batches ran for %v, want %v: g1 is continued before g2 starts", got, want)
	}
	if result.GenerationsSwept != 2 || result.Batches != 3 {
		t.Fatalf("swept %d in %d batches, want 2 swept in 3", result.GenerationsSwept, result.Batches)
	}
}

// TestCommittedBatchLogLineSaysHowManyGenerationsItHandedOn pins what an
// operator reads while a large generation drains: the cut batch finishes no
// generation and continues one (batch_generations 0, batch_continued 1), and the
// batch that finishes it continues none.
func TestCommittedBatchLogLineSaysHowManyGenerationsItHandedOn(t *testing.T) {
	t.Parallel()
	s := script{
		evidence: []string{"g1"},
		override: limitHit(map[int]bool{1: true}, 2, func(batch int) []string {
			if batch == 1 {
				return []string{"g1"}
			}
			return nil
		}),
	}
	var logs bytes.Buffer
	opts := Options{Deletes: true, BatchRows: 4, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	if _, _, err := sweepWith(t, s, opts); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	var got [][2]float64 // batch_generations, batch_continued, per committed batch
	for _, line := range strings.Split(logs.String(), "\n") {
		if !strings.Contains(line, "batch committed") {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		generations, _ := entry["batch_generations"].(float64)
		continued, ok := entry["batch_continued"].(float64)
		if !ok {
			t.Fatalf("batch line has no batch_continued: %s", line)
		}
		got = append(got, [2]float64{generations, continued})
	}
	want := [][2]float64{{0, 1}, {1, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("(batch_generations, batch_continued) per batch = %v, want %v", got, want)
	}
}
