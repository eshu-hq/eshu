// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// largeRowCorpus is a 3,000-row key space of cheap rows with one row whose
// recheck costs largeCost at index docRow. The tail cannot finish while that
// row lies ahead of its cursor (it meets the row 10 ms in and the cancel lands
// when the recheck ends) and finishes in 100 ms once the row is behind it.
func largeRowCorpus(docRow int, largeCost time.Duration, matches ...int) *fakeCorpus {
	match := make(map[int]bool, len(matches))
	for _, m := range matches {
		match[m] = true
	}
	corpus := newCorpus(3000, 37*time.Microsecond, func(i int) bool { return match[i] })
	corpus.rows[docRow].cost = largeCost
	corpus.tailCost = func(from int) time.Duration {
		if from <= docRow {
			return never
		}
		return 100 * ms
	}
	corpus.tailLag = 3 * ms
	corpus.tailDoc = &fakeTailDoc{row: docRow, start: 10 * ms, end: 10*ms + largeCost}
	return corpus
}

func cursorAt(corpus *fakeCorpus, row int) querycontract.SearchCursor {
	return querycontract.SearchCursor{RepoID: corpus.rows[row].repo, RelativePath: corpus.rows[row].path}
}

// A resume chain that starts just before a 500-row window holding one large
// row must cross it. With the flat 300 ms window timeout the window is
// cancelled after its row was paid for, the next call's probe meets the same
// row and is cancelled too, and the call returns progressed=false: a dead end
// for every caller. With the phase room as the timeout the window completes
// (370 ms, inside the 440 ms phase room) or the next call's probe, whose
// timeout is the remaining budget, completes it (470 ms, the measured cost of
// the QA window that holds the 33.2 MB document).
func TestResumeCrossesWindowWithLargeRow(t *testing.T) {
	t.Parallel()

	const start = 1000             // cursor row: the walk reads from row 1001
	const docRow = start + 1 + 300 // inside the first 500-row step after the probe
	for _, tc := range []struct {
		name          string
		cost          time.Duration
		maxCalls      int
		firstCancels  int
		wantFirstDone bool
	}{
		{name: "370 ms row", cost: 370 * ms, maxCalls: 1, firstCancels: 0, wantFirstDone: true},
		// Over the 440 ms phase room: call 1 cancels the step and then
		// the tail (the row is still ahead of it); call 2's probe pays for it.
		{name: "470 ms row", cost: 470 * ms, maxCalls: 2, firstCancels: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			corpus := largeRowCorpus(docRow, tc.cost, start+400, start+900, start+1500)
			cursor := cursorAt(corpus, start)
			var gathered []string
			firstCancels, calls := -1, 0
			for call := 1; ; call++ {
				if call > 5 {
					t.Fatalf("no complete answer after 5 calls")
				}
				calls = call
				page, fdb, _, err := run(t, corpus, 10, 0, cursor)
				if err != nil {
					t.Fatalf("call %d: Search() error = %v", call, err)
				}
				if call == 1 {
					firstCancels = fdb.tx.cancels
				}
				gathered = append(gathered, keys(page.Files)...)
				if page.Partial == nil {
					if call == 1 && !tc.wantFirstDone {
						t.Fatalf("call 1 completed; the fixture must force a resume")
					}
					break
				}
				if !page.Partial.Progressed {
					t.Fatalf("call %d: partial made no progress (dead end at the large row): %+v", call, page.Partial)
				}
				cursor = page.Partial.Cursor
			}
			if want := matchKeys(corpus); !equalKeys(gathered, want) {
				t.Fatalf("gathered %v, want %v", gathered, want)
			}
			if calls > tc.maxCalls {
				t.Fatalf("complete answer after %d calls, want at most %d", calls, tc.maxCalls)
			}
			if firstCancels != tc.firstCancels {
				t.Fatalf("call 1: cancelled statements = %d, want %d", firstCancels, tc.firstCancels)
			}
		})
	}
}

