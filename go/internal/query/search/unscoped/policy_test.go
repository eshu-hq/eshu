// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// policyCorpusRows is the length of the synthetic key space. It is past the
// deepest planted match (120,000) so every class has a real tail range.
const policyCorpusRows = 150_000

// largeDocumentRow is the 0-based key position of the synthetic large document,
// near the end of key order. Only the trigram tail meets it in these classes.
const largeDocumentRow = 146_841

// never is a tail cost no budget can pay: the tail is always cancelled.
const never = time.Minute

// policyCorpus builds a key space shaped like the measured corpus: rows 1 to
// 3,200 cost 37 µs each in a key-ordered walk and later rows 74 µs each, so a
// window to row 5,675 costs about 300 ms and the 0.55 cap reaches about row
// 7,200. The sizes are synthetic; positions are 1-based to match the ruling.
func policyCorpus(tailCost time.Duration, positions ...int) *fakeCorpus {
	match := make(map[int]bool, len(positions))
	for _, p := range positions {
		match[p-1] = true
	}
	corpus := newCorpus(policyCorpusRows, 0, func(i int) bool { return match[i] })
	for i := range corpus.rows {
		corpus.rows[i].cost = 37 * time.Microsecond
		if i >= 3_200 {
			corpus.rows[i].cost = 74 * time.Microsecond
		}
	}
	corpus.tailCost = func(int) time.Duration { return tailCost }
	corpus.tailLag = 3 * ms
	return corpus
}

// withLargeDocument makes the tail meet the large document between start and
// end after it starts; a cancel that fires in that span lands at end.
func withLargeDocument(corpus *fakeCorpus, start, end time.Duration) *fakeCorpus {
	corpus.tailDoc = &fakeTailDoc{row: largeDocumentRow, start: start, end: end}
	return corpus
}

// policyClass is one token class with the outcome the 0.55 continuation cap
// must give it at the default budget.
type policyClass struct {
	name           string
	corpus         func() *fakeCorpus
	outcome        string // exact, exhausted or partial
	rows           int
	tailRan        bool
	tailCancelled  bool
	minMS, maxMS   int64 // elapsed band on the fake clock
	largeDocReason bool  // partial reason budget_exceeded_on_large_document
}

// policyClasses are the classes of the #7730 continuation-policy ruling
// (2026-10-11, section 4). Each row is one token class; RED/GREEN roles are in
// the comments. The figures are the fake clock's, not deployed walls.
func policyClasses() []policyClass {
	return []policyClass{
		{
			// Unchanged: the probe alone fills the page.
			name:    "baseline: 11 matches by row 155",
			corpus:  func() *fakeCorpus { return policyCorpus(never, 15, 30, 45, 60, 75, 90, 105, 120, 135, 145, 155) },
			outcome: "exact", rows: 10, minMS: 0, maxMS: 20,
		},
		{
			// RED at the 0.15 cap: the tail meets the large document and the
			// answer is partial with 0 rows.
			name: "late-cluster: 5,659-5,675",
			corpus: func() *fakeCorpus {
				return withLargeDocument(policyCorpus(never, 5659, 5661, 5662, 5668, 5669, 5670, 5671, 5672, 5673, 5674, 5675), 155*ms, 855*ms)
			},
			outcome: "exact", rows: 10, minMS: 250, maxMS: 360,
		},
		{
			// RED at the 0.15 cap: partial with 5 rows.
			name: "spread: 11th match at 7,177",
			corpus: func() *fakeCorpus {
				return withLargeDocument(policyCorpus(never, 34, 201, 661, 1511, 3115, 3219, 5764, 5976, 6020, 6137, 7177), 155*ms, 855*ms)
			},
			outcome: "exact", rows: 10, minMS: 380, maxMS: 440,
		},
		{
			// Unchanged rows, faster: no tail.
			name: "early-then-cluster: 14, 1,505, 1,507, 5,660-5,674",
			corpus: func() *fakeCorpus {
				return policyCorpus(never, 14, 1505, 1507, 5660, 5668, 5669, 5670, 5671, 5672, 5673, 5674)
			},
			outcome: "exact", rows: 10, minMS: 250, maxMS: 360,
		},
		{
			// Rule 2 and the disclosed cost: two rows (the 0.15 cap gave one),
			// the tail meets the large document 110 ms in and the cancel lands
			// at 809 ms.
			name: "kafka-like, large document early",
			corpus: func() *fakeCorpus {
				return withLargeDocument(policyCorpus(1278*ms, 120, 5750, 21691, 22109, 22119), 110*ms, 809*ms)
			},
			outcome: "partial", rows: 2, tailRan: true, tailCancelled: true, minMS: 1150, maxMS: 1300, largeDocReason: true,
		},
		{
			name: "kafka-like, large document late",
			corpus: func() *fakeCorpus {
				return withLargeDocument(policyCorpus(1278*ms, 120, 5750, 21691, 22109, 22119), 140*ms, 921*ms)
			},
			outcome: "partial", rows: 2, tailRan: true, tailCancelled: true, minMS: 1280, maxMS: 1400, largeDocReason: true,
		},
		{
			// Rule 4 guard: without the large document the tail finishes and
			// the answer is complete. A policy that skips the tail after a
			// match fails this row.
			name:    "kafka-like without the large document, tail 150 ms",
			corpus:  func() *fakeCorpus { return policyCorpus(150*ms, 120, 5750, 21691, 22109, 22119) },
			outcome: "exhausted", rows: 5, tailRan: true, minMS: 500, maxMS: 650,
		},
		{
			// Unchanged: the continuation fills the page before any tail.
			name: "redis-like: 11 by row 3,219",
			corpus: func() *fakeCorpus {
				return policyCorpus(never, 291, 541, 3106, 3128, 3129, 3211, 3213, 3214, 3215, 3216, 3219)
			},
			outcome: "exact", rows: 10, minMS: 100, maxMS: 170,
		},
		{
			// Unchanged: 2 matches past the walk, a 430 ms cold tail.
			name:    "xyzzy-like: 2 matches past the walk, tail 430 ms",
			corpus:  func() *fakeCorpus { return policyCorpus(430*ms, 60000, 90000) },
			outcome: "partial", rows: 0, tailRan: true, tailCancelled: true, minMS: 780, maxMS: 830,
		},
		{
			// Pins the tail reserve after the cap: a 268 ms tail finishes.
			name:    "zero-match, tail 268 ms",
			corpus:  func() *fakeCorpus { return policyCorpus(268 * ms) },
			outcome: "exhausted", rows: 0, tailRan: true, minMS: 650, maxMS: 720,
		},
		{
			// Rule 1 guard. Seeded RED: a 0.65 cap leaves too little for the
			// 300 ms tail and turns this class partial.
			name:    "selective: 40,000, 80,000, 120,000, tail 300 ms",
			corpus:  func() *fakeCorpus { return policyCorpus(300*ms, 40000, 80000, 120000) },
			outcome: "exhausted", rows: 3, tailRan: true, minMS: 680, maxMS: 760,
		},
		{
			// Rule 1 guard against skipping the tail after a match. Seeded
			// RED: "skip the tail when a match was seen" turns it partial.
			name:    "early-match-selective: 150, 40,000, 80,000, tail 300 ms",
			corpus:  func() *fakeCorpus { return policyCorpus(300*ms, 150, 40000, 80000) },
			outcome: "exhausted", rows: 3, tailRan: true, minMS: 680, maxMS: 760,
		},
	}
}

