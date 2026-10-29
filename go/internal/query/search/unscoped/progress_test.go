// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// stalledCorpus makes every window and the tail too slow for the budget: the
// 200-row probe costs 3 s, more than the whole 2000 ms budget, so the probe
// itself is cancelled and no window completes. The partial that results has
// made no progress.
func stalledCorpus() *fakeCorpus {
	// Sized past the moved-cursor resume point: a resume from ~22,200 rows
	// in must still find rows to stall on.
	corpus := newCorpus(60000, 15*ms, func(i int) bool { return i == 45000 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	return corpus
}

// stalledEndedIn is where the stalled walk stops: the probe's timeout is the
// whole budget, so its cancel leaves less than the tail floor and the tail is
// skipped.
const stalledEndedIn = "budget"

// A resume built from a partial that scanned nothing is the identical request:
// the cursor is the request cursor (empty on a first call) and no match was
// found. A client that blindly follows the resume rule would repeat it
// forever, so the partial must say it made no progress.
func TestZeroProgressPartialRepeatsTheRequest(t *testing.T) {
	t.Parallel()

	first, fdb, _, err := run(t, stalledCorpus(), 50, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	partial := first.Partial
	if partial == nil {
		t.Fatal("Partial = nil, want a partial result")
	}
	if fdb.tx.cancels < 1 {
		t.Fatalf("cancelled statements = %d, want the probe cancelled", fdb.tx.cancels)
	}
	if !partial.Cursor.IsZero() || partial.RowsMatched != 0 || partial.RowsScannedInOrder != 0 {
		t.Fatalf("partial = %+v, want an empty cursor, no matches and no rows scanned", partial)
	}
	// The documented resume rule: cursor as returned, offset max(0, offset - rows_matched).
	second, _, _, err := run(t, stalledCorpus(), 50, 0, partial.Cursor)
	if err != nil || second.Partial == nil {
		t.Fatalf("resume = (%+v, %v), want the same partial again", second, err)
	}
	if second.Partial.Cursor != partial.Cursor || second.Partial.RowsMatched != partial.RowsMatched {
		t.Fatalf("resume partial = %+v, want it identical to the first (%+v)", second.Partial, partial)
	}
}

// The partial states whether the call advanced the cursor, so a client can stop
// instead of retrying an identical request, and the hint then says what to do.
func TestPartialStatesWhetherTheCallMadeProgress(t *testing.T) {
	t.Parallel()

	stalled, _, _, err := run(t, stalledCorpus(), 50, 0, querycontract.SearchCursor{})
	if err != nil || stalled.Partial == nil {
		t.Fatalf("stalled search = (%+v, %v), want a partial", stalled, err)
	}
	if stalled.Partial.Progressed {
		t.Fatalf("Progressed = true on a call that scanned nothing: %+v", stalled.Partial)
	}
	for _, want := range []string{"repo_id", "ESHU_CONTENT_SEARCH_BUDGET_MS", "not"} {
		if !strings.Contains(stalled.Partial.Hint, want) {
			t.Errorf("no-progress hint %q does not mention %q", stalled.Partial.Hint, want)
		}
	}

	// A partial that advanced the cursor says so, and keeps the usual hint.
	// The corpus is larger than the capped continuation can exhaust, so the
	// tail meets the dense class and the budget ends the walk.
	corpus := newCorpus(60000, 50*time.Microsecond, func(i int) bool { return i >= 3000 && i%500 == 0 })
	corpus.tailCost = func(int) time.Duration { return 3 * time.Second }
	moved, _, _, err := run(t, corpus, 200, 0, querycontract.SearchCursor{})
	if err != nil || moved.Partial == nil {
		t.Fatalf("dense search = (%+v, %v), want a partial", moved, err)
	}
	if !moved.Partial.Progressed || moved.Partial.Hint != querycontract.SearchPartialHint {
		t.Fatalf("partial = %+v, want Progressed with the scoping hint", moved.Partial)
	}

	// Resuming from an advanced cursor that then stalls reports no progress too.
	stalledAgain, _, _, err := run(t, stalledCorpus(), 50, 0, moved.Partial.Cursor)
	if err != nil || stalledAgain.Partial == nil || stalledAgain.Partial.Progressed {
		t.Fatalf("resume that scanned nothing = (%+v, %v), want Progressed=false", stalledAgain.Partial, err)
	}
}