// A call whose probe window holds one 700 ms row (the measured cost of the QA
// window that holds the 54.9 MB document) completes the probe: its timeout is
// the remaining budget, so the call advances the cursor past the row instead
// of returning progressed=false.
func TestProbeWindowWithLargeRowProgresses(t *testing.T) {
	t.Parallel()

	const start = 1000
	const docRow = start + 1 + 100 // inside the 200-row probe
	corpus := largeRowCorpus(docRow, 700*ms, start+400)
	page, fdb, _, err := run(t, corpus, 10, 0, cursorAt(corpus, start))
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if fdb.tx.cancels != 0 {
		t.Fatalf("cancelled statements = %d, want the probe to complete", fdb.tx.cancels)
	}
	if page.Partial != nil {
		if !page.Partial.Progressed {
			t.Fatalf("partial made no progress: %+v", page.Partial)
		}
		if got, want := page.Partial.Cursor, cursorAt(corpus, start+200); got != want {
			t.Fatalf("cursor = %+v, want the probe's last row %+v", got, want)
		}
	}
}

// TestSpreadProbeWindowTrade pins the disclosed trade of the room timeouts
// (#7730 R1-2 ruling, decision 7) on a probe window whose cost is spread
// evenly over its 200 rows, so a cancel lands within one cheap row of the
// timeout. Matches sit at 40,000, 80,000 and 120,000, beyond the walk.
//
//   - (i) 600 ms window, 300 ms tail: the probe now completes and leaves the
//     tail too little room, so the call is partial and resumable. The flat
//     300 ms probe timeout answered it exhausted with 3 rows in about 603 ms.
//     This is the pinned cost, not a fix.
//   - (ii) 600 ms window, 500 ms tail: the flat timeout cancelled the probe
//     and the tail, a dead end (progressed=false at about 706 ms); the call
//     now progresses. RED on the flat timeout, GREEN now.
//   - (iii) 900 ms window, 300 ms tail: the window costs more than the whole
//     budget, the stated limit, so the probe is cancelled and the call makes
//     no progress. The flat timeout answered it exhausted in about 601 ms:
//     the disclosed regression. No measured window is near it.
func TestSpreadProbeWindowTrade(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		window, tail time.Duration
		progressed   bool
		tailRan      bool
		cancels      int
		minMS, maxMS int64
	}{
		{name: "(i) 600 ms window, 300 ms tail", window: 600 * ms, tail: 300 * ms, progressed: true, tailRan: true, cancels: 1, minMS: 800, maxMS: 810},
		{name: "(ii) 600 ms window, 500 ms tail", window: 600 * ms, tail: 500 * ms, progressed: true, tailRan: true, cancels: 1, minMS: 800, maxMS: 810},
		{name: "(iii) 900 ms window, 300 ms tail", window: 900 * ms, tail: 300 * ms, progressed: false, tailRan: false, cancels: 1, minMS: 800, maxMS: 805},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			corpus := policyCorpus(tc.tail, 40000, 80000, 120000)
			for i := range probeRowsForTest {
				corpus.rows[i].cost = tc.window / probeRowsForTest
			}
			page, fdb, clock, err := run(t, corpus, 10, 0, querycontract.SearchCursor{})
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			elapsed := clock.Now().Sub(time.Unix(1_700_000_000, 0)).Milliseconds()
			tailRan, tailCancelled := tailPhases(fdb.tx)
			if page.Partial == nil {
				t.Fatalf("got a complete page with %d rows at %d ms; want partial 0 rows progressed=%v", len(page.Files), elapsed, tc.progressed)
			}
			if len(page.Files) != 0 || page.Partial.Progressed != tc.progressed {
				t.Fatalf("got partial %d rows progressed=%v at %d ms; want 0 rows progressed=%v",
					len(page.Files), page.Partial.Progressed, elapsed, tc.progressed)
			}
			if tailRan != tc.tailRan || (tc.tailRan && !tailCancelled) {
				t.Fatalf("tail_ran=%v tail_cancelled=%v; want tail_ran=%v and cancelled when it ran", tailRan, tailCancelled, tc.tailRan)
			}
			if fdb.tx.cancels != tc.cancels {
				t.Fatalf("cancelled statements = %d, want %d", fdb.tx.cancels, tc.cancels)
			}
			if elapsed < tc.minMS || elapsed > tc.maxMS {
				t.Fatalf("elapsed = %d ms, want %d..%d ms", elapsed, tc.minMS, tc.maxMS)
			}
		})
	}
}

// probeRowsForTest is the probe window size the walk uses (200 rows).
const probeRowsForTest = 200