// TestPolicyClasses pins the continuation policy (key-ordered phase capped at
// 0.55 x budget, tail unchanged) against every token class of the ruling, at
// the default budget, page size 10 and no cursor.
func TestPolicyClasses(t *testing.T) {
	t.Parallel()

	for _, tc := range policyClasses() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			corpus := tc.corpus()
			page, fdb, clock, err := run(t, corpus, 10, 0, querycontract.SearchCursor{})
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			outcome := "exact"
			switch {
			case page.Partial != nil:
				outcome = "partial"
			case !page.More:
				outcome = "exhausted"
			}
			elapsed := clock.Now().Sub(time.Unix(1_700_000_000, 0)).Milliseconds()
			tailRan, tailCancelled := tailPhases(fdb.tx)
			if outcome != tc.outcome || len(page.Files) != tc.rows || tailRan != tc.tailRan || tailCancelled != tc.tailCancelled {
				t.Fatalf("got %s %d rows tail_ran=%v tail_cancelled=%v at %d ms; want %s %d rows tail_ran=%v tail_cancelled=%v",
					outcome, len(page.Files), tailRan, tailCancelled, elapsed, tc.outcome, tc.rows, tc.tailRan, tc.tailCancelled)
			}
			if got, want := keys(page.Files), matchKeys(corpus); !equalKeys(got, want[:len(got)]) {
				t.Fatalf("files = %v, want a prefix of %v", got, want)
			}
			if elapsed < tc.minMS || elapsed > tc.maxMS {
				t.Fatalf("elapsed = %d ms, want %d..%d ms", elapsed, tc.minMS, tc.maxMS)
			}
			if page.Partial == nil {
				return
			}
			if !page.Partial.Progressed {
				t.Fatalf("Progressed = false: the walk advanced the cursor, so a resume must be offered: %+v", page.Partial)
			}
			wantReason := querycontract.SearchPartialCandidateBudgetExceeded
			if tc.largeDocReason {
				wantReason = querycontract.SearchPartialBudgetExceededOnLargeDocument
				if page.Partial.OverrunMS <= 100 {
					t.Fatalf("OverrunMS = %d, want over 100 for the large document", page.Partial.OverrunMS)
				}
			}
			if page.Partial.Reason != wantReason {
				t.Fatalf("Reason = %q, want %q", page.Partial.Reason, wantReason)
			}
		})
	}
}

// tailPhases reads from the statement log whether the tail ran and whether the
// server cancelled it (a ROLLBACK TO follows the tail statement).
func tailPhases(tx *fakeTx) (ran, cancelled bool) {
	for i, label := range tx.log {
		if label != "tail" {
			continue
		}
		ran = true
		cancelled = i+1 < len(tx.log) && tx.log[i+1] == "rollback_to"
	}
	return ran, cancelled
}
